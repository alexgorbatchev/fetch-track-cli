package metadata

import (
	"testing"
)

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{
			input: "Boris Brejcha – Space X",
			want:  "Boris Brejcha - Space X",
		},
		{
			input: "Artist/Title?With:Illegal|Chars",
			want:  "Artist_Title_With_Illegal_Chars",
		},
		{
			input: "  Multiple   Spaces   -   In  Between  ",
			want:  "Multiple Spaces - In Between",
		},
	}

	for _, tt := range tests {
		got := SanitizeFilename(tt.input)
		if got != tt.want {
			t.Errorf("SanitizeFilename(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
