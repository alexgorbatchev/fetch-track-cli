package metadata

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
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
}

func TestFetchFromITunes(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantErr    bool
		wantTitle  string
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
			wantErr:   false,
			wantTitle: "Space X",
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
			if !tt.wantErr && res.Title != tt.wantTitle {
				t.Errorf("Title = %q, want %q", res.Title, tt.wantTitle)
			}
		})
	}
}

func TestFetchFromMusicBrainz(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantErr    bool
		wantTitle  string
		wantArtist string
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
			wantErr:    false,
			wantTitle:  "Gopnik",
			wantArtist: "DJ Blyatman",
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
							"releases": [{"id": "mb-1", "title": "MB Album", "date": "2021"}]
						}
					]
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
}
