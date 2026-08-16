package metadata

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func createDummyAudioFile(t *testing.T, dir, filename string) string {
	t.Helper()
	filePath := filepath.Join(dir, filename)
	cmd := exec.Command("ffmpeg", "-v", "quiet", "-hide_banner", "-f", "lavfi", "-i", "anullsrc=r=48000:cl=stereo", "-t", "1", "-y", filePath)
	if err := cmd.Run(); err != nil {
		t.Skipf("ffmpeg not available or failed to create dummy audio file: %v", err)
	}
	return filePath
}

func TestApplyMetadataToLocalTrack_ZeroTranscodePreservesNativeFormat(t *testing.T) {
	outDir := t.TempDir()
	srcDir := t.TempDir()

	tests := []struct {
		name          string
		inputFilename string
		meta          TrackMetadataResult
		wantFileName  string
		wantExt       string
	}{
		{
			name:          "mp3 input stream copied as mp3",
			inputFilename: "Gopnik.mp3",
			meta: TrackMetadataResult{
				Title:       "Gopnik",
				Artist:      "DJ Blyatman",
				Album:       "Gopnik - Single",
				ReleaseYear: "2020",
				Genre:       "Hardbass",
				Source:      "iTunes API",
			},
			wantFileName: "DJ Blyatman - Gopnik.mp3",
			wantExt:      ".mp3",
		},
		{
			name:          "m4a input stream copied as m4a",
			inputFilename: "Space X.m4a",
			meta: TrackMetadataResult{
				Title:       "Space X",
				Artist:      "Boris Brejcha",
				Album:       "Space X - Single",
				ReleaseYear: "2024",
				Genre:       "Minimal Techno",
				Source:      "iTunes API",
			},
			wantFileName: "Boris Brejcha - Space X.m4a",
			wantExt:      ".m4a",
		},
		{
			name:          "unknown artist falls back to clean filename with native extension",
			inputFilename: "Raw Track.mp3",
			meta: TrackMetadataResult{
				Title:  "Raw Track",
				Artist: "Unknown Artist",
				Source: "YouTube Raw Fallback",
			},
			wantFileName: "Raw Track.mp3",
			wantExt:      ".mp3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inputPath := createDummyAudioFile(t, srcDir, tt.inputFilename)

			taggedPath, err := ApplyMetadataToLocalTrack(context.Background(), inputPath, tt.meta, outDir)
			if err != nil {
				t.Fatalf("ApplyMetadataToLocalTrack failed: %v", err)
			}

			if filepath.Base(taggedPath) != tt.wantFileName {
				t.Errorf("ApplyMetadataToLocalTrack output filename = %q, want %q", filepath.Base(taggedPath), tt.wantFileName)
			}

			if !strings.HasSuffix(taggedPath, tt.wantExt) {
				t.Errorf("ApplyMetadataToLocalTrack file extension is not %s: %q", tt.wantExt, taggedPath)
			}

			if _, err := os.Stat(taggedPath); os.IsNotExist(err) {
				t.Errorf("tagged output file does not exist on disk: %s", taggedPath)
			}
		})
	}
}

func TestApplyMetadataToLocalTrack_TmpCleanup(t *testing.T) {
	outDir := t.TempDir()
	srcDir := t.TempDir()

	inputPath := createDummyAudioFile(t, srcDir, "CleanupTest.mp3")

	meta := TrackMetadataResult{
		Title:  "Cleanup Test",
		Artist: "Test Artist",
		Source: "Test",
	}

	// Ensure .tmp state before test
	dotTmpPath := ".tmp"
	_, errStat := os.Stat(dotTmpPath)
	existedBefore := !os.IsNotExist(errStat)

	_, err := ApplyMetadataToLocalTrack(context.Background(), inputPath, meta, outDir)
	if err != nil {
		t.Fatalf("ApplyMetadataToLocalTrack failed: %v", err)
	}

	// If .tmp did not exist before the test, it should not exist after
	if !existedBefore {
		if _, err := os.Stat(dotTmpPath); !os.IsNotExist(err) {
			t.Errorf(".tmp directory was created during operation but was not cleaned up")
		}
	}
}
