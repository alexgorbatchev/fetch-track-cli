package verifier

import (
	"testing"
)

func TestIsURL(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"https://www.youtube.com/watch?v=12345", true},
		{"http://soundcloud.com/user/track", true},
		{"https://artist.bandcamp.com/track/song", true},
		{"Boris Brejcha - Space X", false},
		{"tracks/song.m4a", false},
	}

	for _, tt := range tests {
		got := IsURL(tt.input)
		if got != tt.want {
			t.Errorf("IsURL(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}
