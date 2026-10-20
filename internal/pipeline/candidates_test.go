package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"testing"

	"github.com/dj/fetch-track-cli/internal/downloader"
)

func TestListCandidatesDoesNotDownload(t *testing.T) {
	_, cleanup := setupTestEnvironment(t)
	defer cleanup()
	restore := downloader.SetDefaultRunner(func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if slices.Contains(args, "-o") {
			t.Error("candidate discovery downloaded audio")
		}
		return []byte(`{"id":"one","title":"Artist - Title","duration":200,"webpage_url":"https://example.com/one"}`), nil
	})
	defer restore()
	output, err := os.CreateTemp(t.TempDir(), "result")
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	previous := os.Stdout
	os.Stdout = output
	defer func() { os.Stdout = previous }()
	if err := Run(context.Background(), "Artist - Title", Options{Sources: []string{"soundcloud"}, ListCandidates: true, SkipDepCheck: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := output.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	var result JSONExecutionResult
	if err := json.NewDecoder(output).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if len(result.Candidates) != 1 || result.Candidates[0].WebpageURL != "https://example.com/one" {
		t.Fatalf("result: %+v", result)
	}
}

func TestCandidatesJSONFailures(t *testing.T) {
	for _, tc := range []struct {
		name      string
		searchErr error
		selected  string
		list      bool
	}{
		{"search fails", errors.New("search unavailable"), "", true},
		{"selection disappeared", nil, "https://example.com/missing", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, cleanup := setupTestEnvironment(t)
			defer cleanup()
			restore := downloader.SetDefaultRunner(func(ctx context.Context, name string, args ...string) ([]byte, error) {
				if slices.Contains(args, "-o") {
					t.Error("download attempted despite unavailable selection")
					return nil, errors.New("unexpected download")
				}
				return []byte(`{"id":"one","title":"Artist - Title","duration":200,"webpage_url":"https://example.com/one"}`), tc.searchErr
			})
			defer restore()
			output, err := os.CreateTemp(t.TempDir(), "json")
			if err != nil {
				t.Fatal(err)
			}
			defer output.Close()
			previous := os.Stdout
			os.Stdout = output
			defer func() { os.Stdout = previous }()
			err = Run(context.Background(), "Artist - Title", Options{Sources: []string{"soundcloud"}, ListCandidates: tc.list, CandidateURL: tc.selected, JSONOutput: true, SkipDepCheck: true})
			if err == nil {
				t.Fatal("expected error")
			}
			if _, err := output.Seek(0, 0); err != nil {
				t.Fatal(err)
			}
			var result JSONExecutionResult
			if err := json.NewDecoder(output).Decode(&result); err != nil {
				t.Fatal(err)
			}
			if result.Status != "error" || result.Error == "" {
				t.Fatalf("result: %+v", result)
			}
		})
	}
}
