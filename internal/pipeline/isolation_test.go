package pipeline

import (
	"os"
	"testing"

	"github.com/alexgorbatchev/fetch-track-cli/internal/testenv"
)

// TestMain keeps dependency installs and cached checks in temporary directories;
// they otherwise land in the user's real fetch-track directories.
func TestMain(m *testing.M) {
	os.Exit(testenv.RunWithIsolatedHomes(m))
}
