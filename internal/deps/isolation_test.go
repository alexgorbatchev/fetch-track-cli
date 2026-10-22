package deps

import (
	"fmt"
	"os"
	"testing"
)

// TestMain keeps dependency installs inside a temporary managed directory;
// XDG_DATA_HOME otherwise points tests at the user's real fetch-track/bin.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "fetch-track-data-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err = os.Setenv("XDG_DATA_HOME", dir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	if err = os.RemoveAll(dir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		code = 1
	}
	os.Exit(code)
}
