package downloader

import (
	"context"
	"os"
	"path/filepath"
	"testing"
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

func TestSearchSourcesInParallel_EmptySources(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel context immediately to avoid external yt-dlp execution

	_, err := SearchSourcesInParallel(ctx, []string{}, "Artist", "Title", "Query")
	if err == nil {
		t.Error("expected error for canceled context in search")
	}
}
