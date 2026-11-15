//go:build unix

package verifier

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/alexgorbatchev/fetch-track-cli/internal/testenv"
)

// Every external tool the verifier starts must be stopped gracefully when its
// context is cancelled.
func TestVerifierStopsCancelledToolsGracefully(t *testing.T) {
	audioPath := filepath.Join("testdata", "missing.m4a")
	// PCM analysis probes the sample rate before decoding, so reaching its
	// ffmpeg step needs a readable file for the real ffprobe.
	readable := filepath.Join(t.TempDir(), "readable.wav")
	writeFloatWAV(t, readable, 44100, make([]float32, 4096))
	tests := []struct {
		name, tool string
		run        func(ctx context.Context)
	}{
		{name: "metadata ffprobe", tool: "ffprobe", run: func(ctx context.Context) { _, _ = FetchLocalMetadata(ctx, audioPath) }},
		{name: "sample rate ffprobe", tool: "ffprobe", run: func(ctx context.Context) { _, _ = AnalyzePCMAudio(ctx, audioPath, 60) }},
		{name: "pcm ffmpeg", tool: "ffmpeg", run: func(ctx context.Context) {
			requireFFmpeg(t)
			_, _ = AnalyzePCMAudio(ctx, readable, 60)
		}},
		{name: "yt-dlp", tool: "yt-dlp", run: func(ctx context.Context) {
			_, _ = FetchURLMetadataWithRunner(ctx, nil, "https://www.youtube.com/watch?v=example")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			probe := testenv.NewTerminationProbe(t, tt.tool, testenv.ProbeOptions{})
			probe.PrependToPath(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan struct{})
			go func() {
				defer close(done)
				tt.run(ctx)
			}()

			probe.WaitStarted(t)
			cancel()
			<-done

			want := testenv.ProbeOutcome{ReceivedTerm: true, CleanedUp: true, ToolExited: true, ChildExited: true}
			if got := probe.Outcome(t); got != want {
				t.Fatalf("outcome = %+v, want %+v", got, want)
			}
		})
	}
}
