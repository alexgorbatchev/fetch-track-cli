package verifier

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/alexgorbatchev/fetch-track-cli/internal/cache"
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

	// The fixture is digital silence, which has no bandwidth to measure.
	if report.EstimatedBandwidthHz != 0 || !report.HasLowBandwidthWarning {
		t.Errorf("silent fixture report = %+v, want 0 Hz with a low-bandwidth warning", report)
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

// A low-quality verdict names its cause, and keeps the status prefix that
// consumers match. The fidelity tier and the bandwidth line agree with the
// verdict: Opus that meets a lower minimum but not the default bar's bitrate
// floor is standard fidelity, and a cutoff that passes beside a failed bitrate
// is not called clean.
func TestVerifyAudioTrackStatesWhyQualityIsLow(t *testing.T) {
	requireFFmpeg(t)
	source := calibrationFixture(t, "full-band", 44100, fullBand)
	dir := t.TempDir()
	const cleanBandwidth = "AUDIO BANDWIDTH: Clean frequency response"
	tests := []struct {
		name, file, encoder string
		args                []string
		minHz               int
		wantStatus          []string
		wantRecommendation  string
		// wantBandwidth lists text the bandwidth line must contain.
		wantBandwidth []string
	}{
		{"opus below the floor", "Artist - Track (Extended Mix).opus", "libopus", []string{"-b:a", "96k", "-vbr", "off"}, 19000,
			[]string{"STATUS: Low audio quality detected (Opus average bitrate 96.0 kbps is below 145 kbps)."},
			"LOW BITRATE: Opus average bitrate 96.0 kbps is below 145 kbps at a 19.0 kHz minimum. Opus frequency cutoffs do not reflect bitrate.",
			[]string{"AUDIO BANDWIDTH: Frequency cutoff 20.", " kHz reaches the 19.0 kHz minimum; see LOW BITRATE."}},
		{"opus radio edit below the floor", "Artist - Track (Radio Edit).opus", "libopus", []string{"-b:a", "128k", "-vbr", "off"}, 19000,
			[]string{"STATUS: Low audio quality detected (Opus average bitrate 128.0 kbps is below 145 kbps) and is a radio edit."}, "LOW BITRATE: ",
			[]string{"see LOW BITRATE."}},
		{"opus at an 18 kHz minimum", "Artist - Track (Extended Mix).opus", "libopus", []string{"-b:a", "96k", "-vbr", "off"}, 18000,
			[]string{"STATUS: Standard fidelity audio suitable for mixing."}, "", []string{cleanBandwidth}},
		{"opus 160 kbps above the floor", "Artist - Track (Extended Mix).opus", "libopus", []string{"-b:a", "160k", "-vbr", "off"}, 19000,
			[]string{"STATUS: High fidelity audio suitable for mixing."}, "", []string{cleanBandwidth}},
		{"opus 160 kbps above the floor and an 18 kHz minimum", "Artist - Track (Extended Mix).opus", "libopus", []string{"-b:a", "160k", "-vbr", "off"}, 18000,
			[]string{"STATUS: High fidelity audio suitable for mixing."}, "", []string{cleanBandwidth}},
		{"mp3 192 at an 18 kHz minimum", "Artist - Track (Extended Mix).mp3", "libmp3lame", []string{"-b:a", "192k"}, 18000,
			[]string{"STATUS: Standard fidelity audio suitable for mixing."}, "", []string{cleanBandwidth}},
		{"mp3 below the cutoff", "Artist - Track (Extended Mix).mp3", "libmp3lame", []string{"-b:a", "128k"}, 19000,
			[]string{"STATUS: Low audio quality detected (frequency cutoff 16.", " kHz is below the 19.0 kHz minimum)."}, "LOW BANDWIDTH: ",
			[]string{"LOW BANDWIDTH: Frequency cutoff ("}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !hasEncoder(t, tt.encoder) {
				t.Skipf("ffmpeg lacks %s", tt.encoder)
			}
			path := filepath.Join(dir, tt.name, tt.file)
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			encode(t, source, path, tt.encoder, tt.args...)
			report, err := VerifyAudioTrack(context.Background(), path, false, tt.minHz)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range tt.wantStatus {
				if !strings.Contains(report.SummaryStatus, want) {
					t.Errorf("status %q does not contain %q", report.SummaryStatus, want)
				}
			}
			found := tt.wantRecommendation == ""
			for _, rec := range report.Recommendations {
				if tt.wantRecommendation == "" && (strings.HasPrefix(rec, "LOW BITRATE") || strings.HasPrefix(rec, "LOW BANDWIDTH")) {
					t.Errorf("unexpected recommendation %q", rec)
				}
				if tt.wantRecommendation != "" && strings.HasPrefix(rec, tt.wantRecommendation) {
					found = true
				}
			}
			if !found {
				t.Errorf("recommendations %q lack %q", report.Recommendations, tt.wantRecommendation)
			}
			bandwidthLines := 0
			for _, rec := range report.Recommendations {
				if !strings.HasPrefix(rec, "AUDIO BANDWIDTH") && !strings.HasPrefix(rec, "LOW BANDWIDTH") {
					continue
				}
				bandwidthLines++
				for _, want := range tt.wantBandwidth {
					if !strings.Contains(rec, want) {
						t.Errorf("bandwidth line %q does not contain %q", rec, want)
					}
				}
			}
			if bandwidthLines != 1 {
				t.Errorf("recommendations %q have %d bandwidth lines, want 1", report.Recommendations, bandwidthLines)
			}
		})
	}
}

// The status reason and the LOW BITRATE recommendation state the shortfall in
// the same words, measured or not.
func TestQualityTextForABitrateBelowTheFloor(t *testing.T) {
	tests := []struct {
		name               string
		bitrateBps         int
		wantReason         string
		wantRecommendation string
	}{
		{"measured", 96014, "Opus average bitrate 96.0 kbps is below 145 kbps",
			"LOW BITRATE: Opus average bitrate 96.0 kbps is below 145 kbps at a 19.0 kHz minimum. Opus frequency cutoffs do not reflect bitrate."},
		{"unmeasured", 0, "Opus average bitrate could not be measured; it needs 145 kbps",
			"LOW BITRATE: Opus average bitrate could not be measured; it needs 145 kbps at a 19.0 kHz minimum. Opus frequency cutoffs do not reflect bitrate."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := AudioQualityReport{Codec: "opus", BitrateBps: tt.bitrateBps, EstimatedBandwidthHz: 20200}
			EvaluateQuality(&report, 19000)
			if reasons := lowQualityReasonText(report); len(reasons) != 1 || reasons[0] != tt.wantReason {
				t.Errorf("reasons %q, want %q", reasons, tt.wantReason)
			}
			want := []string{"AUDIO BANDWIDTH: Frequency cutoff 20.2 kHz reaches the 19.0 kHz minimum; see LOW BITRATE.", tt.wantRecommendation}
			if got := qualityRecommendations(report); !slices.Equal(got, want) {
				t.Errorf("recommendations %q, want %q", got, want)
			}
		})
	}
}

// The fidelity tier follows the default bar, floor included, whatever minimum
// the verdict applied.
func TestFidelityStatus(t *testing.T) {
	tests := []struct {
		name        string
		report      AudioQualityReport
		minHz       int
		isRadioEdit bool
		want        string
	}{
		{"opus 96 kbps at 18 kHz", AudioQualityReport{Codec: "opus", BitrateBps: 96014, EstimatedBandwidthHz: 20200}, 18000, false, "STATUS: Standard fidelity audio suitable for mixing."},
		{"opus 160 kbps at 18 kHz", AudioQualityReport{Codec: "opus", BitrateBps: 160000, EstimatedBandwidthHz: 20200}, 18000, false, "STATUS: High fidelity audio suitable for mixing."},
		{"opus at the floor at 18 kHz", AudioQualityReport{Codec: "opus", BitrateBps: 145000, EstimatedBandwidthHz: 20200}, 18000, false, "STATUS: High fidelity audio suitable for mixing."},
		{"opus just under the floor at 18 kHz", AudioQualityReport{Codec: "opus", BitrateBps: 144999, EstimatedBandwidthHz: 20200}, 18000, false, "STATUS: Standard fidelity audio suitable for mixing."},
		{"unmeasured opus at 18 kHz", AudioQualityReport{Codec: "opus", EstimatedBandwidthHz: 20200}, 18000, false, "STATUS: Standard fidelity audio suitable for mixing."},
		{"mp3 192 at 18 kHz", AudioQualityReport{Codec: "mp3", BitrateBps: 192000, EstimatedBandwidthHz: 18800}, 18000, false, "STATUS: Standard fidelity audio suitable for mixing."},
		{"mp3 320 at a 20 kHz minimum", AudioQualityReport{Codec: "mp3", BitrateBps: 320000, EstimatedBandwidthHz: 20200}, 20000, false, "STATUS: High fidelity audio suitable for mixing."},
		{"radio edit that meets the minimum", AudioQualityReport{Codec: "opus", BitrateBps: 96014, EstimatedBandwidthHz: 20200}, 18000, true, "STATUS: Downloaded track is a radio edit."},
		{"opus below the floor", AudioQualityReport{Codec: "opus", BitrateBps: 96014, EstimatedBandwidthHz: 20200}, 19000, false, "STATUS: Low audio quality detected (Opus average bitrate 96.0 kbps is below 145 kbps)."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := tt.report
			EvaluateQuality(&report, tt.minHz)
			before := slices.Clone(report.LowQualityReasons)
			if got := fidelityStatus(tt.isRadioEdit, report); got != tt.want {
				t.Errorf("status %q, want %q", got, tt.want)
			}
			if report.MinBandwidthHz != tt.minHz || !slices.Equal(report.LowQualityReasons, before) {
				t.Errorf("grading the default bar changed the report: %+v", report)
			}
		})
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
				if !slices.Contains(args, "bestaudio/best") || slices.Contains(args, "--audio-format") {
					t.Fatalf("verification must download the acquisition stream natively: %v", args)
				}
				outPath := strings.ReplaceAll(args[i+1], "%(ext)s", "m4a")
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
