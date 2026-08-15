package downloader

import (
	"testing"
)

func TestMapSourceSearchPrefix(t *testing.T) {
	tests := []struct {
		source string
		want   string
	}{
		{"youtube", "ytsearch8"},
		{"YouTube", "ytsearch8"},
		{"soundcloud", "scsearch8"},
		{"SoundCloud", "scsearch8"},
		{"bandcamp", "bcsearch8"},
		{"unknown", "ytsearch8"},
	}

	for _, tt := range tests {
		got := MapSourceSearchPrefix(tt.source)
		if got != tt.want {
			t.Errorf("MapSourceSearchPrefix(%q) = %q, want %q", tt.source, got, tt.want)
		}
	}
}
