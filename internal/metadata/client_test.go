package metadata

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
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
		runner: defaultRunner,
	}
}

func TestDefaultRunner(t *testing.T) {
	ctx := context.Background()

	// 1. Successful execution
	out, err := defaultRunner(ctx, "echo", "hello")
	if err != nil || strings.TrimSpace(string(out)) != "hello" {
		t.Fatalf("defaultRunner success failed: out=%q, err=%v", string(out), err)
	}

	// 2. Failure with stderr
	_, err = defaultRunner(ctx, "sh", "-c", "echo 'some error' >&2; exit 1")
	if err == nil || !strings.Contains(err.Error(), "some error") {
		t.Fatalf("expected error containing 'some error', got %v", err)
	}

	// 3. Failure without stderr
	_, err = defaultRunner(ctx, "sh", "-c", "exit 1")
	if err == nil {
		t.Fatal("expected error for non-zero exit code")
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

	c2.SetRunner(defaultRunner)
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

func TestFetchFromAcoustID(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	dummyFile := filepath.Join(tempDir, "sample.m4a")
	if err := os.WriteFile(dummyFile, []byte("dummy audio content"), 0644); err != nil {
		t.Fatalf("creating dummy audio file: %v", err)
	}

	// Generate 11025 * 2 samples of synthetic 16-bit PCM (2 seconds)
	syntheticPCMLen := 11025 * 2
	syntheticPCM := make([]byte, syntheticPCMLen*2)
	for i := 0; i < syntheticPCMLen; i++ {
		binary.LittleEndian.PutUint16(syntheticPCM[i*2:], uint16(i%1000))
	}

	t.Run("successful_acoustid_match_with_cover_art", func(t *testing.T) {
		client := newMockClient(func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.Host, "acoustid.org") {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body: io.NopCloser(bytes.NewBufferString(`{
						"status": "ok",
						"results": [
							{
								"score": 0.95,
								"recordings": [
									{
										"id": "mbid-123",
										"title": "Kernkraft 400 (W&W remix)",
										"artists": [{"name": "Zombie Nation"}],
										"releasegroups": [{"id": "rg-1", "title": "Kernkraft 400 Single", "type": "Single"}],
										"releases": [{"id": "rel-1", "title": "Kernkraft 400", "date": {"year": 2015}}]
									}
								]
							}
						]
					}`)),
				}, nil
			}
			if strings.Contains(req.URL.Host, "coverartarchive.org") {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body: io.NopCloser(bytes.NewBufferString(`{
						"images": [{"image": "https://coverartarchive.org/release/rg-1.jpg"}]
					}`)),
				}, nil
			}
			return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(bytes.NewBufferString(`{}`))}, nil
		})

		client.SetRunner(func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return syntheticPCM, nil
		})

		res, err := client.FetchFromAcoustID(ctx, dummyFile)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Title != "Kernkraft 400 (W&W remix)" {
			t.Errorf("Title = %q, want Kernkraft 400 (W&W remix)", res.Title)
		}
		if res.Artist != "Zombie Nation" {
			t.Errorf("Artist = %q, want Zombie Nation", res.Artist)
		}
		if res.ReleaseYear != "2015" {
			t.Errorf("ReleaseYear = %q, want 2015", res.ReleaseYear)
		}
		if res.CoverArtURL != "https://coverartarchive.org/release/rg-1.jpg" {
			t.Errorf("CoverArtURL = %q", res.CoverArtURL)
		}
		if res.Source != "AcoustID / MusicBrainz" {
			t.Errorf("Source = %q", res.Source)
		}
	})

	t.Run("acoustid_empty_path", func(t *testing.T) {
		client := newMockClient(nil)
		_, err := client.FetchFromAcoustID(ctx, "")
		if err == nil {
			t.Fatal("expected error for empty file path")
		}
	})

	t.Run("acoustid_nonexistent_file", func(t *testing.T) {
		client := newMockClient(nil)
		_, err := client.FetchFromAcoustID(ctx, filepath.Join(tempDir, "nonexistent.m4a"))
		if err == nil {
			t.Fatal("expected error for nonexistent file")
		}
	})

	t.Run("acoustid_ffmpeg_runner_error", func(t *testing.T) {
		client := newMockClient(nil)
		client.SetRunner(func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return nil, errors.New("ffmpeg decode failed")
		})
		_, err := client.FetchFromAcoustID(ctx, dummyFile)
		if err == nil {
			t.Fatal("expected error when runner fails")
		}
	})

	t.Run("acoustid_audio_too_short", func(t *testing.T) {
		client := newMockClient(nil)
		client.SetRunner(func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return []byte{0, 1, 2, 3}, nil // too few samples
		})
		_, err := client.FetchFromAcoustID(ctx, dummyFile)
		if err == nil {
			t.Fatal("expected error when audio is too short")
		}
	})

	t.Run("acoustid_http_error", func(t *testing.T) {
		client := newMockClient(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       io.NopCloser(bytes.NewBufferString(`server error`)),
			}, nil
		})
		client.SetRunner(func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return syntheticPCM, nil
		})
		_, err := client.FetchFromAcoustID(ctx, dummyFile)
		if err == nil {
			t.Fatal("expected error on HTTP 500")
		}
	})

	t.Run("acoustid_invalid_json", func(t *testing.T) {
		client := newMockClient(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString(`{invalid json`)),
			}, nil
		})
		client.SetRunner(func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return syntheticPCM, nil
		})
		_, err := client.FetchFromAcoustID(ctx, dummyFile)
		if err == nil {
			t.Fatal("expected error on invalid json")
		}
	})

	t.Run("acoustid_no_matches", func(t *testing.T) {
		client := newMockClient(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString(`{"status": "ok", "results": []}`)),
			}, nil
		})
		client.SetRunner(func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return syntheticPCM, nil
		})
		_, err := client.FetchFromAcoustID(ctx, dummyFile)
		if err == nil {
			t.Fatal("expected error when no results found")
		}
	})

	t.Run("acoustid_score_below_threshold", func(t *testing.T) {
		client := newMockClient(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(bytes.NewBufferString(`{
					"status": "ok",
					"results": [
						{"score": 0.2, "recordings": [{"title": "Bad Match"}]}
					]
				}`)),
			}, nil
		})
		client.SetRunner(func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return syntheticPCM, nil
		})
		_, err := client.FetchFromAcoustID(ctx, dummyFile)
		if err == nil {
			t.Fatal("expected error when score is below threshold")
		}
	})

	t.Run("acoustid_cache_hit", func(t *testing.T) {
		cacheDir := t.TempDir()
		cacheInst := cache.NewInDir(cacheDir, true)

		networkCalls := 0
		client := newMockClient(func(req *http.Request) (*http.Response, error) {
			networkCalls++
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(bytes.NewBufferString(`{
					"status": "ok",
					"results": [
						{
							"score": 0.95,
							"recordings": [
								{
									"id": "mbid-cached",
									"title": "Cached AcoustID Track",
									"artists": [{"name": "Cached Artist"}]
								}
							]
						}
					]
				}`)),
			}, nil
		})
		client.Cache = cacheInst
		client.SetRunner(func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return syntheticPCM, nil
		})

		// First call hits network
		res1, err := client.FetchFromAcoustID(ctx, dummyFile)
		if err != nil || res1.Title != "Cached AcoustID Track" {
			t.Fatalf("first call failed: %v", err)
		}
		if networkCalls != 1 {
			t.Fatalf("expected 1 network call, got %d", networkCalls)
		}

		// Second call with different file path but same audio/fingerprint hits cache
		dummyFile2 := filepath.Join(tempDir, "sample2.m4a")
		if err := os.WriteFile(dummyFile2, []byte("dummy audio content 2"), 0644); err != nil {
			t.Fatalf("writing dummy file 2: %v", err)
		}

		res2, err := client.FetchFromAcoustID(ctx, dummyFile2)
		if err != nil || res2.Title != "Cached AcoustID Track" {
			t.Fatalf("second call from cache failed: %v", err)
		}
		if networkCalls != 1 {
			t.Fatalf("expected network call count to remain 1 on cache hit, got %d", networkCalls)
		}
	})

	t.Run("acoustid_releases_fallback", func(t *testing.T) {
		client := newMockClient(func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.Host, "acoustid.org") {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body: io.NopCloser(bytes.NewBufferString(`{
						"status": "ok",
						"results": [
							{
								"score": 0.85,
								"recordings": [
									{
										"id": "mbid-2",
										"title": "Release Only Track",
										"artists": [],
										"releases": [{"id": "rel-99", "title": "Release Title Album", "date": {"year": 2019}}]
									}
								]
							}
						]
					}`)),
				}, nil
			}
			return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(bytes.NewBufferString(`{}`))}, nil
		})

		client.SetRunner(func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return syntheticPCM, nil
		})

		res, err := client.FetchFromAcoustID(ctx, dummyFile)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Artist != "Unknown Artist" {
			t.Errorf("expected Unknown Artist, got %q", res.Artist)
		}
		if res.Album != "Release Title Album" {
			t.Errorf("Album = %q, want Release Title Album", res.Album)
		}
		if res.ReleaseYear != "2019" {
			t.Errorf("ReleaseYear = %q, want 2019", res.ReleaseYear)
		}
	})
}

func TestFetchFromShazam(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	dummyFile := filepath.Join(tempDir, "sample.m4a")
	if err := os.WriteFile(dummyFile, []byte("dummy audio content"), 0644); err != nil {
		t.Fatalf("creating dummy audio file: %v", err)
	}

	t.Run("shazam_empty_path", func(t *testing.T) {
		client := newMockClient(nil)
		_, err := client.FetchFromShazam(ctx, "")
		if err == nil {
			t.Fatal("expected error for empty file path")
		}
	})

	t.Run("shazam_nonexistent_file", func(t *testing.T) {
		client := newMockClient(nil)
		_, err := client.FetchFromShazam(ctx, filepath.Join(tempDir, "nonexistent.m4a"))
		if err == nil {
			t.Fatal("expected error for nonexistent file")
		}
	})

	t.Run("shazam_mock_response_match", func(t *testing.T) {
		client := newMockClient(func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.Host, "shazam.com") {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body: io.NopCloser(bytes.NewBufferString(`{
						"matches": [{"id": "123"}],
						"track": {
							"title": "Kernkraft 400 (W&W Remix)",
							"subtitle": "Zombie Nation",
							"genres": {"primary": "Dance"},
							"images": {"coverarthq": "https://images.shazam.com/cover/400x400cc.jpg"},
							"sections": [
								{
									"type": "SONG",
									"metadata": [
										{"title": "Album", "text": "Dance Mix 2023"},
										{"title": "Released", "text": "2023"}
									]
								}
							]
						}
					}`)),
				}, nil
			}
			return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(bytes.NewBufferString(`{}`))}, nil
		})

		// Use synthetic valid small audio file created via ffmpeg
		realAudioPath := filepath.Join(tempDir, "real.m4a")
		_, _ = defaultRunner(ctx, "ffmpeg", "-v", "quiet", "-hide_banner", "-f", "lavfi", "-i", "anullsrc=r=16000:cl=mono", "-t", "1", "-y", realAudioPath)

		if _, err := os.Stat(realAudioPath); err == nil {
			res, err := client.FetchFromShazam(ctx, realAudioPath)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.Title != "Kernkraft 400 (W&W Remix)" {
				t.Errorf("Title = %q", res.Title)
			}
			if res.Artist != "Zombie Nation" {
				t.Errorf("Artist = %q", res.Artist)
			}
			if res.Album != "Dance Mix 2023" {
				t.Errorf("Album = %q", res.Album)
			}
			if res.ReleaseYear != "2023" {
				t.Errorf("ReleaseYear = %q", res.ReleaseYear)
			}
			if !strings.Contains(res.CoverArtURL, "1400x1400cc.jpg") {
				t.Errorf("CoverArtURL = %q, expected 1400x1400 upgraded url", res.CoverArtURL)
			}
			if res.Source != "Shazam API" {
				t.Errorf("Source = %q", res.Source)
			}
		}
	})

	t.Run("shazam_fallback_artist_and_coverart", func(t *testing.T) {
		client := newMockClient(func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.Host, "shazam.com") {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body: io.NopCloser(bytes.NewBufferString(`{
						"matches": [{"id": "123"}],
						"track": {
							"title": "Solo Track",
							"subtitle": "",
							"artists": [{"adamid": "12345"}],
							"images": {"coverart": "https://images.shazam.com/cover/800x800cc.jpg"}
						}
					}`)),
				}, nil
			}
			return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(bytes.NewBufferString(`{}`))}, nil
		})

		realAudioPath := filepath.Join(tempDir, "real_solo.m4a")
		_, _ = defaultRunner(ctx, "ffmpeg", "-v", "quiet", "-hide_banner", "-f", "lavfi", "-i", "anullsrc=r=16000:cl=mono", "-t", "1", "-y", realAudioPath)

		if _, err := os.Stat(realAudioPath); err == nil {
			res, err := client.FetchFromShazam(ctx, realAudioPath)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.Artist != "12345" {
				t.Errorf("Artist = %q, want 12345", res.Artist)
			}
			if !strings.Contains(res.CoverArtURL, "1400x1400cc.jpg") {
				t.Errorf("CoverArtURL = %q", res.CoverArtURL)
			}
		}
	})

	t.Run("shazam_alternate_image_fields", func(t *testing.T) {
		tests := []struct {
			name     string
			trackJSON string
			wantURL  string
		}{
			{
				name: "background_image",
				trackJSON: `{
					"title": "Track 1",
					"images": {"background": "https://images.shazam.com/bg/400x400cc.jpg"}
				}`,
				wantURL: "https://images.shazam.com/bg/1400x1400cc.jpg",
			},
			{
				name: "share_image",
				trackJSON: `{
					"title": "Track 2",
					"share": {"image": "https://images.shazam.com/share.jpg"}
				}`,
				wantURL: "https://images.shazam.com/share.jpg",
			},
			{
				name: "share_avatar",
				trackJSON: `{
					"title": "Track 3",
					"share": {"avatar": "https://images.shazam.com/share-avatar.jpg"}
				}`,
				wantURL: "https://images.shazam.com/share-avatar.jpg",
			},
			{
				name: "hub_image",
				trackJSON: `{
					"title": "Track 4",
					"hub": {"image": "https://images.shazam.com/hub.jpg"}
				}`,
				wantURL: "https://images.shazam.com/hub.jpg",
			},
			{
				name: "hub_options_image",
				trackJSON: `{
					"title": "Track 5",
					"hub": {"options": [{"image": "https://images.shazam.com/hub-opt.jpg"}]}
				}`,
				wantURL: "https://images.shazam.com/hub-opt.jpg",
			},
			{
				name: "sections_avatar",
				trackJSON: `{
					"title": "Track 6",
					"sections": [{"avatar": "https://images.shazam.com/sec-avatar.jpg"}]
				}`,
				wantURL: "https://images.shazam.com/sec-avatar.jpg",
			},
			{
				name: "sections_metapages_image",
				trackJSON: `{
					"title": "Track 7",
					"sections": [{"metapages": [{"image": "https://images.shazam.com/sec-meta.jpg"}]}]
				}`,
				wantURL: "https://images.shazam.com/sec-meta.jpg",
			},
		}

		realAudioPath := filepath.Join(tempDir, "real_alt.m4a")
		_, _ = defaultRunner(ctx, "ffmpeg", "-v", "quiet", "-hide_banner", "-f", "lavfi", "-i", "anullsrc=r=16000:cl=mono", "-t", "1", "-y", realAudioPath)

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				client := newMockClient(func(req *http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body: io.NopCloser(bytes.NewBufferString(fmt.Sprintf(`{
							"matches": [{"id": "123"}],
							"track": %s
						}`, tt.trackJSON))),
					}, nil
				})

				res, err := client.FetchFromShazam(ctx, realAudioPath)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if res.CoverArtURL != tt.wantURL {
					t.Errorf("CoverArtURL = %q, want %q", res.CoverArtURL, tt.wantURL)
				}
			})
		}
	})

	t.Run("shazam_empty_track_match", func(t *testing.T) {
		client := newMockClient(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString(`{"matches": []}`)),
			}, nil
		})

		realAudioPath := filepath.Join(tempDir, "real_empty.m4a")
		_, _ = defaultRunner(ctx, "ffmpeg", "-v", "quiet", "-hide_banner", "-f", "lavfi", "-i", "anullsrc=r=16000:cl=mono", "-t", "1", "-y", realAudioPath)

		if _, err := os.Stat(realAudioPath); err == nil {
			_, err := client.FetchFromShazam(ctx, realAudioPath)
			if err == nil {
				t.Fatal("expected error for empty shazam matches")
			}
		}
	})
}

func TestResolveTrackMetadataFallback(t *testing.T) {
	// Mock client that returns 404 for AcoustID/Shazam/iTunes and successful MusicBrainz
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

	res := client.ResolveTrackMetadata(context.Background(), "", "Gopnik", "Fallback Artist", "Gopnik", true)
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

func TestResolveTrackMetadata_AllBranches(t *testing.T) {
	tempDir := t.TempDir()
	dummyAudio := filepath.Join(tempDir, "audio.m4a")
	if err := os.WriteFile(dummyAudio, []byte("audio"), 0644); err != nil {
		t.Fatalf("writing audio file: %v", err)
	}

	syntheticPCMLen := 11025 * 2
	syntheticPCM := make([]byte, syntheticPCMLen*2)
	for i := 0; i < syntheticPCMLen; i++ {
		binary.LittleEndian.PutUint16(syntheticPCM[i*2:], uint16(i%1000))
	}

	// 1. Test AcoustID Match branch in ResolveTrackMetadata
	t.Run("acoustid_match_branch", func(t *testing.T) {
		clientAcoustID := newMockClient(func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.Host, "acoustid.org") {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body: io.NopCloser(bytes.NewBufferString(`{
						"status": "ok",
						"results": [
							{
								"score": 0.99,
								"recordings": [
									{
										"id": "mbid-1",
										"title": "AcoustID Title",
										"artists": [{"name": "AcoustID Artist"}],
										"releasegroups": [{"id": "rg-1", "title": "AcoustID Album"}]
									}
								]
							}
						]
					}`)),
				}, nil
			}
			if strings.Contains(req.URL.Host, "coverartarchive.org") {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body: io.NopCloser(bytes.NewBufferString(`{
						"images": [{"image": "https://coverartarchive.org/rg-1.jpg"}]
					}`)),
				}, nil
			}
			return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(bytes.NewBufferString(`{}`))}, nil
		})
		clientAcoustID.SetRunner(func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return syntheticPCM, nil
		})

		res := clientAcoustID.ResolveTrackMetadata(context.Background(), dummyAudio, "AcoustID Title", "Fallback Artist", "AcoustID Title", true)
		if res.Source != "AcoustID / MusicBrainz" {
			t.Errorf("Source = %q, want AcoustID / MusicBrainz", res.Source)
		}
		if res.Artist != "AcoustID Artist" {
			t.Errorf("Artist = %q, want AcoustID Artist", res.Artist)
		}
		if res.CoverArtURL != "https://coverartarchive.org/rg-1.jpg" {
			t.Errorf("CoverArtURL = %q", res.CoverArtURL)
		}
	})

	// 2. Test Shazam Match branch when AcoustID fails
	t.Run("shazam_match_branch", func(t *testing.T) {
		realAudioPath := filepath.Join(tempDir, "real_shazam.m4a")
		_, _ = defaultRunner(context.Background(), "ffmpeg", "-v", "quiet", "-hide_banner", "-f", "lavfi", "-i", "anullsrc=r=16000:cl=mono", "-t", "1", "-y", realAudioPath)

		clientShazam := newMockClient(func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.Host, "shazam.com") {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body: io.NopCloser(bytes.NewBufferString(`{
						"matches": [{"id": "1"}],
						"track": {
							"title": "Shazam Track",
							"subtitle": "Shazam Artist",
							"genres": {"primary": "Electronic"},
							"images": {"coverarthq": "https://images.shazam.com/cover/400x400cc.jpg"}
						}
					}`)),
				}, nil
			}
			return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(bytes.NewBufferString(`{}`))}, nil
		})
		// AcoustID fails
		clientShazam.SetRunner(func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return nil, errors.New("acoustid failed")
		})

		if _, err := os.Stat(realAudioPath); err == nil {
			res := clientShazam.ResolveTrackMetadata(context.Background(), realAudioPath, "Shazam Track", "Fallback Artist", "Shazam Track", true)
			if res.Source != "Shazam API" {
				t.Errorf("Source = %q, want Shazam API", res.Source)
			}
			if res.Artist != "Shazam Artist" {
				t.Errorf("Artist = %q, want Shazam Artist", res.Artist)
			}
			if !strings.Contains(res.CoverArtURL, "1400x1400cc.jpg") {
				t.Errorf("CoverArtURL = %q", res.CoverArtURL)
			}
		}
	})

	// 2b. Test Shazam Match branch when Shazam has no art and falls back to iTunes
	t.Run("shazam_match_no_art_itunes_fallback", func(t *testing.T) {
		realAudioPath := filepath.Join(tempDir, "real_shazam_noart.m4a")
		_, _ = defaultRunner(context.Background(), "ffmpeg", "-v", "quiet", "-hide_banner", "-f", "lavfi", "-i", "anullsrc=r=16000:cl=mono", "-t", "1", "-y", realAudioPath)

		clientShazam := newMockClient(func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.Host, "shazam.com") {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body: io.NopCloser(bytes.NewBufferString(`{
						"matches": [{"id": "1"}],
						"track": {
							"title": "NoArt Track",
							"subtitle": "NoArt Artist"
						}
					}`)),
				}, nil
			}
			if strings.Contains(req.URL.Host, "itunes.apple.com") {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body: io.NopCloser(bytes.NewBufferString(`{
						"results": [
							{
								"trackName": "NoArt Track",
								"artistName": "NoArt Artist",
								"collectionName": "Enriched Album",
								"releaseDate": "2024-06-01",
								"artworkUrl100": "https://img.example.com/100x100bb.jpg"
							}
						]
					}`)),
				}, nil
			}
			return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(bytes.NewBufferString(`{}`))}, nil
		})
		clientShazam.SetRunner(func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return nil, errors.New("acoustid failed")
		})

		if _, err := os.Stat(realAudioPath); err == nil {
			res := clientShazam.ResolveTrackMetadata(context.Background(), realAudioPath, "NoArt Track", "NoArt Artist", "NoArt Track", true)
			if res.Source != "Shazam API" {
				t.Errorf("Source = %q, want Shazam API", res.Source)
			}
			if res.Album != "Enriched Album" {
				t.Errorf("Album = %q, want Enriched Album", res.Album)
			}
			if res.ReleaseYear != "2024" {
				t.Errorf("ReleaseYear = %q, want 2024", res.ReleaseYear)
			}
			if !strings.Contains(res.CoverArtURL, "1400x1400bb.jpg") {
				t.Errorf("CoverArtURL = %q", res.CoverArtURL)
			}
		}
	})

	// 2c. Test AcoustID Match branch when AcoustID has no art and falls back to MusicBrainz
	t.Run("acoustid_match_no_art_musicbrainz_fallback", func(t *testing.T) {
		clientAcoustID := newMockClient(func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.Host, "acoustid.org") {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body: io.NopCloser(bytes.NewBufferString(`{
						"status": "ok",
						"results": [
							{
								"score": 0.99,
								"recordings": [
									{
										"id": "mbid-noart",
										"title": "AcoustID NoArt",
										"artists": [{"name": "AcoustID Artist"}],
										"releasegroups": []
									}
								]
							}
						]
					}`)),
				}, nil
			}
			if strings.Contains(req.URL.Host, "itunes.apple.com") {
				return &http.Response{
					StatusCode: http.StatusNotFound,
					Body:       io.NopCloser(bytes.NewBufferString(`{"results":[]}`)),
				}, nil
			}
			if strings.Contains(req.URL.Host, "musicbrainz.org") {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body: io.NopCloser(bytes.NewBufferString(`{
						"recordings": [
							{
								"title": "AcoustID NoArt",
								"artist-credit": [{"name": "AcoustID Artist"}],
								"releases": [{"id": "rel-mb", "title": "MB Album"}]
							}
						]
					}`)),
				}, nil
			}
			if strings.Contains(req.URL.Host, "coverartarchive.org") {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body: io.NopCloser(bytes.NewBufferString(`{
						"images": [{"image": "https://coverartarchive.org/mb-art.jpg"}]
					}`)),
				}, nil
			}
			return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(bytes.NewBufferString(`{}`))}, nil
		})
		clientAcoustID.SetRunner(func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return syntheticPCM, nil
		})

		res := clientAcoustID.ResolveTrackMetadata(context.Background(), dummyAudio, "AcoustID NoArt", "AcoustID Artist", "AcoustID NoArt", true)
		if res.Source != "AcoustID / MusicBrainz" {
			t.Errorf("Source = %q, want AcoustID / MusicBrainz", res.Source)
		}
		if res.CoverArtURL != "https://coverartarchive.org/mb-art.jpg" {
			t.Errorf("CoverArtURL = %q", res.CoverArtURL)
		}
	})

	// 3. Test iTunes Match branch
	t.Run("itunes_match_branch", func(t *testing.T) {
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

		resITunes := clientITunes.ResolveTrackMetadata(context.Background(), "", "Space X", "Boris Brejcha", "Space X", true)
		if resITunes.Source != "iTunes API" {
			t.Errorf("expected iTunes API source, got %q", resITunes.Source)
		}
	})

	// 4. Test YouTube Fallback branch when all fail
	t.Run("youtube_fallback_branch", func(t *testing.T) {
		clientFallback := newMockClient(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Body:       io.NopCloser(bytes.NewBufferString(`{}`)),
			}, nil
		})

		resFallback := clientFallback.ResolveTrackMetadata(context.Background(), "", "Unknown Track", "Raw Artist", "Unknown Track", true)
		if resFallback.Source != "YouTube Fallback" {
			t.Errorf("expected YouTube Fallback source, got %q", resFallback.Source)
		}
		if resFallback.Artist != "Raw Artist" {
			t.Errorf("Artist = %q, want Raw Artist", resFallback.Artist)
		}

		resEmptyArtist := clientFallback.ResolveTrackMetadata(context.Background(), "", "Fallback Track", "", "Fallback Track", false)
		if resEmptyArtist.Artist != "Unknown Artist" {
			t.Errorf("expected 'Unknown Artist', got %q", resEmptyArtist.Artist)
		}
	})

	// 5. Test Cache Hit branch in ResolveTrackMetadata
	t.Run("cache_hit_branch", func(t *testing.T) {
		clientCache := newMockClient(func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.Host, "itunes") {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body: io.NopCloser(bytes.NewBufferString(`{
						"results": [
							{"trackName": "Space X", "artistName": "Boris Brejcha", "collectionName": "Single", "releaseDate": "2024-01-01"}
						]
					}`)),
				}, nil
			}
			return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(bytes.NewBufferString(`{}`))}, nil
		})

		cacheDir := t.TempDir()
		cacheInst := cache.NewInDir(cacheDir, true)
		clientCache.Cache = cacheInst

		_ = clientCache.ResolveTrackMetadata(context.Background(), "", "Space X", "Boris Brejcha", "Space X", true)
		resCached := clientCache.ResolveTrackMetadata(context.Background(), "", "Space X", "Boris Brejcha", "Space X", true)
		if !strings.Contains(resCached.Source, "[cached]") {
			t.Errorf("expected [cached] source, got %q", resCached.Source)
		}
	})
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

func TestResolveTrackMetadata_ArtFallbacks(t *testing.T) {
	ctx := context.Background()

	t.Run("itunes_art_fallback_when_shazam_has_no_art", func(t *testing.T) {
		tempDir := t.TempDir()
		audioFile := filepath.Join(tempDir, "track.m4a")
		_ = os.WriteFile(audioFile, []byte("dummy audio"), 0644)

		client := newMockClient(func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.String(), "itunes.apple.com") {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body: io.NopCloser(bytes.NewBufferString(`{
						"results": [
							{
								"trackName": "Track",
								"artistName": "Artist",
								"collectionName": "Album Single",
								"releaseDate": "2024-01-01",
								"artworkUrl100": "https://img.example.com/100x100bb.jpg"
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

		res := client.ResolveTrackMetadata(ctx, "", "Track", "Artist", "Track", true)
		if res.Title != "Track" || res.CoverArtURL == "" {
			t.Fatalf("expected iTunes art fallback, got res=%+v", res)
		}
	})

	t.Run("musicbrainz_art_fallback_when_itunes_fails", func(t *testing.T) {
		client := newMockClient(func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.String(), "musicbrainz.org") {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body: io.NopCloser(bytes.NewBufferString(`{
						"recordings": [
							{
								"title": "MB Track",
								"artist-credit": [{"name": "MB Artist"}],
								"releases": [{"id": "rel-1", "title": "MB Album", "date": "2023-05-01"}]
							}
						]
					}`)),
				}, nil
			}
			if strings.Contains(req.URL.String(), "coverartarchive.org") {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body: io.NopCloser(bytes.NewBufferString(`{
						"images": [{"image": "https://coverartarchive.org/art.jpg"}]
					}`)),
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Body:       io.NopCloser(bytes.NewBufferString(`{"results":[]}`)),
			}, nil
		})

		res := client.ResolveTrackMetadata(ctx, "", "MB Track", "", "MB Track", true)
		if res.Title != "MB Track" || res.CoverArtURL == "" {
			t.Fatalf("expected MusicBrainz match and artwork, got %+v", res)
		}
	})
}

