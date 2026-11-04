//go:build unix

package testenv

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const (
	// probeTimeout bounds how long a probe waits for its tool to start or exit.
	probeTimeout = 10 * time.Second
	// childExitTimeout bounds how long a probe waits, once its tool is gone, for
	// the tool's child to exit. A signaled child exits at once; one the signal
	// never reached runs on, which this bound keeps from stalling the test.
	childExitTimeout = 2 * time.Second
)

// TerminationProbe is a fake external tool that records how it was stopped.
// Like a PyInstaller one-file build, it creates a scratch directory when it
// starts and removes it only when it is asked to terminate. It also starts a
// child process without a signal handler of its own, which exits only when a
// signal reaches the tool's whole process group.
type TerminationProbe struct {
	// Path is the absolute path of the fake tool.
	Path  string
	state string
}

// ProbeOutcome is what a TerminationProbe observed after its tool stopped.
type ProbeOutcome struct {
	// ReceivedTerm reports that the tool received SIGTERM.
	ReceivedTerm bool
	// CleanedUp reports that the tool removed its scratch directory.
	CleanedUp bool
	// ToolExited reports that the tool process is gone.
	ToolExited bool
	// ChildExited reports that the tool's child process is gone.
	ChildExited bool
}

// NewTerminationProbe writes an executable named name into a new temporary
// directory. With ignoreTerm, the tool records SIGTERM but keeps running, so
// only SIGKILL stops it. Cleanup kills any process the probe left running.
func NewTerminationProbe(t testing.TB, name string, ignoreTerm bool) *TerminationProbe {
	t.Helper()
	root := t.TempDir()
	p := &TerminationProbe{
		Path:  filepath.Join(root, "bin", name),
		state: filepath.Join(root, "state"),
	}
	for _, dir := range []string{filepath.Dir(p.Path), p.state} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	onTerm := `rm -rf "$state/scratch"; exit 143`
	if ignoreTerm {
		onTerm = ":"
	}
	script := fmt.Sprintf(`#!/bin/sh
state=%s
mkdir "$state/scratch"
sleep 30 &
echo $! > "$state/child.pid"
trap 'echo TERM >> "$state/signals"; %s' TERM
echo $$ > "$state/tool.pid.tmp" && mv "$state/tool.pid.tmp" "$state/tool.pid"
while :; do sleep 0.05; done
`, shellQuote(p.state), onTerm)
	if err := os.WriteFile(p.Path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.killLeftovers)
	return p
}

// PrependToPath puts the probe's directory first on PATH for the rest of the
// test, so the tool is found by its bare name.
func (p *TerminationProbe) PrependToPath(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", filepath.Dir(p.Path)+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// WaitStarted blocks until the tool has started and installed its SIGTERM
// handler.
func (p *TerminationProbe) WaitStarted(t testing.TB) {
	t.Helper()
	if !waitFor(probeTimeout, func() bool { return p.pid("tool.pid") > 0 }) {
		t.Fatalf("%s did not start within %s", filepath.Base(p.Path), probeTimeout)
	}
}

// Outcome reports how the tool stopped. It waits for the tool and its child to
// exit, giving up after a bound, so call it once the command has returned.
func (p *TerminationProbe) Outcome(t testing.TB) ProbeOutcome {
	t.Helper()
	tool, child := p.pid("tool.pid"), p.pid("child.pid")
	if tool <= 0 || child <= 0 {
		t.Fatalf("%s never started", filepath.Base(p.Path))
	}
	waitFor(probeTimeout, func() bool { return !running(tool) })
	waitFor(childExitTimeout, func() bool { return !running(child) })
	signals, err := os.ReadFile(filepath.Join(p.state, "signals"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	_, scratchErr := os.Stat(filepath.Join(p.state, "scratch"))
	return ProbeOutcome{
		ReceivedTerm: strings.Contains(string(signals), "TERM"),
		CleanedUp:    errors.Is(scratchErr, os.ErrNotExist),
		ToolExited:   !running(tool),
		ChildExited:  !running(child),
	}
}

func (p *TerminationProbe) killLeftovers() {
	for _, name := range []string{"child.pid", "tool.pid"} {
		if pid := p.pid(name); pid > 0 && running(pid) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	}
}

func (p *TerminationProbe) pid(name string) int {
	data, err := os.ReadFile(filepath.Join(p.state, name))
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0
	}
	return pid
}

// running reports whether pid names a process that has not been collected. An
// exited child of the tool is collected as soon as the tool itself is gone.
func running(pid int) bool {
	return !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
}

func waitFor(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return cond()
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
