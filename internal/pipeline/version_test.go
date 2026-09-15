package pipeline

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPreserveVersionInTitle(t *testing.T) {
	tests := []struct {
		name          string
		sourceTitle   string
		enrichedTitle string
		wantTitle     string
	}{
		{
			name:          "preserves instrumental",
			sourceTitle:   "Parra for Cuva - Unfinished Colours (Instrumental)",
			enrichedTitle: "Unfinished Colours",
			wantTitle:     "Unfinished Colours (Instrumental)",
		},
		{
			name:          "already contains instrumental",
			sourceTitle:   "Parra for Cuva - Unfinished Colours (Instrumental)",
			enrichedTitle: "Unfinished Colours (Instrumental)",
			wantTitle:     "Unfinished Colours (Instrumental)",
		},
		{
			name:          "preserves VIP",
			sourceTitle:   "Skrillex - Mumbai Power (VIP)",
			enrichedTitle: "Mumbai Power",
			wantTitle:     "Mumbai Power (VIP)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PreserveVersionInTitle(tt.sourceTitle, tt.enrichedTitle)
			if got != tt.wantTitle {
				t.Errorf("PreserveVersionInTitle(%q, %q) = %q, want %q", tt.sourceTitle, tt.enrichedTitle, got, tt.wantTitle)
			}
		})
	}
}

func TestResolveCollisionSafePath(t *testing.T) {
	dir := t.TempDir()

	// Existing file
	existingFile := filepath.Join(dir, "Song.m4a")
	if err := os.WriteFile(existingFile, []byte("existing vocal"), 0644); err != nil {
		t.Fatalf("failed to create existing file: %v", err)
	}

	// New incoming file that would collide
	newTempFile := filepath.Join(dir, "temp_incoming.m4a")
	if err := os.WriteFile(newTempFile, []byte("incoming instrumental"), 0644); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	safePath := ResolveCollisionSafePath(existingFile, newTempFile)
	expected := filepath.Join(dir, "Song (1).m4a")

	if safePath != expected {
		t.Errorf("ResolveCollisionSafePath = %q, want %q", safePath, expected)
	}
}
