//go:build unix

package verifier

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFetchLocalMetadataEmptyProbeOutput(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ffprobe"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	_, err := FetchLocalMetadata(context.Background(), "track.m4a")
	if err == nil || !strings.HasSuffix(err.Error(), "empty ffprobe output") {
		t.Fatalf("FetchLocalMetadata() error = %v, want empty ffprobe output", err)
	}
}
