package deps

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"
)

func TestFoldHomeErrorPreservesCause(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cause := errors.New("unavailable")
	err := FoldHomeError(fmt.Errorf("reading %s: %w", filepath.Join(home, "track.flac"), cause))
	if err.Error() != "reading ~/track.flac: unavailable" || !errors.Is(err, cause) {
		t.Fatalf("folded error lost display or cause: %v", err)
	}
	if FoldHomeError(nil) != nil {
		t.Fatal("nil error must remain nil")
	}
}

func TestFoldHomeWithDir(t *testing.T) {
	const home = "/home/dev"

	tests := []struct {
		name string
		in   string
		home string
		want string
	}{
		{"exact home directory", "/home/dev", home, "~"},
		{"nested path", "/home/dev/projects/app", home, "~/projects/app"},
		{"nested path with trailing home slash", "/home/dev/tracks/Song.m4a", "/home/dev/", "~/tracks/Song.m4a"},
		{"sibling directory sharing prefix", "/home/developer/app", home, "/home/developer/app"},
		{"sibling directory with hyphen suffix", "/home/dev-2/app", home, "/home/dev-2/app"},
		{"path outside home", "/var/tmp/app", home, "/var/tmp/app"},
		{"mounted prefix containing home substring", "/mnt/home/dev/app", home, "/mnt/home/dev/app"},
		{"embedded in log line", "DONE: /home/dev/tracks/Song.m4a", home, "DONE: ~/tracks/Song.m4a"},
		{"multiple embedded paths in error", "fetching /home/dev/a.m4a: probing /home/dev/a.m4a: failed", home, "fetching ~/a.m4a: probing ~/a.m4a: failed"},
		{"quoted home directory", `saved to "/home/dev"`, home, `saved to "~"`},
		{"empty home", "/home/dev/app", "", "/home/dev/app"},
		{"root home", "/home/dev/app", "/", "/home/dev/app"},
		{"dot home", "/home/dev/app", ".", "/home/dev/app"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FoldHomeWithDir(tt.in, tt.home); got != tt.want {
				t.Fatalf("FoldHomeWithDir(%q, %q) = %q, want %q", tt.in, tt.home, got, tt.want)
			}
		})
	}
}

func TestFoldHome(t *testing.T) {
	home := filepath.Join(t.TempDir(), "userhome")
	t.Setenv("HOME", home)

	if got := FoldHome(home); got != "~" {
		t.Fatalf("FoldHome(home) = %q, want ~", got)
	}
	nested := filepath.Join(home, "tracks", "Artist - Track.m4a")
	wantNested := filepath.Join("~", "tracks", "Artist - Track.m4a")
	if got := FoldHome(nested); got != wantNested {
		t.Fatalf("FoldHome(nested) = %q, want %q", got, wantNested)
	}
	sibling := home + "er/tracks/Artist - Track.m4a"
	if got := FoldHome(sibling); got != sibling {
		t.Fatalf("FoldHome(sibling) = %q, want unchanged %q", got, sibling)
	}

	t.Setenv("HOME", "")
	if got := FoldHome("/outside/path"); got != "/outside/path" {
		t.Fatalf("FoldHome with empty HOME = %q, want /outside/path", got)
	}
}
