//go:build unix

package testenv

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/alexgorbatchev/godeps/v2"
)

// The probe must tell a graceful stop apart from exec.CommandContext's
// default, which SIGKILLs only the direct child.
func TestTerminationProbe(t *testing.T) {
	tests := []struct {
		name    string
		command func(context.Context, string) *exec.Cmd
		want    ProbeOutcome
	}{
		{
			name:    "graceful group termination",
			command: func(ctx context.Context, path string) *exec.Cmd { return godeps.CommandContext(ctx, path) },
			want:    ProbeOutcome{ReceivedTerm: true, CleanedUp: true, ToolExited: true, ChildExited: true},
		},
		{
			name:    "kill of the direct child only",
			command: func(ctx context.Context, path string) *exec.Cmd { return exec.CommandContext(ctx, path) },
			want:    ProbeOutcome{ReceivedTerm: false, CleanedUp: false, ToolExited: true, ChildExited: false},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			probe := NewTerminationProbe(t, "tool", false)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cmd := tt.command(ctx, probe.Path)
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			probe.WaitStarted(t)
			cancel()
			_ = cmd.Wait()
			if got := probe.Outcome(t); got != tt.want {
				t.Fatalf("outcome = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestTerminationProbeIgnoringTerm(t *testing.T) {
	probe := NewTerminationProbe(t, "tool", true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := godeps.CommandContext(ctx, probe.Path)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	probe.WaitStarted(t)
	cancel()
	err := cmd.Wait()
	if err == nil || !strings.Contains(err.Error(), "killed") {
		t.Fatalf("Wait() error = %v, want the tool killed after ignoring SIGTERM", err)
	}
	want := ProbeOutcome{ReceivedTerm: true, CleanedUp: false, ToolExited: true, ChildExited: true}
	if got := probe.Outcome(t); got != want {
		t.Fatalf("outcome = %+v, want %+v", got, want)
	}
}

func TestTerminationProbePrependToPath(t *testing.T) {
	probe := NewTerminationProbe(t, "fetch-track-probe-tool", false)
	probe.PrependToPath(t)
	path, err := exec.LookPath("fetch-track-probe-tool")
	if err != nil || path != probe.Path {
		t.Fatalf("LookPath = %q, %v; want %q", path, err, probe.Path)
	}
}
