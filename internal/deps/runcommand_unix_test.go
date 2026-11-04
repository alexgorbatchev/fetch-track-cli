//go:build unix

package deps

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/alexgorbatchev/fetch-track-cli/internal/testenv"
)

// gracePeriod is how long godeps.CommandContext lets a tool handle SIGTERM
// before it sends SIGKILL.
const gracePeriod = 5 * time.Second

func TestRunCommandStopsTimedOutToolGracefully(t *testing.T) {
	t.Parallel()
	probe := testenv.NewTerminationProbe(t, "yt-dlp", false)
	// Long enough for the tool to install its SIGTERM handler on a busy machine.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := RunCommand(ctx, probe.Path)
	deadline, _ := ctx.Deadline()
	elapsed := time.Since(deadline)

	if err == nil {
		t.Fatal("RunCommand() succeeded, want the timed-out tool to fail")
	}
	if elapsed >= gracePeriod {
		t.Fatalf("RunCommand() returned %s after the timeout; a tool that exits on SIGTERM must not wait for SIGKILL", elapsed)
	}
	want := testenv.ProbeOutcome{ReceivedTerm: true, CleanedUp: true, ToolExited: true, ChildExited: true}
	if got := probe.Outcome(t); got != want {
		t.Fatalf("outcome = %+v, want %+v", got, want)
	}
}

func TestRunCommandKillsCancelledToolAfterGracePeriod(t *testing.T) {
	t.Parallel()
	probe := testenv.NewTerminationProbe(t, "yt-dlp", true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := RunCommand(ctx, probe.Path)
		done <- err
	}()

	probe.WaitStarted(t)
	cancelled := time.Now()
	cancel()
	err := <-done
	elapsed := time.Since(cancelled)

	if err == nil || !strings.Contains(err.Error(), "killed") {
		t.Fatalf("RunCommand() error = %v, want the tool killed after ignoring SIGTERM", err)
	}
	if elapsed < gracePeriod {
		t.Fatalf("RunCommand() returned after %s, before the %s grace period", elapsed, gracePeriod)
	}
	want := testenv.ProbeOutcome{ReceivedTerm: true, CleanedUp: false, ToolExited: true, ChildExited: true}
	if got := probe.Outcome(t); got != want {
		t.Fatalf("outcome = %+v, want %+v", got, want)
	}
}
