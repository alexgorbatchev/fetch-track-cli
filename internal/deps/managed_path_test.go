package deps

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/alexgorbatchev/godeps/v2"
)

// TestInitManagedPath checks which yt-dlp the process resolves after
// InitManagedPath, for the unpacked package layout and an earlier one-file
// install, with and without a copy elsewhere on PATH.
func TestInitManagedPath(t *testing.T) {
	tests := []struct {
		name          string
		oneFile       bool
		globalVersion string // empty: no copy elsewhere on PATH
		wantManaged   bool
	}{
		{name: "satisfying global copy wins", globalVersion: "2026.08.19", wantManaged: false},
		{name: "outdated global copy yields to managed copy", globalVersion: "2023.01.01", wantManaged: true},
		{name: "managed copy fills in a missing global copy", wantManaged: true},
		{name: "one-file managed install still resolves", oneFile: true, wantManaged: true},
		{name: "satisfying global copy wins over one-file install", oneFile: true, globalVersion: "2026.08.19", wantManaged: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dataHome := t.TempDir()
			t.Setenv("XDG_DATA_HOME", dataHome)
			binDir := filepath.Join(dataHome, AppName, "bin")
			managed := filepath.Join(binDir, "yt-dlp", "yt-dlp")
			if tt.oneFile {
				managed = filepath.Join(binDir, "yt-dlp")
			}
			writeExecutable(t, managed)

			pathDirs := []string{t.TempDir()}
			global := ""
			if tt.globalVersion != "" {
				global = filepath.Join(pathDirs[0], "yt-dlp")
				writeExecutable(t, global)
			}
			t.Setenv("PATH", pathDirs[0])

			versions := map[string]string{managed: "2026.10.01", global: tt.globalVersion}
			t.Cleanup(SetDefaultRunner(func(ctx context.Context, name string, args ...string) ([]byte, error) {
				if v, ok := versions[name]; ok && v != "" {
					return []byte(v + "\n"), nil
				}
				return nil, exec.ErrNotFound
			}))

			if err := InitManagedPath(context.Background()); err != nil {
				t.Fatalf("InitManagedPath() error = %v", err)
			}
			got, err := exec.LookPath("yt-dlp")
			if err != nil {
				t.Fatalf("yt-dlp does not resolve on PATH %q: %v", os.Getenv("PATH"), err)
			}
			want := global
			if tt.wantManaged {
				want = managed
			}
			if got != want {
				t.Fatalf("yt-dlp resolves to %q, want %q", got, want)
			}
		})
	}
}

func TestInitManagedPathCancelled(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := InitManagedPath(ctx); err == nil {
		t.Fatal("InitManagedPath() with a cancelled context succeeded")
	}
}

// A second placement in the same process must not repeat the version
// commands, until PATH or the managed directory changes.
func TestInitManagedPathRunsVersionChecksOnce(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	writeExecutable(t, filepath.Join(dataHome, AppName, "bin", "yt-dlp", "yt-dlp"))
	globalDir := t.TempDir()
	writeExecutable(t, filepath.Join(globalDir, "yt-dlp"))
	t.Setenv("PATH", globalDir)
	calls := 0
	t.Cleanup(SetDefaultRunner(func(ctx context.Context, name string, args ...string) ([]byte, error) {
		calls++
		return []byte("2026.08.19\n"), nil
	}))

	steps := []struct {
		name      string
		change    func()
		wantCalls int
	}{
		{name: "first placement checks versions", change: func() {}, wantCalls: 1},
		{name: "repeated placement is skipped", change: func() {}, wantCalls: 0},
		{name: "changed PATH is placed again", change: func() { t.Setenv("PATH", globalDir+string(os.PathListSeparator)+t.TempDir()) }, wantCalls: 1},
	}
	for _, step := range steps {
		step.change()
		calls = 0
		if err := InitManagedPath(context.Background()); err != nil {
			t.Fatalf("%s: InitManagedPath() error = %v", step.name, err)
		}
		if calls != step.wantCalls {
			t.Fatalf("%s: %d version commands, want %d", step.name, calls, step.wantCalls)
		}
	}
}

func TestInstallMissingDependenciesReplacesOneFileYtDlp(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	t.Setenv("PATH", t.TempDir())
	binDir := filepath.Join(dataHome, AppName, "bin")
	oneFile := filepath.Join(binDir, "yt-dlp")
	writeExecutable(t, oneFile)
	t.Cleanup(SetDefaultRunner(func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte("2026.08.19\n"), nil
	}))
	installs := 0
	original := RequiredDependencies
	t.Cleanup(func() { RequiredDependencies = original })
	RequiredDependencies = []Dependency{{
		Name:       "yt-dlp",
		MinVersion: "2024.08.01",
		Installer: godeps.InstallerFunc(func(ctx context.Context, dir string) error {
			installs++
			if err := os.Remove(filepath.Join(dir, "yt-dlp")); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			writeExecutable(t, filepath.Join(dir, "yt-dlp", "yt-dlp"))
			return nil
		}),
	}}

	if !HasOneFileYtDlp() {
		t.Fatal("HasOneFileYtDlp() = false for a one-file bin/yt-dlp")
	}
	installed, err := InstallMissingDependencies(context.Background())
	if err != nil || !slices.Equal(installed, []string{"yt-dlp"}) || installs != 1 {
		t.Fatalf("InstallMissingDependencies() = %v, %v with %d installs; want [yt-dlp] once", installed, err, installs)
	}
	if HasOneFileYtDlp() {
		t.Fatal("HasOneFileYtDlp() = true after the unpacked build replaced it")
	}
	installed, err = InstallMissingDependencies(context.Background())
	if err != nil || len(installed) != 0 || installs != 1 {
		t.Fatalf("second InstallMissingDependencies() = %v, %v with %d installs; want nothing installed", installed, err, installs)
	}

	RequiredDependencies[0].Installer = godeps.InstallerFunc(func(context.Context, string) error { return errors.New("download failed") })
	if err := os.RemoveAll(filepath.Join(binDir, "yt-dlp")); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, oneFile)
	if _, err := InstallMissingDependencies(context.Background()); err == nil || !strings.Contains(err.Error(), "replacing the one-file yt-dlp build") {
		t.Fatalf("InstallMissingDependencies() error = %v, want the failed replacement", err)
	}
}

func writeExecutable(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}
