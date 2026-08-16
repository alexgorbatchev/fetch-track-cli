package verifier

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
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

func TestFetchURLMetadata_InvalidURL(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Canceled context to fail command fast

	_, err := FetchURLMetadata(ctx, "https://invalid-nonexistent-domain-123456.com/track")
	if err == nil {
		t.Error("expected error for invalid URL or canceled context")
	}
}
