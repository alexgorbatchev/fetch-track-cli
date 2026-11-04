package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alexgorbatchev/fetch-track-cli/internal/deps"
	"github.com/alexgorbatchev/godeps/v2"
)

// A run whose dependency check fails must still give progress consumers a
// terminal error event.
func TestRootCommandReportsDependencyFailureProgress(t *testing.T) {
	_, cleanup := setupMainTestEnv(t)
	defer cleanup()
	t.Setenv("AGENT", "1")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Cleanup(deps.SetDefaultRunner(func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return nil, exec.ErrNotFound
	}))
	output, err := os.CreateTemp(t.TempDir(), "events")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = output.Close() }()
	stdout := os.Stdout
	os.Stdout = output
	defer func() { os.Stdout = stdout }()

	cmd := newRootCommand()
	cmd.SetArgs([]string{"--progress-target", "stdout", "-o", t.TempDir(), "Artist - Track"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("Execute() succeeded with every dependency missing")
	}

	if _, err := output.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(output)
	for scanner.Scan() {
		var ev struct {
			Type  string `json:"type"`
			Phase string `json:"phase"`
			Error string `json:"error"`
		}
		if json.Unmarshal(scanner.Bytes(), &ev) == nil && ev.Type == "error" && ev.Phase == "dependencies" && strings.Contains(ev.Error, "is missing") {
			return
		}
	}
	data, _ := os.ReadFile(output.Name())
	t.Fatalf("no dependencies error event on the progress target: %s", data)
}

// --auto-install replaces a managed one-file yt-dlp even when its version is
// current, because it unpacks itself into the temporary directory every run.
func TestEnsureDependenciesReplacesOneFileYtDlp(t *testing.T) {
	_, cleanup := setupMainTestEnv(t)
	defer cleanup()
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	binDir := filepath.Join(dataHome, deps.AppName, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	oneFile := filepath.Join(binDir, "yt-dlp")
	if err := os.WriteFile(oneFile, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	replaced := 0
	for i := range deps.RequiredDependencies {
		if deps.RequiredDependencies[i].Name == "yt-dlp" {
			deps.RequiredDependencies[i].Installer = godeps.InstallerFunc(func(ctx context.Context, dir string) error {
				replaced++
				if err := os.Remove(filepath.Join(dir, "yt-dlp")); err != nil {
					return err
				}
				return os.MkdirAll(filepath.Join(dir, "yt-dlp"), 0o755)
			})
		}
	}
	autoInstall = true
	defer func() { autoInstall = false }()

	if err := ensureDependencies(context.Background()); err != nil {
		t.Fatalf("ensureDependencies() error = %v", err)
	}
	if replaced != 1 || deps.HasOneFileYtDlp() {
		t.Fatalf("one-file yt-dlp replaced %d times, still present: %v", replaced, deps.HasOneFileYtDlp())
	}
	if err := ensureDependencies(context.Background()); err != nil || replaced != 1 {
		t.Fatalf("second ensureDependencies() = %v after %d replacements, want no further installs", err, replaced)
	}

	if err := os.Remove(filepath.Join(binDir, "yt-dlp")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oneFile, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	for i := range deps.RequiredDependencies {
		if deps.RequiredDependencies[i].Name == "yt-dlp" {
			deps.RequiredDependencies[i].Installer = godeps.InstallerFunc(func(context.Context, string) error { return errors.New("download failed") })
		}
	}
	stderr := captureStderr(t, func() {
		if err := ensureDependencies(context.Background()); err != nil {
			t.Errorf("ensureDependencies() error = %v; a failed replacement must keep the working one-file build", err)
		}
	})
	if !strings.Contains(stderr, "warning: keeping the one-file yt-dlp build") || !strings.Contains(stderr, "download failed") {
		t.Fatalf("stderr = %q, want a warning about the failed replacement", stderr)
	}
	if !deps.HasOneFileYtDlp() {
		t.Fatal("the one-file build was removed although its replacement failed")
	}
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	original := os.Stderr
	os.Stderr = file
	func() {
		defer func() { os.Stderr = original }()
		fn()
	}()
	data, err := os.ReadFile(file.Name())
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
