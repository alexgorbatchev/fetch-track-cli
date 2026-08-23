package verifier

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dj/fetch-track-cli/internal/cache"
)

func createTestAudioFile(t *testing.T, filename string) string {
	t.Helper()
	dir := t.TempDir()
	filePath := filepath.Join(dir, filename)
	cmd := exec.Command("ffmpeg", "-v", "quiet", "-hide_banner", "-f", "lavfi", "-i", "anullsrc=r=48000:cl=stereo", "-t", "2", "-y", filePath)
	if err := cmd.Run(); err != nil {
		t.Skipf("ffmpeg not available or failed to create test audio file: %v", err)
	}
	return filePath
}

func TestIsYouTubeURL(t *testing.T) {
	tests := []struct {
		url  string
		want bool
	}{
		{"https://www.youtube.com/watch?v=abc12345678", true},
		{"https://youtu.be/abc12345678", true},
		{"https://soundcloud.com/artist/track", true},
		{"https://bandcamp.com/artist/track", true},
		{"invalid_string", false},
	}

	for _, tt := range tests {
		got := IsYouTubeURL(tt.url)
		if got != tt.want {
			t.Errorf("IsYouTubeURL(%q) = %v, want %v", tt.url, got, tt.want)
		}
	}
}

func TestAnalyzePCMAudio_RealAudioFile(t *testing.T) {
	audioFile := createTestAudioFile(t, "test_pcm.m4a")

	report, err := AnalyzePCMAudio(context.Background(), audioFile, 2.0)
	if err != nil {
		t.Fatalf("AnalyzePCMAudio failed: %v", err)
	}

	if report == nil {
		t.Fatal("expected non-nil AudioQualityReport")
	}

	if report.EstimatedBandwidthHz <= 0 {
		t.Errorf("EstimatedBandwidthHz = %d, want > 0", report.EstimatedBandwidthHz)
	}
}

func TestAnalyzePCMAudio_Errors(t *testing.T) {
	ctx := context.Background()

	// Nonexistent file
	_, err := AnalyzePCMAudio(ctx, "/nonexistent/path/test.m4a", 2.0)
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestVerifyAudioTrack_LocalFile(t *testing.T) {
	audioFile := createTestAudioFile(t, "Boris Brejcha - Space X (Extended Mix).m4a")

	report, err := VerifyAudioTrack(context.Background(), audioFile, true)
	if err != nil {
		t.Fatalf("VerifyAudioTrack failed for local file: %v", err)
	}

	if report.Metadata.Title == "" {
		t.Error("expected non-empty Title in verification report")
	}

	if report.SummaryStatus == "" {
		t.Error("expected non-empty SummaryStatus in verification report")
	}
}

func TestVerifyAudioTrack_URLWithRunner(t *testing.T) {
	ctx := context.Background()
	dummyAudio := createTestAudioFile(t, "sample.m4a")

	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		// Check if it's metadata dump
		for i, a := range args {
			if a == "--dump-json" {
				return []byte(`{"title": "Space X", "uploader": "Boris Brejcha", "duration": 2, "ext": "m4a"}`), nil
			}
			if a == "-o" && i+1 < len(args) {
				outPath := args[i+1]
				// Copy dummy audio to outPath
				data, err := os.ReadFile(dummyAudio)
				if err != nil {
					return nil, err
				}
				if err := os.WriteFile(outPath, data, 0644); err != nil {
					return nil, err
				}
				return []byte("downloaded"), nil
			}
		}
		return []byte("ok"), nil
	}

	report, err := VerifyAudioTrackWithRunner(ctx, runner, "https://soundcloud.com/boris-brejcha/space-x-extended-mix", true)
	if err != nil {
		t.Fatalf("VerifyAudioTrackWithRunner error = %v", err)
	}
	if report == nil || report.Metadata.Title != "Space X" {
		t.Errorf("unexpected report: %+v", report)
	}
}

func TestVerifyAudioTrack_Errors(t *testing.T) {
	ctx := context.Background()

	// Nonexistent local file
	_, err := VerifyAudioTrack(ctx, "/nonexistent/audio.m4a", true)
	if err == nil {
		t.Fatal("expected error for nonexistent local file")
	}
}

func TestFetchURLMetadata_Runner(t *testing.T) {
	ctx := context.Background()

	t.Run("successful_fetch_and_cache", func(t *testing.T) {
		jsonData := `{"title": "Track Title", "uploader": "Uploader", "duration": 360, "ext": "m4a"}`
		runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return []byte(jsonData), nil
		}

		tempDir := t.TempDir()
		c := cache.NewInDir(tempDir, true)

		res, err := FetchURLMetadataWithRunner(ctx, runner, "https://youtube.com/watch?v=123", c)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Title != "Track Title" || res.Uploader != "Uploader" || res.DurationSeconds != 360 {
			t.Errorf("unexpected metadata result: %+v", res)
		}

		// Subsequent call should hit cache
		cachedRes, err := FetchURLMetadataWithRunner(ctx, nil, "https://youtube.com/watch?v=123", c)
		if err != nil {
			t.Fatalf("unexpected error from cache: %v", err)
		}
		if !cachedRes.Cached {
			t.Errorf("expected cached = true")
		}
	})

	t.Run("artist_fallback_for_uploader", func(t *testing.T) {
		jsonData := `{"title": "", "artist": "Artist Name", "duration": 200, "ext": ""}`
		runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return []byte(jsonData), nil
		}

		res, err := FetchURLMetadataWithRunner(ctx, runner, "https://soundcloud.com/track", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Title != "Unknown Title" || res.Uploader != "Artist Name" || res.Format != "m4a" {
			t.Errorf("unexpected result: %+v", res)
		}
	})

	t.Run("runner_error", func(t *testing.T) {
		runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return nil, errors.New("yt-dlp failed")
		}

		_, err := FetchURLMetadataWithRunner(ctx, runner, "https://youtube.com/watch?v=err", nil)
		if err == nil {
			t.Fatal("expected error on runner failure")
		}
	})

	t.Run("empty_output_error", func(t *testing.T) {
		runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return []byte(""), nil
		}

		_, err := FetchURLMetadataWithRunner(ctx, runner, "https://youtube.com/watch?v=empty", nil)
		if err == nil {
			t.Fatal("expected error on empty output")
		}
	})

	t.Run("invalid_json_error", func(t *testing.T) {
		runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return []byte("not valid json"), nil
		}

		_, err := FetchURLMetadataWithRunner(ctx, runner, "https://youtube.com/watch?v=bad", nil)
		if err == nil {
			t.Fatal("expected error on invalid json")
		}
	})
}

func TestFetchYouTubeMetadata(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _ = FetchYouTubeMetadata(ctx, "https://youtube.com/watch?v=123")
}

func TestSetDefaultRunner_Verifier(t *testing.T) {
	cleanup := SetDefaultRunner(func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte("test"), nil
	})
	defer cleanup()
}

func TestDefaultRunner_Verifier(t *testing.T) {
	ctx := context.Background()
	_, err := defaultRunner(ctx, "sh", "-c", "echo error message >&2; exit 1")
	if err == nil {
		t.Fatal("expected error from defaultRunner")
	}

	_, err = defaultRunner(ctx, "sh", "-c", "echo 'goroutine 1 [running]:\nmain.main()\n\tmain.go:10 +0x1\npanic: fail' >&2; exit 1")
	if err == nil {
		t.Fatal("expected error from defaultRunner")
	}
	if strings.Contains(err.Error(), "goroutine 1") || strings.Contains(err.Error(), "+0x1") {
		t.Errorf("expected stack trace to be stripped from defaultRunner error, got: %v", err)
	}

	_, _ = defaultRunner(ctx, "echo", "hello")
}

func TestComputeGoertzelDb_EdgeCases(t *testing.T) {
	// Empty samples
	db := ComputeGoertzelDb(nil, 0, 0, 1000, 48000)
	if db != -120.0 {
		t.Errorf("expected -120 dBFS for empty samples, got %f", db)
	}

	// Zero length range
	samples := make([]float32, 100)
	db = ComputeGoertzelDb(samples, 10, 10, 1000, 48000)
	if db != -120.0 {
		t.Errorf("expected -120 dBFS for zero length range, got %f", db)
	}
}
