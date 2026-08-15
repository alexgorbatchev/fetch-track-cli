package metadata

import (
	"bytes"
	"context"
	"io"
	"net/http"
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

func TestFetchFromITunes(t *testing.T) {
	jsonResp := `{
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
	}`

	client := newMockClient(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewBufferString(jsonResp)),
		}, nil
	})

	res, err := client.FetchFromITunes(context.Background(), "Boris Brejcha - Space X")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.Title != "Space X" {
		t.Errorf("Title = %q, want Space X", res.Title)
	}
	if res.Artist != "Boris Brejcha" {
		t.Errorf("Artist = %q, want Boris Brejcha", res.Artist)
	}
	wantArtwork := "https://is1-ssl.mzstatic.com/image/thumb/Music123/v4/1400x1400bb.jpg"
	if res.CoverArtURL != wantArtwork {
		t.Errorf("CoverArtURL = %q, want %q", res.CoverArtURL, wantArtwork)
	}
	if res.Source != "iTunes API" {
		t.Errorf("Source = %q, want iTunes API", res.Source)
	}
}

func TestResolveTrackMetadataFallback(t *testing.T) {
	// Mock client that returns 404 for iTunes and MusicBrainz
	client := newMockClient(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Body:       io.NopCloser(bytes.NewBufferString(`{}`)),
		}, nil
	})

	res := client.ResolveTrackMetadata(context.Background(), "Unknown Song", "Default Artist", "Default Title")
	if res.Artist != "Default Artist" {
		t.Errorf("Artist = %q, want Default Artist", res.Artist)
	}
	if res.Title != "Default Title" {
		t.Errorf("Title = %q, want Default Title", res.Title)
	}
	if res.Source != "YouTube Fallback" {
		t.Errorf("Source = %q, want YouTube Fallback", res.Source)
	}
}
