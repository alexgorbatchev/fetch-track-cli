// Package testenv prepares process environments for tests: isolated data and
// cache directories, and fake external tools that record how they were stopped.
package testenv

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// isolatedHomes are the XDG base directories RunWithIsolatedHomes replaces.
var isolatedHomes = []string{"XDG_DATA_HOME", "XDG_CACHE_HOME"}

// RunWithIsolatedHomes runs the package's tests with XDG_DATA_HOME and
// XDG_CACHE_HOME pointing into a new temporary directory, so dependency
// installs, managed PATH setup, and cached dependency checks never touch the
// user's real fetch-track directories, and removes that directory afterward.
// Call it from TestMain and pass its result to os.Exit.
func RunWithIsolatedHomes(m *testing.M) (code int) {
	root, err := os.MkdirTemp("", "fetch-track-test-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer func() {
		if err := os.RemoveAll(root); err != nil {
			fmt.Fprintln(os.Stderr, err)
			code = 1
		}
	}()
	for _, name := range isolatedHomes {
		dir := filepath.Join(root, name)
		if err := os.Mkdir(dir, 0o755); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if err := os.Setenv(name, dir); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}
	return m.Run()
}
