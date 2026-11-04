package deps

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
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
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := InitManagedPath(ctx); err == nil {
		t.Fatal("InitManagedPath() with a cancelled context succeeded")
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
