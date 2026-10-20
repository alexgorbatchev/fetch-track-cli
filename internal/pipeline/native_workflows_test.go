package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dj/fetch-track-cli/internal/deps"
	"github.com/dj/fetch-track-cli/internal/downloader"
)

func capturePipelineJSON(t *testing.T, query string, opts Options) (JSONExecutionResult, error) {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	previous := os.Stdout
	os.Stdout = f
	defer func() { os.Stdout = previous }()
	err = Run(context.Background(), query, opts)
	if _, seekErr := f.Seek(0, 0); seekErr != nil {
		t.Fatal(seekErr)
	}
	decoder := json.NewDecoder(f)
	var result JSONExecutionResult
	if decodeErr := decoder.Decode(&result); decodeErr != nil {
		t.Fatalf("stdout is not JSON: %v", decodeErr)
	}
	var extra any
	if decodeErr := decoder.Decode(&extra); decodeErr != io.EOF {
		t.Fatalf("unexpected stdout after result: %v (%v)", extra, decodeErr)
	}
	return result, err
}

func TestNativeCandidateSelectionAndFingerprint(t *testing.T) {
	const selected = "https://example.com/selected"
	for _, mode := range []string{"success", "download failure", "fingerprint mismatch"} {
		t.Run(mode, func(t *testing.T) {
			_, cleanup := setupTestEnvironment(t)
			defer cleanup()
			outDir := t.TempDir()
			var attempted []string
			restore := downloader.SetDefaultRunner(func(ctx context.Context, name string, args ...string) ([]byte, error) {
				if slices.Contains(args, "-o") {
					url := args[len(args)-1]
					attempted = append(attempted, url)
					if url != selected {
						t.Fatalf("downloaded unselected URL %q", url)
					}
					if mode == "download failure" {
						return nil, errors.New("selected download unavailable")
					}
					if err := os.WriteFile(filepath.Join(outDir, "Artist - Title.flac"), []byte("native audio bytes"), 0600); err != nil {
						t.Fatal(err)
					}
					return []byte("downloaded"), nil
				}
				return []byte(`{"id":"best","title":"Artist - Title (Extended Mix)","duration":500,"webpage_url":"https://example.com/other"}` + "\n" + `{"id":"chosen","title":"Artist - Title","duration":200,"webpage_url":"https://example.com/selected"}`), nil
			})
			defer restore()
			result, err := capturePipelineJSON(t, "Artist - Title", Options{
				OutDir: outDir, Sources: []string{"soundcloud"}, SkipDepCheck: true, SkipVerify: true, SkipMetadata: true,
				CandidateURL: selected, JSONOutput: true, Verbose: true, Debug: true, ConfirmFingerprint: true,
				Runner: func(ctx context.Context, name string, args ...string) ([]byte, error) {
					if name != "tag-track" || !slices.Contains(args, "--dry-run") {
						t.Fatalf("unexpected fingerprint invocation: %s %v", name, args)
					}
					if mode == "fingerprint mismatch" {
						return []byte("title: Unrelated\nartist: Somebody\nsource: Shazam API\n"), nil
					}
					return []byte("title: Title\nartist: Artist\nsource: Shazam API\n"), nil
				},
			})
			if len(attempted) == 0 {
				t.Fatal("selected candidate was not downloaded")
			}
			if mode != "success" {
				if err == nil || result.Status != "error" || result.Error == "" {
					t.Fatalf("result=%+v err=%v", result, err)
				}
				if mode == "fingerprint mismatch" {
					if _, err := os.Stat(filepath.Join(outDir, "Artist - Title.flac")); !os.IsNotExist(err) {
						t.Fatalf("rejected audio retained: %v", err)
					}
				}
				return
			}
			if err != nil || result.Status != "success" || result.SelectedCandidate == nil || result.SelectedCandidate.WebpageURL != selected || result.CandidateCount != 1 {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			data, err := os.ReadFile(result.OutputFile)
			if err != nil || string(data) != "native audio bytes" {
				t.Fatalf("audio changed: %q %v", data, err)
			}
		})
	}
}

func TestJSONTagMetadataVariantsAndFailure(t *testing.T) {
	for _, first := range []string{"artist: Artist", "album: Album", "year: 2026", "source: Provider", "- Matched: Artist - Title", "- Source: Provider", "failure"} {
		t.Run(first, func(t *testing.T) {
			_, cleanup := setupTestEnvironment(t)
			defer cleanup()
			outDir := t.TempDir()
			file := filepath.Join(outDir, "Artist - Title.flac")
			restore := downloader.SetDefaultRunner(func(ctx context.Context, name string, args ...string) ([]byte, error) {
				if slices.Contains(args, "-o") {
					return nil, os.WriteFile(file, []byte("audio"), 0600)
				}
				return []byte(`{"id":"one","title":"Artist - Title","duration":300,"webpage_url":"https://example.com/one"}`), nil
			})
			defer restore()
			result, err := capturePipelineJSON(t, "Artist - Title", Options{OutDir: outDir, Sources: []string{"soundcloud"}, SkipDepCheck: true, SkipVerify: true, JSONOutput: true, NoCache: true, Verbose: true, ProgressTarget: "tcp://127.0.0.1:12345", Runner: func(ctx context.Context, name string, args ...string) ([]byte, error) {
				for _, flag := range []string{"track", "update", "--in-place", "--source-url", "--no-cache", "-v", "--progress-target"} {
					if !slices.Contains(args, flag) {
						t.Fatalf("missing %s: %v", flag, args)
					}
				}
				if first == "failure" {
					return nil, errors.New("tagging unavailable")
				}
				return []byte(first + "\ntitle: Title\nDONE: " + file + "\n"), nil
			}})
			if err != nil || result.Status != "success" || result.OutputFile != file {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if first == "failure" {
				if result.Metadata != nil {
					t.Fatalf("invented metadata: %+v", result.Metadata)
				}
				return
			}
			if result.Metadata == nil || result.Metadata.Title != "Title" {
				t.Fatalf("metadata=%+v", result.Metadata)
			}
			fields := map[string]string{"artist: Artist": result.Metadata.Artist, "album: Album": result.Metadata.Album, "year: 2026": result.Metadata.Year, "source: Provider": result.Metadata.Source, "- Source: Provider": result.Metadata.Source}
			if value, ok := fields[first]; ok && value != strings.TrimSpace(strings.SplitN(first, ":", 2)[1]) {
				t.Fatalf("first metadata field %q was lost: %+v", first, result.Metadata)
			}
		})
	}
}

func TestJSONDependencyFailureHasOneResult(t *testing.T) {
	_, cleanup := setupTestEnvironment(t)
	defer cleanup()
	restore := deps.SetDefaultRunner(func(context.Context, string, ...string) ([]byte, error) {
		return nil, errors.New("dependency unavailable")
	})
	defer restore()
	result, err := capturePipelineJSON(t, "Artist - Title", Options{JSONOutput: true, NoCache: true})
	if err == nil || result.Status != "error" || !strings.Contains(result.Error, "dependency verification failed") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}
