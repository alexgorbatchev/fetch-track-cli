package metadata

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dj/fetch-track-cli/internal/cache"
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

func TestApplyMetadataToLocalTrack_PreservesOriginDateAndProvenance(t *testing.T) {
	outDir := t.TempDir()
	srcDir := t.TempDir()

	testFormats := []struct {
		filename string
	}{
		{filename: "test_provenance.m4a"},
		{filename: "test_provenance.mp3"},
	}

	for _, tf := range testFormats {
		t.Run(tf.filename, func(t *testing.T) {
			inputPath := createDummyAudioFile(t, srcDir, tf.filename)

			meta := TrackMetadataResult{
				Title:          "Space X",
				Artist:         "Boris Brejcha",
				Album:          "Space X - Single",
				Genre:          "Minimal Techno",
				ReleaseDate:    "2024-05-10",
				ReleaseYear:    "2024",
				Source:         "iTunes API",
				AudioSourceURL: "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
				FetchedAt:      time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC),
			}

			taggedPath, err := ApplyMetadataToLocalTrack(context.Background(), inputPath, meta, outDir)
			if err != nil {
				t.Fatalf("ApplyMetadataToLocalTrack failed: %v", err)
			}

			cmd := exec.Command("ffprobe", "-v", "quiet", "-print_format", "json", "-show_format", taggedPath)
			var stdout bytes.Buffer
			cmd.Stdout = &stdout
			if err := cmd.Run(); err != nil {
				t.Fatalf("ffprobe failed: %v", err)
			}

			var probeData struct {
				Format struct {
					Tags map[string]string `json:"tags"`
				} `json:"format"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &probeData); err != nil {
				t.Fatalf("unmarshaling ffprobe json: %v", err)
			}

			tags := probeData.Format.Tags
			// Lowercase all tag keys for format-agnostic lookup (e.g. ID3 vs MP4 atom names)
			normTags := make(map[string]string)
			for k, v := range tags {
				normTags[strings.ToLower(k)] = v
			}

			dateVal := normTags["date"]
			if dateVal == "" {
				dateVal = normTags["year"]
			}
			if dateVal != "2024-05-10" && dateVal != "2024" {
				t.Errorf("expected date tag 2024-05-10 or 2024, got %q (tags: %v)", dateVal, tags)
			}

			commentVal := normTags["comment"]
			if commentVal == "" {
				commentVal = normTags["description"]
			}
			if !strings.Contains(commentVal, "Source: https://www.youtube.com/watch?v=dQw4w9WgXcQ") {
				t.Errorf("comment missing audio source URL: %q", commentVal)
			}
			if !strings.Contains(commentVal, "Metadata: iTunes API") {
				t.Errorf("comment missing metadata provider: %q", commentVal)
			}
			if !strings.Contains(commentVal, "Fetched: 2026-08-23") {
				t.Errorf("comment missing fetched date: %q", commentVal)
			}
		})
	}
}

func TestApplyMetadataToLocalTrack_WithCoverArt(t *testing.T) {
	outDir := t.TempDir()
	srcDir := t.TempDir()

	// Create a dummy image file for mock cover art HTTP server
	dummyImg := []byte("\xff\xd8\xff\xe0\x00\x10JFIF\x00\x01\x01\x01\x00`\x00`\x00\x00\xff\xdb\x00C\x00\x08\x06\x06\x07\x06\x05\x08\x07\x07\x07\t\t\x08\n\x0c\x14\r\x0c\x0b\x0b\x0c\x19\x12\x13\x0f\x14\x1d\x1a\x1f\x1e\x1d\x1a\x1c\x1c $.' \",#\x1c\x1c(7),01444\x1f'9=82<.342\xff\xc0\x00\x0b\x08\x00\x01\x00\x01\x01\x01\x11\x00\xff\xc4\x00\x1f\x00\x00\x01\x05\x01\x01\x01\x01\x01\x01\x00\x00\x00\x00\x00\x00\x00\x00\x01\x02\x03\x04\x05\x06\x07\x08\t\n\x0b\xff\xda\x00\x08\x01\x01\x00\x00?\x00\xbf\x00\xff\xd9")
	cmdImg := exec.Command("ffmpeg", "-v", "quiet", "-hide_banner", "-f", "lavfi", "-i", "color=c=blue:s=100x100", "-frames:v", "1", "-y", filepath.Join(srcDir, "cover.jpg"))
	if err := cmdImg.Run(); err == nil {
		if data, errRead := os.ReadFile(filepath.Join(srcDir, "cover.jpg")); errRead == nil {
			dummyImg = data
		}
	}

	execCmd := exec.Command("ffmpeg", "-v", "quiet", "-hide_banner", "-f", "lavfi", "-i", "color=c=blue:s=100x100", "-frames:v", "1", "-y", filepath.Join(srcDir, "cover.jpg"))
	_ = execCmd.Run()

	inputPath := createDummyAudioFile(t, srcDir, "CoverTrack.m4a")

	meta := TrackMetadataResult{
		Title:          "Cover Track",
		Artist:         "Cover Artist",
		Album:          "Cover Album",
		ReleaseDate:    "2025-01-01",
		ReleaseYear:    "2025",
		Source:         "iTunes API",
		AudioSourceURL: "https://www.youtube.com/watch?v=12345",
	}

	// 1. Without cover art URL
	taggedPath, err := ApplyMetadataToLocalTrack(context.Background(), inputPath, meta, outDir, true)
	if err != nil {
		t.Fatalf("ApplyMetadataToLocalTrack failed: %v", err)
	}
	if _, err := os.Stat(taggedPath); os.IsNotExist(err) {
		t.Fatalf("tagged file does not exist: %s", taggedPath)
	}

	// 2. With mock cover art HTTP server & cache
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(dummyImg)
	}))
	defer ts.Close()

	cacheDir := t.TempDir()
	c := cache.NewInDir(cacheDir, true)

	metaWithCover := meta
	metaWithCover.CoverArtURL = ts.URL + "/cover.jpg"

	inputPath2 := createDummyAudioFile(t, srcDir, "CoverTrack2.m4a")
	taggedPath2, err := ApplyMetadataToLocalTrack(context.Background(), inputPath2, metaWithCover, outDir, true, c)
	if err != nil {
		t.Fatalf("ApplyMetadataToLocalTrack with cover failed: %v", err)
	}
	if _, err := os.Stat(taggedPath2); os.IsNotExist(err) {
		t.Fatalf("tagged file with cover does not exist: %s", taggedPath2)
	}

	// 4. Invalid input file error
	_, err = ApplyMetadataToLocalTrack(context.Background(), "/nonexistent/invalid/file.m4a", meta, outDir)
	if err == nil {
		t.Error("expected error for nonexistent input file")
	}

	// 5. Canceled context error
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	inputPath4 := createDummyAudioFile(t, srcDir, "CoverTrack4.m4a")
	_, err = ApplyMetadataToLocalTrack(canceledCtx, inputPath4, meta, outDir)
	if err == nil {
		t.Error("expected error for canceled context")
	}
}

func TestNormalizeCoverArtToSquare(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()

	t.Run("empty_path", func(t *testing.T) {
		_, err := NormalizeCoverArtToSquare(ctx, "", tempDir)
		if err == nil {
			t.Fatal("expected error for empty image path")
		}
	})

	t.Run("nonexistent_path", func(t *testing.T) {
		_, err := NormalizeCoverArtToSquare(ctx, filepath.Join(tempDir, "nonexistent.jpg"), tempDir)
		if err == nil {
			t.Fatal("expected error for nonexistent image path")
		}
	})

	t.Run("crops_16_9_to_1_1_square", func(t *testing.T) {
		// Generate 1280x720 16:9 test image
		input16x9 := filepath.Join(tempDir, "test_16_9.jpg")
		genCmd := exec.CommandContext(ctx, "ffmpeg", "-v", "quiet", "-hide_banner", "-y", "-f", "lavfi", "-i", "color=c=red:s=1280x720", "-frames:v", "1", input16x9)
		if err := genCmd.Run(); err != nil {
			t.Skipf("ffmpeg not available: %v", err)
		}

		squarePath, err := NormalizeCoverArtToSquare(ctx, input16x9, tempDir)
		if err != nil {
			t.Fatalf("NormalizeCoverArtToSquare error: %v", err)
		}
		if squarePath == input16x9 {
			t.Fatalf("expected new square file, got original path %s", squarePath)
		}

		// Verify square dimensions using ffprobe
		probeCmd := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-show_entries", "stream=width,height", "-of", "json", squarePath)
		out, err := probeCmd.Output()
		if err != nil {
			t.Fatalf("ffprobe error: %v", err)
		}

		var data struct {
			Streams []struct {
				Width  int `json:"width"`
				Height int `json:"height"`
			} `json:"streams"`
		}
		if err := json.Unmarshal(out, &data); err != nil || len(data.Streams) == 0 {
			t.Fatalf("parsing ffprobe json: %v", err)
		}

		if data.Streams[0].Width != 1400 || data.Streams[0].Height != 1400 {
			t.Errorf("expected 1400x1400 dimensions, got %dx%d", data.Streams[0].Width, data.Streams[0].Height)
		}
	})
}
