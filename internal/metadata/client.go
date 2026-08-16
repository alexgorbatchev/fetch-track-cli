package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dj/fetch-track-cli/internal/downloader"
)

// TrackMetadataResult contains normalized track metadata and artwork URL.
type TrackMetadataResult struct {
	Title       string `json:"title"`
	Artist      string `json:"artist"`
	Album       string `json:"album"`
	Genre       string `json:"genre"`
	ReleaseYear string `json:"releaseYear"`
	CoverArtURL string `json:"coverArtUrl,omitempty"`
	Source      string `json:"source"` // iTunes API, MusicBrainz API, YouTube Fallback
}

// HTTPClient interface for test mockability.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// Client wraps HTTP capabilities for metadata lookup.
type Client struct {
	httpClient HTTPClient
}

// NewClient creates a new Client with a default HTTP client timeout.
func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
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
	releaseYear := ""
	if len(item.ReleaseDate) >= 4 {
		releaseYear = item.ReleaseDate[:4]
	}

	return &TrackMetadataResult{
		Title:       item.TrackName,
		Artist:      item.ArtistName,
		Album:       album,
		Genre:       genre,
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
	releaseYear := ""
	var releaseID string

	if len(rec.Releases) > 0 {
		rel := rec.Releases[0]
		if rel.Title != "" {
			album = rel.Title
		}
		if len(rel.Date) >= 4 {
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
			}
		}
		cCancel()
	}

	return &TrackMetadataResult{
		Title:       rec.Title,
		Artist:      artist,
		Album:       album,
		Genre:       "Electronic / Ambient",
		ReleaseYear: releaseYear,
		CoverArtURL: coverArtURL,
		Source:      "MusicBrainz API",
	}, nil
}

// ResolveTrackMetadata performs iTunes -> MusicBrainz -> YouTube Fallback resolution chain.
func (c *Client) ResolveTrackMetadata(ctx context.Context, searchQuery, fallbackArtist, fallbackTitle string, verbose ...bool) TrackMetadataResult {
	isVerbose := len(verbose) > 0 && verbose[0]

	expectedTitle := fallbackTitle
	if expectedTitle == "" {
		expectedTitle = searchQuery
	}

	if isVerbose {
		fmt.Printf("metadata search: %q\n", searchQuery)
		fmt.Printf("itunes: searching\n")
	}

	if res, err := c.FetchFromITunes(ctx, searchQuery, expectedTitle); err == nil && res.Title != "" && res.Artist != "" {
		if isVerbose {
			fmt.Printf("itunes match: %q (Album: %s, %s)\n", res.Artist+" - "+res.Title, res.Album, res.ReleaseYear)
			if res.CoverArtURL != "" {
				fmt.Printf("itunes artwork: %s\n", res.CoverArtURL)
			}
		}
		return *res
	} else if isVerbose {
		fmt.Printf("itunes: no match\nmusicbrainz: searching\n")
	}

	if res, err := c.FetchFromMusicBrainz(ctx, searchQuery, expectedTitle); err == nil && res.Title != "" {
		if isVerbose {
			fmt.Printf("musicbrainz match: %q (Album: %s, %s)\n", res.Artist+" - "+res.Title, res.Album, res.ReleaseYear)
			if res.CoverArtURL != "" {
				fmt.Printf("coverart archive: %s\n", res.CoverArtURL)
			}
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
		ReleaseYear: fmt.Sprintf("%d", time.Now().Year()),
		Source:      "YouTube Fallback",
	}
}
