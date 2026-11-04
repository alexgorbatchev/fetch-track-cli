package deps

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestRunCommand(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory: %v", err)
	}
	tests := []struct {
		name       string
		script     string
		wantOut    string
		wantErrMsg string
	}{
		{name: "standard output", script: "echo result; echo noise >&2", wantOut: "result\n"},
		{name: "sanitized standard error with folded home", script: `echo "failed at $HOME/tool" >&2; exit 3`, wantErrMsg: "exit status 3: failed at ~/tool"},
		{name: "failure without standard error", script: "exit 4", wantErrMsg: "exit status 4"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := RunCommand(context.Background(), "sh", "-c", tt.script)
			if tt.wantErrMsg != "" {
				if err == nil || err.Error() != tt.wantErrMsg {
					t.Fatalf("RunCommand() error = %v, want %q", err, tt.wantErrMsg)
				}
				if strings.Contains(err.Error(), home) {
					t.Fatalf("error %q exposes the home directory", err)
				}
				var exitErr *exec.ExitError
				if !errors.As(err, &exitErr) {
					t.Fatalf("error %v does not wrap the exec error", err)
				}
				return
			}
			if err != nil || string(out) != tt.wantOut {
				t.Fatalf("RunCommand() = %q, %v; want %q", out, err, tt.wantOut)
			}
		})
	}
}

func TestRunCommandMissingExecutable(t *testing.T) {
	_, err := RunCommand(context.Background(), "fetch-track-no-such-tool")
	if !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("RunCommand() error = %v, want exec.ErrNotFound", err)
	}
}
