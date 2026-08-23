package downloader

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dj/fetch-track-cli/internal/cache"
)

func TestMapSourceSearchPrefix(t *testing.T) {
	tests := []struct {
		source string
		want   string
	}{
		{"youtube", "ytsearch8"},
		{"YouTube", "ytsearch8"},
		{"soundcloud", "scsearch8"},
		{"SoundCloud", "scsearch8"},
		{"bandcamp", "bcsearch8"},
		{"unknown", "ytsearch8"},
		{"  ", "ytsearch8"},
	}

	for _, tt := range tests {
		got := MapSourceSearchPrefix(tt.source)
		if got != tt.want {
			t.Errorf("MapSourceSearchPrefix(%q) = %q, want %q", tt.source, got, tt.want)
		}
	}
}

func TestListAudioFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Create dummy audio and non-audio files
	filesToCreate := []string{
		"track1.m4a",
		"track2.mp3",
		"track3.opus",
		"track4.flac",
		"readme.txt",
		"image.jpg",
	}

	for _, name := range filesToCreate {
		if err := os.WriteFile(filepath.Join(tmpDir, name), []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create dummy file %s: %v", name, err)
		}
	}

	// Create a subdirectory to ensure it's ignored
	if err := os.Mkdir(filepath.Join(tmpDir, "subdir"), 0755); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}

	audioFiles, err := listAudioFiles(tmpDir)
	if err != nil {
		t.Fatalf("listAudioFiles failed: %v", err)
	}

	wantAudioFiles := map[string]bool{
		"track1.m4a":  true,
		"track2.mp3":  true,
		"track3.opus": true,
		"track4.flac": true,
	}

	if len(audioFiles) != len(wantAudioFiles) {
		t.Errorf("listAudioFiles returned %d files, want %d", len(audioFiles), len(wantAudioFiles))
	}

	for f := range wantAudioFiles {
		if !audioFiles[f] {
			t.Errorf("expected file %s in listAudioFiles result", f)
		}
	}

	if audioFiles["readme.txt"] || audioFiles["image.jpg"] {
		t.Errorf("non-audio file was included in listAudioFiles result")
	}
}

func TestEvaluateAndInspectCandidatesInParallel_Empty(t *testing.T) {
	_, err := EvaluateAndInspectCandidatesInParallel(context.Background(), nil, "Artist", "Title")
	if err == nil || err != ErrNoCandidateFound {
		t.Errorf("expected ErrNoCandidateFound for empty candidates, got %v", err)
	}
}

func TestDeduplicateCandidates(t *testing.T) {
	candidates := []Candidate{
		{ID: "1", Title: "DJ Blyatman - Gopnik", Duration: 192, Source: "soundcloud", WebpageURL: "https://soundcloud.com/djblyatman/gopnik"},
		{ID: "1", Title: "DJ Blyatman - Gopnik", Duration: 192, Source: "soundcloud", WebpageURL: "https://soundcloud.com/djblyatman/gopnik"},       // Exact URL duplicate
		{ID: "2", Title: "DJ Blyatman - Gopnik", Duration: 192, Source: "soundcloud", WebpageURL: "https://soundcloud.com/djblyatman/gopnik-track"}, // Title + Source + Duration duplicate
		{ID: "3", Title: "DJ BLYATMAN - GOPNIK (Official Audio)", Duration: 193, Source: "youtube", WebpageURL: "https://youtube.com/watch?v=3"},    // Unique candidate
	}

	unique := DeduplicateCandidates(candidates)
	if len(unique) != 2 {
		t.Fatalf("expected 2 unique candidates, got %d", len(unique))
	}

	if unique[0].ID != "1" || unique[1].ID != "3" {
		t.Errorf("unexpected unique candidate IDs: %+v", unique)
	}
}

func TestSearchSourcesInParallelWithRunner(t *testing.T) {
	ctx := context.Background()

	jsonLine1 := `{"id":"v1","title":"Boris Brejcha - Space X (Extended Mix)","duration":503,"webpage_url":"https://soundcloud.com/boris-brejcha/space-x"}`
	jsonLine2 := `{"id":"v2","title":"Boris Brejcha - Space X (Radio Edit)","duration":195,"webpage_url":"https://youtube.com/watch?v=v2"}`
	jsonShort := `{"id":"v3","title":"Short Snippet","duration":30,"webpage_url":"https://youtube.com/watch?v=v3"}`
	jsonLong := `{"id":"v4","title":"Continuous Full Album","duration":3600,"webpage_url":"https://youtube.com/watch?v=v4"}`

	mockOutput := jsonLine1 + "\n" + jsonLine2 + "\n" + jsonShort + "\n" + jsonLong + "\n"

	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte(mockOutput), nil
	}

	tempDir := t.TempDir()
	c := cache.NewInDir(tempDir, true)

	// 1. Initial search
	results, err := SearchSourcesInParallelWithRunner(ctx, runner, []string{"youtube", "soundcloud"}, "Boris Brejcha", "Space X", "Boris Brejcha Space X", c, true)
	if err != nil {
		t.Fatalf("unexpected search error: %v", err)
	}
	if len(results) != 4 {
		t.Errorf("expected 4 valid filtered candidates, got %d", len(results))
	}

	// 2. Cached search hit
	cachedResults, err := SearchSourcesInParallelWithRunner(ctx, nil, []string{"youtube", "soundcloud"}, "Boris Brejcha", "Space X", "Boris Brejcha Space X", c, false)
	if err != nil {
		t.Fatalf("unexpected cached search error: %v", err)
	}
	if len(cachedResults) != 4 {
		t.Errorf("expected 4 cached candidates, got %d", len(cachedResults))
	}

	// 3. Empty results error
	emptyRunner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte(""), nil
	}
	_, err = SearchSourcesInParallelWithRunner(ctx, emptyRunner, []string{"youtube"}, "Unknown", "Song", "Unknown Song", nil, false)
	if err == nil || !errors.Is(err, ErrNoCandidateFound) {
		t.Errorf("expected ErrNoCandidateFound, got: %v", err)
	}
}

func TestDownloadAudioStreamWithRunner(t *testing.T) {
	ctx := context.Background()
	outDir := t.TempDir()

	t.Run("successful_download", func(t *testing.T) {
		runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
			// Create the expected output file
			createdFile := filepath.Join(outDir, "DownloadedTrack.m4a")
			if err := os.WriteFile(createdFile, []byte("audio"), 0644); err != nil {
				return nil, err
			}
			return []byte("downloaded"), nil
		}

		path, err := DownloadAudioStreamWithRunner(ctx, runner, "https://soundcloud.com/test/track", outDir, true)
		if err != nil {
			t.Fatalf("unexpected download error: %v", err)
		}
		if filepath.Base(path) != "DownloadedTrack.m4a" {
			t.Errorf("expected DownloadedTrack.m4a, got %q", path)
		}
	})

	t.Run("all_retries_fail", func(t *testing.T) {
		failRunner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return nil, errors.New("yt-dlp 403 forbidden")
		}

		_, err := DownloadAudioStreamWithRunner(ctx, failRunner, "https://youtube.com/watch?v=err", outDir, true)
		if err == nil || !errors.Is(err, ErrDownloadFailed) {
			t.Errorf("expected ErrDownloadFailed, got: %v", err)
		}
	})
}

func TestSearchAndSelectBestCandidate(t *testing.T) {
	ctx := context.Background()

	jsonLine1 := `{"id":"1","title":"Space X (Extended Mix)","duration":503,"webpage_url":"https://soundcloud.com/1"}`
	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte(jsonLine1), nil
	}

	selected, err := SearchAndSelectBestCandidateWithRunner(ctx, runner, []string{"soundcloud"}, "Boris Brejcha", "Space X", "Boris Brejcha Space X")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if selected == nil || selected.ID != "1" {
		t.Errorf("unexpected selected candidate: %+v", selected)
	}

	ytURL, err := SearchYouTubeCandidatesWithRunner(ctx, runner, "Boris Brejcha", "Space X", "Boris Brejcha Space X")
	if err != nil {
		t.Fatalf("unexpected youtube error: %v", err)
	}
	if ytURL != "https://soundcloud.com/1" {
		t.Errorf("unexpected url: %q", ytURL)
	}
}

func TestRankYouTubeCandidates(t *testing.T) {
	cands := []Candidate{
		{ID: "1", Title: "Space X (Extended Mix)", Duration: 503, Source: "youtube", Score: 150},
	}
	res := RankYouTubeCandidates(cands, "Boris Brejcha", "Space X")
	if res == nil || res.ID != "1" {
		t.Errorf("expected candidate 1, got %+v", res)
	}
}

func TestDownloader_AdditionalErrorBranches(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()

	// 1. defaultRunner error with stderr
	_, err := defaultRunner(ctx, "sh", "-c", "echo error message >&2; exit 1")
	if err == nil {
		t.Fatal("expected error from defaultRunner")
	}

	// 1b. defaultRunner error with python traceback
	_, err = defaultRunner(ctx, "sh", "-c", "echo 'Traceback (most recent call last):\n  File \"yt_dlp.py\", line 1\nRuntimeError: download fail' >&2; exit 1")
	if err == nil {
		t.Fatal("expected error from defaultRunner")
	}
	if strings.Contains(err.Error(), "Traceback") || strings.Contains(err.Error(), "File \"") {
		t.Errorf("expected stack trace to be stripped from defaultRunner error, got: %v", err)
	}

	// 2. DownloadAudioStreamWithRunner mkdir error
	blockerFile := filepath.Join(tempDir, "blocker")
	_ = os.WriteFile(blockerFile, []byte("file"), 0644)
	_, err = DownloadAudioStreamWithRunner(ctx, defaultRunner, "https://test", blockerFile)
	if err == nil {
		t.Fatal("expected error when output dir cannot be created")
	}

	// 3. SearchAndSelectBestCandidateWithRunner search error
	failRunner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte(""), nil
	}
	_, err = SearchAndSelectBestCandidateWithRunner(ctx, failRunner, []string{"youtube"}, "Artist", "Title", "Query")
	if err == nil {
		t.Fatal("expected error on search failure")
	}

	// 4. SearchYouTubeCandidatesWithRunner error
	_, err = SearchYouTubeCandidatesWithRunner(ctx, failRunner, "Artist", "Title", "Query")
	if err == nil {
		t.Fatal("expected error on youtube search failure")
	}

	// 5. SearchSourcesInParallel candidate with only ID (no webpage_url)
	idOnlyJSON := `{"id":"yt123","title":"Artist - Title (Extended Mix)","duration":300}` + "\n"
	idRunner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte(idOnlyJSON), nil
	}
	cands, err := SearchSourcesInParallelWithRunner(ctx, idRunner, []string{"youtube", "soundcloud"}, "Artist", "Title", "Artist Title", nil)
	if err != nil || len(cands) == 0 {
		t.Fatalf("unexpected error searching ID-only candidates: %v", err)
	}
}

func TestConvenienceFunctions(t *testing.T) {
	ctx := context.Background()
	outDir := t.TempDir()

	jsonLine := `{"id":"1","title":"Space X (Extended Mix)","duration":503,"webpage_url":"https://soundcloud.com/1"}`
	mockRunner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		// If download call
		for _, a := range args {
			if a == "-o" {
				createdFile := filepath.Join(outDir, "Track.m4a")
				_ = os.WriteFile(createdFile, []byte("audio"), 0644)
				return []byte("downloaded"), nil
			}
		}
		return []byte(jsonLine), nil
	}

	cleanup := SetDefaultRunner(mockRunner)
	defer cleanup()

	// Test SearchSourcesInParallel
	results, err := SearchSourcesInParallel(ctx, []string{"soundcloud"}, "Boris Brejcha", "Space X", "Boris Brejcha Space X", nil)
	if err != nil || len(results) == 0 {
		t.Fatalf("SearchSourcesInParallel failed: %v", err)
	}

	// Test SearchAndSelectBestCandidate
	best, err := SearchAndSelectBestCandidate(ctx, []string{"soundcloud"}, "Boris Brejcha", "Space X", "Boris Brejcha Space X")
	if err != nil || best == nil {
		t.Fatalf("SearchAndSelectBestCandidate failed: %v", err)
	}

	// Test SearchYouTubeCandidates
	ytURL, err := SearchYouTubeCandidates(ctx, "Boris Brejcha", "Space X", "Boris Brejcha Space X")
	if err != nil || ytURL == "" {
		t.Fatalf("SearchYouTubeCandidates failed: %v", err)
	}

	// Test DownloadAudioStream
	downloaded, err := DownloadAudioStream(ctx, "https://soundcloud.com/1", outDir, true)
	if err != nil || downloaded == "" {
		t.Fatalf("DownloadAudioStream failed: %v", err)
	}
}

func TestDefaultRunner_Downloader(t *testing.T) {
	ctx := context.Background()
	_, _ = defaultRunner(ctx, "echo", "test")
}
