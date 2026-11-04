//go:build unix

package downloader

import (
	"context"
	"testing"

	"github.com/alexgorbatchev/fetch-track-cli/internal/testenv"
)

// The default runner must give yt-dlp the chance to remove its temporary files
// when a search or download is cancelled.
func TestDefaultRunnerStopsCancelledYtDlpGracefully(t *testing.T) {
	tests := []struct {
		name string
		run  func(ctx context.Context, outDir string)
	}{
		{name: "search", run: func(ctx context.Context, _ string) {
			_, _ = SearchSourcesInParallelWithRunner(ctx, nil, []string{"youtube"}, "Artist", "Title", "Artist - Title", nil)
		}},
		{name: "download", run: func(ctx context.Context, outDir string) {
			_, _ = DownloadAudioStreamWithRunner(ctx, nil, "https://www.youtube.com/watch?v=example", outDir)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("AGENT", "1")
			probe := testenv.NewTerminationProbe(t, "yt-dlp", false)
			probe.PrependToPath(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			outDir := t.TempDir()
			done := make(chan struct{})
			go func() {
				defer close(done)
				tt.run(ctx, outDir)
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
