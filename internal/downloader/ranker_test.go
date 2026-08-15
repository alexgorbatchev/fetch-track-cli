package downloader

import (
	"testing"
)

func TestRankYouTubeCandidates(t *testing.T) {
	tests := []struct {
		name       string
		candidates []YouTubeCandidate
		artist     string
		title      string
		wantID     string
	}{
		{
			name:       "empty candidates",
			candidates: nil,
			artist:     "Boris Brejcha",
			title:      "Space X",
			wantID:     "",
		},
		{
			name: "prefer extended mix over radio edit",
			candidates: []YouTubeCandidate{
				{ID: "radio123", Title: "Boris Brejcha - Space X (Radio Edit)", Duration: 210},
				{ID: "ext456", Title: "Boris Brejcha - Space X (Extended Mix)", Duration: 503},
			},
			artist: "Boris Brejcha",
			title:  "Space X",
			wantID: "ext456",
		},
		{
			name: "penalize continuous full album",
			candidates: []YouTubeCandidate{
				{ID: "album999", Title: "Boris Brejcha - Full Album 2024", Duration: 3600},
				{ID: "orig123", Title: "Boris Brejcha - Space X (Original Mix)", Duration: 480},
			},
			artist: "Boris Brejcha",
			title:  "Space X",
			wantID: "orig123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RankYouTubeCandidates(tt.candidates, tt.artist, tt.title)
			if tt.wantID == "" {
				if got != nil {
					t.Errorf("RankYouTubeCandidates() = %v, want nil", got)
				}
				return
			}
			if got == nil || got.ID != tt.wantID {
				gotID := ""
				if got != nil {
					gotID = got.ID
				}
				t.Errorf("RankYouTubeCandidates() ID = %q, want %q", gotID, tt.wantID)
			}
		})
	}
}
