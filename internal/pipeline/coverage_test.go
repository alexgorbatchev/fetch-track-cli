package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dj/fetch-track-cli/internal/deps"
	"github.com/dj/fetch-track-cli/internal/downloader"
)

func TestLoggerMultilineDebug(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "log")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	l := newLogger(true, time.Now(), nil)
	l.output = f
	l.Println("\nfirst\nsecond")
	l.Printf("\nthird %s\n", "value")
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"ms] first", "ms] second", "ms] third value"} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("log = %q, missing %q", b, want)
		}
	}
	if !strings.HasPrefix(string(b), "\n") || strings.Contains(string(b), "ms] \n") {
		t.Fatalf("blank lines = %q", b)
	}
}

func TestRunFingerprintRetry(t *testing.T) {
	for _, accept := range []bool{false, true} {
		t.Run(fmt.Sprintf("accept=%v", accept), func(t *testing.T) {
			_, cleanup := setupTestEnvironment(t)
			defer cleanup()
			outDir := t.TempDir()
			downloads := 0
			restore := downloader.SetDefaultRunner(func(ctx context.Context, name string, args ...string) ([]byte, error) {
				for _, arg := range args {
					if arg == "-o" {
						downloads++
						if err := os.WriteFile(filepath.Join(outDir, "candidate.m4a"), []byte("synthetic audio"), 0600); err != nil {
							return nil, err
						}
						return nil, nil
					}
				}
				return []byte("{\"id\":\"first\",\"title\":\"Artist - Night (Extended Mix)\",\"duration\":400,\"webpage_url\":\"https://example.test/first\"}\n{\"id\":\"second\",\"title\":\"Artist - Night (Original Mix)\",\"duration\":400,\"webpage_url\":\"https://example.test/second\"}\n"), nil
			})
			defer restore()
			opts := Options{OutDir: outDir, Sources: []string{"soundcloud"}, SkipVerify: true, SkipMetadata: true, SkipDepCheck: true, NoCache: true, Verbose: true, ConfirmFingerprint: true}
			opts.Runner = func(context.Context, string, ...string) ([]byte, error) {
				if accept && downloads > 1 {
					return []byte("title: Night\nartist: Artist\nsource: Shazam\n"), nil
				}
				return []byte("title: Other\nartist: Someone\nsource: Shazam\n"), nil
			}
			err := Run(context.Background(), "Artist - Night", opts)
			if downloads != 2 {
				t.Fatalf("downloads = %d, want 2", downloads)
			}
			if accept {
				if err != nil {
					t.Fatal(err)
				}
				if _, err := os.Stat(filepath.Join(outDir, "candidate.m4a")); err != nil {
					t.Fatal(err)
				}
			} else {
				if err == nil || !strings.Contains(err.Error(), "acoustic fingerprint confirmation failed") {
					t.Fatalf("error = %v", err)
				}
				if _, err := os.Stat(filepath.Join(outDir, "candidate.m4a")); !os.IsNotExist(err) {
					t.Fatalf("rejected file remains: %v", err)
				}
			}
		})
	}
}

func TestRunMissingDependencies(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	restore := deps.SetDefaultRunner(func(context.Context, string, ...string) ([]byte, error) { return nil, errors.New("missing") })
	defer restore()
	err := Run(context.Background(), "Artist - Night", Options{IsAgent: true, NoCache: true})
	if err == nil || !strings.Contains(err.Error(), "dependency verification failed") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunMetadataFieldsInAnyOrder(t *testing.T) {
	for _, first := range []string{"artist: Artist", "album: Album", "year: 2026", "source: Shazam", "matched: Artist - Night", "- Source: Shazam"} {
		t.Run(first, func(t *testing.T) {
			_, cleanup := setupTestEnvironment(t)
			defer cleanup()
			outDir := t.TempDir()
			file := filepath.Join(outDir, "candidate.m4a")
			restore := downloader.SetDefaultRunner(func(ctx context.Context, name string, args ...string) ([]byte, error) {
				for _, arg := range args {
					if arg == "-o" {
						return nil, os.WriteFile(file, []byte("audio"), 0600)
					}
				}
				return []byte("{\"id\":\"one\",\"title\":\"Artist - Night (Extended Mix)\",\"duration\":400,\"webpage_url\":\"https://example.test/one\"}\n"), nil
			})
			defer restore()
			opts := Options{OutDir: outDir, Sources: []string{"soundcloud"}, SkipVerify: true, SkipDepCheck: true, NoCache: true, Verbose: true}
			opts.Runner = func(context.Context, string, ...string) ([]byte, error) {
				return []byte(first + "\ntitle: Night\nmatched: Artist - Night\n- source: Shazam\noutput: " + file + "\n"), nil
			}
			output, err := os.CreateTemp(t.TempDir(), "result")
			if err != nil {
				t.Fatal(err)
			}
			defer output.Close()
			old := os.Stdout
			os.Stdout = output
			defer func() { os.Stdout = old }()
			opts.JSONOutput = true
			if err := Run(context.Background(), "Artist - Night", opts); err != nil {
				t.Fatal(err)
			}
			os.Stdout = old
			if _, err := output.Seek(0, 0); err != nil {
				t.Fatal(err)
			}
			var result JSONExecutionResult
			if err := json.NewDecoder(output).Decode(&result); err != nil {
				t.Fatal(err)
			}
			if result.Metadata == nil || result.Metadata.Title != "Night" || result.Metadata.Source != "Shazam" || result.OutputFile != file {
				t.Fatalf("result = %+v", result)
			}
			switch first {
			case "artist: Artist":
				if result.Metadata.Artist != "Artist" {
					t.Fatalf("artist = %q", result.Metadata.Artist)
				}
			case "album: Album":
				if result.Metadata.Album != "Album" {
					t.Fatalf("album = %q", result.Metadata.Album)
				}
			case "year: 2026":
				if result.Metadata.Year != "2026" {
					t.Fatalf("year = %q", result.Metadata.Year)
				}
			}
			if _, err := os.Stat(file); err != nil {
				t.Fatalf("output missing: %v", err)
			}
		})
	}
}
