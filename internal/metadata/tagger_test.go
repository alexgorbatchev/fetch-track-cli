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

func TestApplyMetadataToLocalTrack_ForcesM4AFormat(t *testing.T) {
	outDir := t.TempDir()
	srcDir := t.TempDir()

	tests := []struct {
		name          string
		inputFilename string
		meta          TrackMetadataResult
		wantFileName  string
	}{
		{
			name:          "mp3 input converted to m4a",
			inputFilename: "Gopnik.mp3",
			meta: TrackMetadataResult{
				Title:       "Gopnik",
				Artist:      "DJ Blyatman",
				Album:       "Gopnik - Single",
				ReleaseYear: "2020",
				Genre:       "Hardbass",
				Source:      "iTunes API",
			},
			wantFileName: "DJ Blyatman - Gopnik.m4a",
		},
		{
			name:          "m4a input stream copied to m4a",
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
		},
		{
			name:          "unknown artist falls back to clean filename with m4a",
			inputFilename: "Raw Track.mp3",
			meta: TrackMetadataResult{
				Title:  "Raw Track",
				Artist: "Unknown Artist",
				Source: "YouTube Raw Fallback",
			},
			wantFileName: "Raw Track.m4a",
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

			if !strings.HasSuffix(taggedPath, ".m4a") {
				t.Errorf("ApplyMetadataToLocalTrack file extension is not .m4a: %q", taggedPath)
			}

			if _, err := os.Stat(taggedPath); os.IsNotExist(err) {
				t.Errorf("tagged output file does not exist on disk: %s", taggedPath)
			}

			// Verify source non-m4a file was cleaned up if input was different from output
			if inputPath != taggedPath {
				if _, err := os.Stat(inputPath); !os.IsNotExist(err) {
					t.Errorf("original input file %s was not cleaned up after format conversion", inputPath)
				}
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
