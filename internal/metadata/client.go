package metadata

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/alexgorbatchev/gochromaprint"
	"github.com/alexgorbatchev/godeps"
	"github.com/alexgorbatchev/goshazam"
	"github.com/dj/fetch-track-cli/internal/cache"
	"github.com/dj/fetch-track-cli/internal/downloader"
)

// TrackMetadataResult contains normalized track metadata and artwork URL.
type TrackMetadataResult struct {
	Title          string    `json:"title"`
	Artist         string    `json:"artist"`
	Album          string    `json:"album"`
	Genre          string    `json:"genre"`
	ReleaseDate    string    `json:"releaseDate,omitempty"`
	ReleaseYear    string    `json:"releaseYear"`
	CoverArtURL    string    `json:"coverArtUrl,omitempty"`
	Source         string    `json:"source"` // AcoustID / MusicBrainz, Shazam API, iTunes API, MusicBrainz API, YouTube Fallback
	AudioSourceURL string    `json:"audioSourceUrl,omitempty"`
	FetchedAt      time.Time `json:"fetchedAt,omitempty"`
}

// HTTPClient interface for test mockability.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// CommandRunner abstracts command execution for testability.
type CommandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

func defaultRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		if stderr.Len() > 0 {
			cleanErr := godeps.SanitizeStderr(stderr.String())
			if cleanErr != "" {
				return nil, fmt.Errorf("%w: %s", err, cleanErr)
			}
			return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
		}
		return nil, err
	}
	return stdout.Bytes(), nil
}

// Client wraps HTTP capabilities and command runners for metadata lookup.
type Client struct {
	httpClient HTTPClient
	runner     CommandRunner
	Cache      *cache.Cache
}

// NewClient creates a new Client with a default HTTP client timeout and runner.
func NewClient(cacheInst ...*cache.Cache) *Client {
	var c *cache.Cache
	if len(cacheInst) > 0 {
		c = cacheInst[0]
	}
	return &Client{
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
		runner: defaultRunner,
		Cache:  c,
	}
}

// SetRunner sets a custom command runner for external tools (e.g. ffmpeg).
func (c *Client) SetRunner(runner CommandRunner) {
	if runner != nil {
		c.runner = runner
	}
}

// FetchFromAcoustID computes an acoustic Chromaprint fingerprint and queries the AcoustID database.
func (c *Client) FetchFromAcoustID(ctx context.Context, filePath string) (*TrackMetadataResult, error) {
	if filePath == "" {
		return nil, errors.New("empty audio file path")
	}
	if _, err := os.Stat(filePath); err != nil {
		return nil, fmt.Errorf("stat audio file %s: %w", filePath, err)
	}

	runner := c.runner
	if runner == nil {
		runner = defaultRunner
	}

	cmdCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	stdout, err := runner(cmdCtx, "ffmpeg",
		"-v", "quiet",
		"-i", filePath,
		"-f", "s16le",
		"-ac", "1",
		"-ar", "11025",
		"-",
	)
	if err != nil || len(stdout) == 0 {
		if err != nil {
			return nil, fmt.Errorf("decoding audio for fingerprinting: %w", err)
		}
		return nil, errors.New("decoding audio for fingerprinting: empty output")
	}

	samples := make([]int16, len(stdout)/2)
	for i := 0; i < len(samples); i++ {
		samples[i] = int16(binary.LittleEndian.Uint16(stdout[i*2:]))
	}

	if len(samples) < 11025 {
		return nil, errors.New("audio stream too short for acoustic fingerprinting")
	}

	ctxChroma := gochromaprint.New(gochromaprint.AlgorithmDefault)
	if err := ctxChroma.Start(11025, 1); err != nil {
		return nil, fmt.Errorf("starting chromaprint: %w", err)
	}
	if err := ctxChroma.Feed(samples); err != nil {
		return nil, fmt.Errorf("feeding chromaprint samples: %w", err)
	}
	if err := ctxChroma.Finish(); err != nil {
		return nil, fmt.Errorf("finishing chromaprint calculation: %w", err)
	}

	fingerprint, err := ctxChroma.Fingerprint()
	if err != nil || fingerprint == "" {
		return nil, fmt.Errorf("generating fingerprint: %w", err)
	}

	duration := float64(len(samples)) / 11025.0

	apiURL := fmt.Sprintf("https://api.acoustid.org/v2/lookup?client=HSqh_oejCAM&duration=%d&fingerprint=%s&meta=recordings+releasegroups+releases+compress",
		int(duration), url.QueryEscape(fingerprint))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating AcoustID request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing AcoustID HTTP request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("AcoustID API returned HTTP status %d", resp.StatusCode)
	}

	var data struct {
		Status  string `json:"status"`
		Results []struct {
			Score      float64 `json:"score"`
			Recordings []struct {
				ID      string `json:"id"`
				Title   string `json:"title"`
				Artists []struct {
					Name string `json:"name"`
				} `json:"artists"`
				ReleaseGroups []struct {
					ID    string `json:"id"`
					Title string `json:"title"`
					Type  string `json:"type"`
				} `json:"releasegroups"`
				Releases []struct {
					ID    string `json:"id"`
					Title string `json:"title"`
					Date  struct {
						Year int `json:"year"`
					} `json:"date"`
				} `json:"releases"`
			} `json:"recordings"`
		} `json:"results"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("decoding AcoustID JSON response: %w", err)
	}

	if len(data.Results) == 0 {
		return nil, errors.New("no AcoustID matches found")
	}

	var bestRec *struct {
		ID      string `json:"id"`
		Title   string `json:"title"`
		Artists []struct {
			Name string `json:"name"`
		} `json:"artists"`
		ReleaseGroups []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
			Type  string `json:"type"`
		} `json:"releasegroups"`
		Releases []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
			Date  struct {
				Year int `json:"year"`
			} `json:"date"`
		} `json:"releases"`
	}

	for i := range data.Results {
		res := &data.Results[i]
		if res.Score >= 0.7 && len(res.Recordings) > 0 {
			bestRec = &res.Recordings[0]
			break
		}
	}

	if bestRec == nil || bestRec.Title == "" {
		return nil, errors.New("no confident recording matched in AcoustID results")
	}

	artist := "Unknown Artist"
	if len(bestRec.Artists) > 0 && bestRec.Artists[0].Name != "" {
		artist = bestRec.Artists[0].Name
	}

	album := "DJ Collection"
	releaseID := ""
	releaseYear := ""

	if len(bestRec.ReleaseGroups) > 0 && bestRec.ReleaseGroups[0].Title != "" {
		album = bestRec.ReleaseGroups[0].Title
		releaseID = bestRec.ReleaseGroups[0].ID
	}
	if len(bestRec.Releases) > 0 {
		if album == "DJ Collection" && bestRec.Releases[0].Title != "" {
			album = bestRec.Releases[0].Title
		}
		if bestRec.Releases[0].Date.Year > 0 {
			releaseYear = fmt.Sprintf("%d", bestRec.Releases[0].Date.Year)
		}
		if releaseID == "" {
			releaseID = bestRec.Releases[0].ID
		}
	}

	coverArtURL := ""
	if releaseID != "" {
		coverURL := fmt.Sprintf("https://coverartarchive.org/release-group/%s", releaseID)
		cCtx, cCancel := context.WithTimeout(ctx, 3*time.Second)
		cReq, err := http.NewRequestWithContext(cCtx, http.MethodGet, coverURL, nil)
		if err == nil {
			cResp, cErr := c.httpClient.Do(cReq)
			if cErr == nil && cResp.StatusCode == http.StatusOK {
				var coverData struct {
					Images []struct {
						Image string `json:"image"`
					} `json:"images"`
				}
				if json.NewDecoder(cResp.Body).Decode(&coverData) == nil && len(coverData.Images) > 0 {
					coverArtURL = coverData.Images[0].Image
				}
				_ = cResp.Body.Close()
			} else if cResp != nil {
				_ = cResp.Body.Close()
			}
		}
		cCancel()
	}

	return &TrackMetadataResult{
		Title:       bestRec.Title,
		Artist:      artist,
		Album:       album,
		Genre:       "Electronic",
		ReleaseDate: releaseYear,
		ReleaseYear: releaseYear,
		CoverArtURL: coverArtURL,
		Source:      "AcoustID / MusicBrainz",
	}, nil
}

// FetchFromShazam performs audio fingerprinting and recognition against Apple Shazam catalog.
func (c *Client) FetchFromShazam(ctx context.Context, filePath string) (*TrackMetadataResult, error) {
	if filePath == "" {
		return nil, errors.New("empty audio file path")
	}
	if _, err := os.Stat(filePath); err != nil {
		return nil, fmt.Errorf("stat audio file %s: %w", filePath, err)
	}

	var opts []goshazam.Option
	if stdClient, ok := c.httpClient.(*http.Client); ok {
		opts = append(opts, goshazam.WithStandardHTTPClient(stdClient))
	}
	opts = append(opts, goshazam.WithTimeout(15*time.Second))

	shz := goshazam.New(opts...)
	shzRes, err := shz.RecognizeFile(ctx, filePath)
	if err != nil {
		return nil, fmt.Errorf("shazam recognition: %w", err)
	}

	if shzRes == nil || shzRes.Track == nil || shzRes.Track.Title == "" {
		return nil, errors.New("no shazam track match found")
	}

	track := shzRes.Track
	artist := track.Subtitle
	if artist == "" && len(track.Artists) > 0 {
		artist = track.Artists[0].AdamID
	}
	if artist == "" {
		artist = "Unknown Artist"
	}

	album := "DJ Collection"
	releaseYear := ""
	for _, sec := range track.Sections {
		for _, meta := range sec.Metadata {
			if strings.EqualFold(meta.Title, "Album") && meta.Text != "" {
				album = meta.Text
			} else if strings.EqualFold(meta.Title, "Released") && meta.Text != "" {
				releaseYear = meta.Text
			}
		}
	}

	genre := "Electronic"
	if track.Genres != nil && track.Genres["primary"] != "" {
		genre = track.Genres["primary"]
	}

	coverArtURL := ""
	if track.Images != nil {
		if track.Images["coverarthq"] != "" {
			coverArtURL = track.Images["coverarthq"]
		} else if track.Images["coverart"] != "" {
			coverArtURL = track.Images["coverart"]
		}
	}

	if coverArtURL != "" {
		coverArtURL = strings.Replace(coverArtURL, "400x400cc.jpg", "1400x1400cc.jpg", 1)
		coverArtURL = strings.Replace(coverArtURL, "800x800cc.jpg", "1400x1400cc.jpg", 1)
	}

	return &TrackMetadataResult{
		Title:       track.Title,
		Artist:      artist,
		Album:       album,
		Genre:       genre,
		ReleaseDate: releaseYear,
		ReleaseYear: releaseYear,
		CoverArtURL: coverArtURL,
		Source:      "Shazam API",
	}, nil
}

// FetchFromITunes queries the iTunes Search API for song metadata and 1400x1400 artwork.
func (c *Client) FetchFromITunes(ctx context.Context, query, expectedTitle string) (*TrackMetadataResult, error) {
	apiURL := fmt.Sprintf("https://itunes.apple.com/search?term=%s&entity=song&limit=5", url.QueryEscape(query))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating iTunes request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing iTunes HTTP request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("iTunes API returned HTTP status %d", resp.StatusCode)
	}

	var data struct {
		Results []struct {
			TrackName        string `json:"trackName"`
			ArtistName       string `json:"artistName"`
			CollectionName   string `json:"collectionName"`
			PrimaryGenreName string `json:"primaryGenreName"`
			ReleaseDate      string `json:"releaseDate"`
			ArtworkURL100    string `json:"artworkUrl100"`
		} `json:"results"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("decoding iTunes JSON response: %w", err)
	}

	if len(data.Results) == 0 {
		return nil, fmt.Errorf("no iTunes match for %q", query)
	}

	expectedNorm := downloader.NormalizeUnicode(expectedTitle)
	var matchedResult *struct {
		TrackName        string `json:"trackName"`
		ArtistName       string `json:"artistName"`
		CollectionName   string `json:"collectionName"`
		PrimaryGenreName string `json:"primaryGenreName"`
		ReleaseDate      string `json:"releaseDate"`
		ArtworkURL100    string `json:"artworkUrl100"`
	}

	for i := range data.Results {
		item := &data.Results[i]
		if expectedNorm == "" {
			matchedResult = item
			break
		}
		itemNorm := downloader.NormalizeUnicode(item.TrackName)
		if strings.Contains(itemNorm, expectedNorm) || strings.Contains(expectedNorm, itemNorm) {
			matchedResult = item
			break
		}
	}

	if matchedResult == nil {
		return nil, fmt.Errorf("no iTunes track name matched expected title %q", expectedTitle)
	}

	item := *matchedResult
	if item.TrackName == "" || item.ArtistName == "" {
		return nil, fmt.Errorf("incomplete iTunes data for %q", query)
	}

	highResArtwork := ""
	if item.ArtworkURL100 != "" {
		highResArtwork = strings.Replace(item.ArtworkURL100, "100x100bb", "1400x1400bb", 1)
	}

	album := item.CollectionName
	if album == "" {
		album = "DJ Collection"
	}
	genre := item.PrimaryGenreName
	if genre == "" {
		genre = "Electronic"
	}
	releaseDate := ""
	releaseYear := ""
	if len(item.ReleaseDate) >= 10 {
		releaseDate = item.ReleaseDate[:10]
		releaseYear = item.ReleaseDate[:4]
	} else if len(item.ReleaseDate) >= 4 {
		releaseDate = item.ReleaseDate
		releaseYear = item.ReleaseDate[:4]
	}

	return &TrackMetadataResult{
		Title:       item.TrackName,
		Artist:      item.ArtistName,
		Album:       album,
		Genre:       genre,
		ReleaseDate: releaseDate,
		ReleaseYear: releaseYear,
		CoverArtURL: highResArtwork,
		Source:      "iTunes API",
	}, nil
}

// FetchFromMusicBrainz queries MusicBrainz API and Cover Art Archive.
func (c *Client) FetchFromMusicBrainz(ctx context.Context, query, expectedTitle string) (*TrackMetadataResult, error) {
	apiURL := fmt.Sprintf("https://musicbrainz.org/ws/2/recording/?query=%s&fmt=json", url.QueryEscape(query))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating MusicBrainz request: %w", err)
	}
	req.Header.Set("User-Agent", "fetch-track-cli/1.0 (https://github.com/dj/fetch-track-cli)")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing MusicBrainz request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("MusicBrainz API returned HTTP status %d", resp.StatusCode)
	}

	var data struct {
		Recordings []struct {
			Title        string `json:"title"`
			ArtistCredit []struct {
				Name string `json:"name"`
			} `json:"artist-credit"`
			Releases []struct {
				ID    string `json:"id"`
				Title string `json:"title"`
				Date  string `json:"date"`
			} `json:"releases"`
		} `json:"recordings"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("decoding MusicBrainz JSON response: %w", err)
	}

	if len(data.Recordings) == 0 {
		return nil, fmt.Errorf("no MusicBrainz match for %q", query)
	}

	expectedNorm := downloader.NormalizeUnicode(expectedTitle)
	var matchedRec *struct {
		Title        string `json:"title"`
		ArtistCredit []struct {
			Name string `json:"name"`
		} `json:"artist-credit"`
		Releases []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
			Date  string `json:"date"`
		} `json:"releases"`
	}

	for i := range data.Recordings {
		rec := &data.Recordings[i]
		if expectedNorm == "" {
			matchedRec = rec
			break
		}
		recNorm := downloader.NormalizeUnicode(rec.Title)
		if strings.Contains(recNorm, expectedNorm) || strings.Contains(expectedNorm, recNorm) {
			matchedRec = rec
			break
		}
	}

	if matchedRec == nil {
		return nil, fmt.Errorf("no MusicBrainz recording matched expected title %q", expectedTitle)
	}

	rec := *matchedRec
	if rec.Title == "" {
		return nil, fmt.Errorf("incomplete MusicBrainz recording for %q", query)
	}

	artist := "Unknown Artist"
	if len(rec.ArtistCredit) > 0 && rec.ArtistCredit[0].Name != "" {
		artist = rec.ArtistCredit[0].Name
	}

	album := "DJ Collection"
	releaseDate := ""
	releaseYear := ""
	var releaseID string

	if len(rec.Releases) > 0 {
		rel := rec.Releases[0]
		if rel.Title != "" {
			album = rel.Title
		}
		if len(rel.Date) >= 10 {
			releaseDate = rel.Date[:10]
			releaseYear = rel.Date[:4]
		} else if len(rel.Date) >= 4 {
			releaseDate = rel.Date
			releaseYear = rel.Date[:4]
		}
		releaseID = rel.ID
	}

	coverArtURL := ""
	if releaseID != "" {
		coverURL := fmt.Sprintf("https://coverartarchive.org/release/%s", releaseID)
		cCtx, cCancel := context.WithTimeout(ctx, 3*time.Second)
		cReq, err := http.NewRequestWithContext(cCtx, http.MethodGet, coverURL, nil)
		if err == nil {
			cResp, cErr := c.httpClient.Do(cReq)
			if cErr == nil && cResp.StatusCode == http.StatusOK {
				var coverData struct {
					Images []struct {
						Image string `json:"image"`
					} `json:"images"`
				}
				if json.NewDecoder(cResp.Body).Decode(&coverData) == nil && len(coverData.Images) > 0 {
					coverArtURL = coverData.Images[0].Image
				}
				_ = cResp.Body.Close()
			} else if cResp != nil {
				_ = cResp.Body.Close()
			}
		}
		cCancel()
	}

	return &TrackMetadataResult{
		Title:       rec.Title,
		Artist:      artist,
		Album:       album,
		Genre:       "Electronic / Ambient",
		ReleaseDate: releaseDate,
		ReleaseYear: releaseYear,
		CoverArtURL: coverArtURL,
		Source:      "MusicBrainz API",
	}, nil
}

// ResolveTrackMetadata performs AcoustID -> Shazam -> iTunes -> MusicBrainz -> YouTube Fallback resolution chain.
func (c *Client) ResolveTrackMetadata(ctx context.Context, audioFilePath, searchQuery, fallbackArtist, fallbackTitle string, verbose ...bool) TrackMetadataResult {
	isVerbose := len(verbose) > 0 && verbose[0]

	cacheKey := fmt.Sprintf("%s:%s:%s:%s", audioFilePath, searchQuery, fallbackArtist, fallbackTitle)
	var cachedRes TrackMetadataResult
	if c.Cache != nil && c.Cache.Get("metadata", cacheKey, &cachedRes) {
		if isVerbose {
			fmt.Printf("metadata cache hit for %q\n", searchQuery)
		}
		if !strings.HasSuffix(cachedRes.Source, "[cached]") {
			cachedRes.Source = cachedRes.Source + " [cached]"
		}
		return cachedRes
	}

	// 1. Acoustic Fingerprinting Fallback: AcoustID -> Shazam
	if audioFilePath != "" {
		if _, err := os.Stat(audioFilePath); err == nil {
			if isVerbose {
				fmt.Printf("metadata acoustic: probing %s via AcoustID\n", audioFilePath)
			}
			if res, err := c.FetchFromAcoustID(ctx, audioFilePath); err == nil && res.Title != "" && res.Artist != "" {
				if isVerbose {
					fmt.Printf("acoustid match: %q (Album: %s, %s)\n", res.Artist+" - "+res.Title, res.Album, res.ReleaseYear)
					if res.CoverArtURL != "" {
						fmt.Printf("acoustid artwork: %s\n", res.CoverArtURL)
					}
				}
				if c.Cache != nil {
					_ = c.Cache.Put("metadata", cacheKey, *res, 7*24*time.Hour)
				}
				return *res
			} else if isVerbose {
				fmt.Printf("acoustid: no match (%v)\nmetadata acoustic: probing %s via Shazam\n", err, audioFilePath)
			}

			if res, err := c.FetchFromShazam(ctx, audioFilePath); err == nil && res.Title != "" && res.Artist != "" {
				if isVerbose {
					fmt.Printf("shazam match: %q (Album: %s, %s)\n", res.Artist+" - "+res.Title, res.Album, res.ReleaseYear)
					if res.CoverArtURL != "" {
						fmt.Printf("shazam artwork: %s\n", res.CoverArtURL)
					}
				}
				if c.Cache != nil {
					_ = c.Cache.Put("metadata", cacheKey, *res, 7*24*time.Hour)
				}
				return *res
			} else if isVerbose {
				fmt.Printf("shazam: no match (%v)\n", err)
			}
		}
	}

	expectedTitle := fallbackTitle
	if expectedTitle == "" {
		expectedTitle = searchQuery
	}

	search := searchQuery
	if fallbackArtist != "" && !strings.Contains(strings.ToLower(searchQuery), strings.ToLower(fallbackArtist)) {
		search = fmt.Sprintf("%s %s", fallbackArtist, searchQuery)
	}
	search = strings.ReplaceAll(search, "_", " ")

	if isVerbose {
		fmt.Printf("metadata search: %q\n", search)
		fmt.Printf("itunes: searching\n")
	}

	if res, err := c.FetchFromITunes(ctx, search, expectedTitle); err == nil && res.Title != "" && res.Artist != "" {
		if isVerbose {
			fmt.Printf("itunes match: %q (Album: %s, %s)\n", res.Artist+" - "+res.Title, res.Album, res.ReleaseYear)
			if res.CoverArtURL != "" {
				fmt.Printf("itunes artwork: %s\n", res.CoverArtURL)
			}
		}
		if c.Cache != nil {
			_ = c.Cache.Put("metadata", cacheKey, *res, 7*24*time.Hour)
		}
		return *res
	} else if isVerbose {
		fmt.Printf("itunes: no match\nmusicbrainz: searching\n")
	}

	if res, err := c.FetchFromMusicBrainz(ctx, search, expectedTitle); err == nil && res.Title != "" {
		if isVerbose {
			fmt.Printf("musicbrainz match: %q (Album: %s, %s)\n", res.Artist+" - "+res.Title, res.Album, res.ReleaseYear)
			if res.CoverArtURL != "" {
				fmt.Printf("coverart archive: %s\n", res.CoverArtURL)
			}
		}
		if c.Cache != nil {
			_ = c.Cache.Put("metadata", cacheKey, *res, 7*24*time.Hour)
		}
		return *res
	} else if isVerbose {
		fmt.Printf("musicbrainz: no match\nmetadata fallback: youtube raw\n")
	}

	artist := fallbackArtist
	if artist == "" {
		artist = "Unknown Artist"
	}
	title := fallbackTitle
	if title == "" {
		title = searchQuery
	}

	return TrackMetadataResult{
		Title:       title,
		Artist:      artist,
		Album:       "DJ Collection",
		Genre:       "Electronic",
		ReleaseDate: fmt.Sprintf("%d", time.Now().Year()),
		ReleaseYear: fmt.Sprintf("%d", time.Now().Year()),
		Source:      "YouTube Fallback",
	}
}
