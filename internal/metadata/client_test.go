package metadata

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/dj/fetch-track-cli/internal/cache"
)

type mockTransport func(req *http.Request) (*http.Response, error)

func (m mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return m(req)
}

func newMockClient(fn mockTransport) *Client {
	return &Client{
		httpClient: &http.Client{
			Transport: fn,
		},
	}
}

func TestNewClient(t *testing.T) {
	c := NewClient()
	if c == nil || c.httpClient == nil {
		t.Fatal("NewClient returned nil client")
	}

	cacheDir := t.TempDir()
	cacheInst := cache.NewInDir(cacheDir, true)
	c2 := NewClient(cacheInst)
	if c2 == nil || c2.Cache == nil {
		t.Fatal("NewClient with cache returned nil cache")
	}
}

func TestFetchFromITunes(t *testing.T) {
	tests := []struct {
		name            string
		statusCode      int
		body            string
		wantErr         bool
		wantTitle       string
		wantReleaseDate string
		wantReleaseYear string
	}{
		{
			name:       "successful iTunes search",
			statusCode: http.StatusOK,
			body: `{
				"results": [
					{
						"trackName": "Space X",
						"artistName": "Boris Brejcha",
						"collectionName": "Space X Single",
						"primaryGenreName": "Minimal Techno",
						"releaseDate": "2024-05-10T07:00:00Z",
						"artworkUrl100": "https://is1-ssl.mzstatic.com/image/thumb/Music123/v4/100x100bb.jpg"
					}
				]
			}`,
			wantErr:         false,
			wantTitle:       "Space X",
			wantReleaseDate: "2024-05-10",
			wantReleaseYear: "2024",
		},
		{
			name:       "empty iTunes results",
			statusCode: http.StatusOK,
			body:       `{"results": []}`,
			wantErr:    true,
		},
		{
			name:       "iTunes HTTP error 500",
			statusCode: http.StatusInternalServerError,
			body:       `error`,
			wantErr:    true,
		},
		{
			name:       "invalid iTunes JSON",
			statusCode: http.StatusOK,
			body:       `{invalid json}`,
			wantErr:    true,
		},
		{
			name:       "title mismatch",
			statusCode: http.StatusOK,
			body: `{
				"results": [
					{
						"trackName": "Completely Different Song",
						"artistName": "Other Artist"
					}
				]
			}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newMockClient(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: tt.statusCode,
					Body:       io.NopCloser(bytes.NewBufferString(tt.body)),
				}, nil
			})

			res, err := client.FetchFromITunes(context.Background(), "Space X", "Space X")
			if (err != nil) != tt.wantErr {
				t.Fatalf("FetchFromITunes error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr {
				if res.Title != tt.wantTitle {
					t.Errorf("Title = %q, want %q", res.Title, tt.wantTitle)
				}
				if tt.wantReleaseDate != "" && res.ReleaseDate != tt.wantReleaseDate {
					t.Errorf("ReleaseDate = %q, want %q", res.ReleaseDate, tt.wantReleaseDate)
				}
				if tt.wantReleaseYear != "" && res.ReleaseYear != tt.wantReleaseYear {
					t.Errorf("ReleaseYear = %q, want %q", res.ReleaseYear, tt.wantReleaseYear)
				}
			}
		})
	}
}

func TestFetchFromMusicBrainz(t *testing.T) {
	tests := []struct {
		name            string
		statusCode      int
		body            string
		wantErr         bool
		wantTitle       string
		wantArtist      string
		wantReleaseDate string
		wantReleaseYear string
	}{
		{
			name:       "successful MusicBrainz search",
			statusCode: http.StatusOK,
			body: `{
				"recordings": [
					{
						"title": "Gopnik",
						"artist-credit": [{"name": "DJ Blyatman"}],
						"releases": [
							{"id": "rel-123", "title": "Gopnik Album", "date": "2020-01-01"}
						]
					}
				]
			}`,
			wantErr:         false,
			wantTitle:       "Gopnik",
			wantArtist:      "DJ Blyatman",
			wantReleaseDate: "2020-01-01",
			wantReleaseYear: "2020",
		},
		{
			name:       "empty MusicBrainz recordings",
			statusCode: http.StatusOK,
			body:       `{"recordings": []}`,
			wantErr:    true,
		},
		{
			name:       "invalid MusicBrainz JSON",
			statusCode: http.StatusOK,
			body:       `{bad}`,
			wantErr:    true,
		},
		{
			name:       "title mismatch in MusicBrainz",
			statusCode: http.StatusOK,
			body: `{
				"recordings": [
					{
						"title": "Unrelated Track",
						"artist-credit": [{"name": "Someone Else"}]
					}
				]
			}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newMockClient(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: tt.statusCode,
					Body:       io.NopCloser(bytes.NewBufferString(tt.body)),
				}, nil
			})

			res, err := client.FetchFromMusicBrainz(context.Background(), "Gopnik", "Gopnik")
			if (err != nil) != tt.wantErr {
				t.Fatalf("FetchFromMusicBrainz error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr {
				if res.Title != tt.wantTitle {
					t.Errorf("Title = %q, want %q", res.Title, tt.wantTitle)
				}
				if res.Artist != tt.wantArtist {
					t.Errorf("Artist = %q, want %q", res.Artist, tt.wantArtist)
				}
				if tt.wantReleaseDate != "" && res.ReleaseDate != tt.wantReleaseDate {
					t.Errorf("ReleaseDate = %q, want %q", res.ReleaseDate, tt.wantReleaseDate)
				}
				if tt.wantReleaseYear != "" && res.ReleaseYear != tt.wantReleaseYear {
					t.Errorf("ReleaseYear = %q, want %q", res.ReleaseYear, tt.wantReleaseYear)
				}
			}
		})
	}
}

func TestResolveTrackMetadataFallback(t *testing.T) {
	// Mock client that returns 404 for iTunes and successful MusicBrainz
	client := newMockClient(func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Host, "itunes") {
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Body:       io.NopCloser(bytes.NewBufferString(`{}`)),
			}, nil
		}
		if strings.Contains(req.URL.Host, "musicbrainz") {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(bytes.NewBufferString(`{
					"recordings": [
						{
							"title": "Gopnik MB",
							"artist-credit": [{"name": "DJ Blyatman MB"}],
							"releases": [{"id": "mb-1", "title": "MB Album", "date": "2021-06-15"}]
						}
					]
				}`)),
			}, nil
		}
		if strings.Contains(req.URL.Host, "coverartarchive.org") {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(bytes.NewBufferString(`{
					"images": [{"image": "https://coverartarchive.org/image.jpg"}]
				}`)),
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Body:       io.NopCloser(bytes.NewBufferString(`{}`)),
		}, nil
	})

	res := client.ResolveTrackMetadata(context.Background(), "Gopnik", "Fallback Artist", "Gopnik", true)
	if res.Artist != "DJ Blyatman MB" {
		t.Errorf("Artist = %q, want DJ Blyatman MB", res.Artist)
	}
	if res.Source != "MusicBrainz API" {
		t.Errorf("Source = %q, want MusicBrainz API", res.Source)
	}
	if res.ReleaseDate != "2021-06-15" {
		t.Errorf("ReleaseDate = %q, want 2021-06-15", res.ReleaseDate)
	}
	if res.CoverArtURL != "https://coverartarchive.org/image.jpg" {
		t.Errorf("CoverArtURL = %q, want https://coverartarchive.org/image.jpg", res.CoverArtURL)
	}
}

func TestResolveTrackMetadata_ITunesAndYouTubeFallback(t *testing.T) {
	// 1. Test iTunes Match branch
	clientITunes := newMockClient(func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Host, "itunes") {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(bytes.NewBufferString(`{
					"results": [
						{
							"trackName": "Space X",
							"artistName": "Boris Brejcha",
							"collectionName": "Space X Single",
							"releaseDate": "2024-05-10T07:00:00Z",
							"artworkUrl100": "https://is1-ssl.mzstatic.com/image/thumb/Music123/v4/100x100bb.jpg"
						}
					]
				}`)),
			}, nil
		}
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(bytes.NewBufferString(`{}`))}, nil
	})

	resITunes := clientITunes.ResolveTrackMetadata(context.Background(), "Space X", "Boris Brejcha", "Space X", true)
	if resITunes.Source != "iTunes API" {
		t.Errorf("expected iTunes API source, got %q", resITunes.Source)
	}

	// 2. Test YouTube Fallback branch when both fail
	clientFallback := newMockClient(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Body:       io.NopCloser(bytes.NewBufferString(`{}`)),
		}, nil
	})

	resFallback := clientFallback.ResolveTrackMetadata(context.Background(), "Unknown Track", "Raw Artist", "Unknown Track", true)
	if resFallback.Source != "YouTube Fallback" {
		t.Errorf("expected YouTube Fallback source, got %q", resFallback.Source)
	}
	if resFallback.Artist != "Raw Artist" {
		t.Errorf("Artist = %q, want Raw Artist", resFallback.Artist)
	}

	// 3. Test Cache Hit branch in ResolveTrackMetadata
	cacheDir := t.TempDir()
	cacheInst := cache.NewInDir(cacheDir, true)
	clientITunes.Cache = cacheInst

	// First call caches it
	_ = clientITunes.ResolveTrackMetadata(context.Background(), "Space X", "Boris Brejcha", "Space X", true)
	// Second call retrieves from cache
	resCached := clientITunes.ResolveTrackMetadata(context.Background(), "Space X", "Boris Brejcha", "Space X", true)
	if !strings.Contains(resCached.Source, "[cached]") {
		t.Errorf("expected [cached] source, got %q", resCached.Source)
	}

	// 4. Test YouTube Fallback with empty fallbackArtist
	resEmptyArtist := clientFallback.ResolveTrackMetadata(context.Background(), "Fallback Track", "", "Fallback Track", false)
	if resEmptyArtist.Artist != "Unknown Artist" {
		t.Errorf("expected 'Unknown Artist', got %q", resEmptyArtist.Artist)
	}
}

func TestFetchFromITunes_AdditionalBranches(t *testing.T) {
	ctx := context.Background()

	t.Run("empty_expected_title_matches_first", func(t *testing.T) {
		client := newMockClient(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(bytes.NewBufferString(`{
					"results": [
						{"trackName": "First Track", "artistName": "Artist", "releaseDate": "2024"}
					]
				}`)),
			}, nil
		})

		res, err := client.FetchFromITunes(ctx, "First Track", "")
		if err != nil || res.Title != "First Track" {
			t.Fatalf("unexpected result: res=%v, err=%v", res, err)
		}
	})

	t.Run("incomplete_data_error", func(t *testing.T) {
		client := newMockClient(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(bytes.NewBufferString(`{
					"results": [
						{"trackName": "Track", "artistName": ""}
					]
				}`)),
			}, nil
		})

		_, err := client.FetchFromITunes(ctx, "Track", "Track")
		if err == nil {
			t.Fatal("expected error for incomplete data")
		}
	})
}

func TestFetchFromMusicBrainz_AdditionalBranches(t *testing.T) {
	ctx := context.Background()

	t.Run("empty_expected_title_and_4digit_date", func(t *testing.T) {
		client := newMockClient(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(bytes.NewBufferString(`{
					"recordings": [
						{
							"title": "First MB Track",
							"artist-credit": [{"name": "MB Artist"}],
							"releases": [{"id": "r1", "title": "Album", "date": "2021"}]
						}
					]
				}`)),
			}, nil
		})

		res, err := client.FetchFromMusicBrainz(ctx, "Track", "")
		if err != nil || res.Title != "First MB Track" {
			t.Fatalf("unexpected result: res=%v, err=%v", res, err)
		}
		if res.ReleaseYear != "2021" {
			t.Errorf("expected 2021, got %q", res.ReleaseYear)
		}
	})

	t.Run("incomplete_musicbrainz_recording", func(t *testing.T) {
		client := newMockClient(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(bytes.NewBufferString(`{
					"recordings": [
						{"title": "", "artist-credit": []}
					]
				}`)),
			}, nil
		})

		_, err := client.FetchFromMusicBrainz(ctx, "Track", "Track")
		if err == nil {
			t.Fatal("expected error for incomplete recording")
		}
	})
}
