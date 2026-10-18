package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/alexgorbatchev/fetch-track-cli/internal/downloader"
)

func TestProgressJobID(t *testing.T) {
	for _, jobID := range []string{"abc123", ""} {
		t.Run("job="+jobID, func(t *testing.T) {
			_, cleanup := setupMainTestEnv(t)
			defer cleanup()
			t.Setenv("AGENT", "1")
			outDir := t.TempDir()
			path := filepath.Join(outDir, "Artist - Track.m4a")
			restore := downloader.SetDefaultRunner(func(ctx context.Context, name string, args ...string) ([]byte, error) {
				for _, arg := range args {
					if arg == "-o" {
						return nil, os.WriteFile(path, []byte("synthetic audio"), 0o600)
					}
				}
				return []byte(`{"id":"track","title":"Artist - Track","duration":360,"webpage_url":"https://soundcloud.com/artist/track"}`), nil
			})
			defer restore()
			output, err := os.CreateTemp(t.TempDir(), "events")
			if err != nil {
				t.Fatal(err)
			}
			defer output.Close()
			stdout := os.Stdout
			os.Stdout = output
			defer func() { os.Stdout = stdout }()
			cmd := newRootCommand()
			args := []string{"--progress-target", "stdout", "--skip-verify", "--skip-metadata", "--confirm-fingerprint=false", "-s", "soundcloud", "-o", outDir, "Artist - Track"}
			if jobID != "" {
				args = append(args, "--job-id", jobID)
			}
			cmd.SetArgs(args)
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			if _, err := output.Seek(0, io.SeekStart); err != nil {
				t.Fatal(err)
			}
			scanner := bufio.NewScanner(output)
			sequence := 0
			lastPercent := 0.0
			actualID := jobID
			for scanner.Scan() {
				var ev struct {
					JobID    string   `json:"job_id"`
					Sequence int      `json:"sequence"`
					Type     string   `json:"type"`
					Percent  *float64 `json:"percent"`
				}
				// The normal CLI result shares stdout; inspect telemetry lines only.
				if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil || ev.Type == "" {
					continue
				}
				sequence++
				if actualID == "" {
					actualID = ev.JobID
				}
				if ev.JobID == "" || ev.JobID != actualID || ev.Sequence != sequence {
					t.Fatalf("invalid envelope: %s", scanner.Text())
				}
				if ev.Percent == nil || *ev.Percent < lastPercent || *ev.Percent > 100 {
					t.Fatalf("invalid completion: %s", scanner.Text())
				}
				lastPercent = *ev.Percent
			}
			if err := scanner.Err(); err != nil {
				t.Fatal(err)
			}
			if sequence == 0 || lastPercent != 100 {
				t.Fatalf("events=%d, completion=%v", sequence, lastPercent)
			}
		})
	}
}
