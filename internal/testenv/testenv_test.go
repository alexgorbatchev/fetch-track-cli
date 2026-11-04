package testenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	os.Exit(RunWithIsolatedHomes(m))
}

func TestRunWithIsolatedHomes(t *testing.T) {
	for _, name := range isolatedHomes {
		dir := os.Getenv(name)
		if filepath.Base(dir) != name || !strings.HasPrefix(filepath.Base(filepath.Dir(dir)), "fetch-track-test-") {
			t.Errorf("%s = %q, want a directory inside a fetch-track-test- temporary directory", name, dir)
		}
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			t.Errorf("%s %q is not a directory: %v", name, dir, err)
		}
	}
}
