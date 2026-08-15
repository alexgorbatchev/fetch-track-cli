package downloader

import (
	"testing"
)

func TestRankCandidates(t *testing.T) {
	tests := []struct {
		name       string
		candidates []Candidate
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
			name: "prefer extended mix over radio edit across sources",
			candidates: []Candidate{
				{ID: "radio123", Title: "Boris Brejcha - Space X (Radio Edit)", Duration: 210, Source: "youtube"},
				{ID: "ext456", Title: "Boris Brejcha - Space X (Extended Mix)", Duration: 503, Source: "soundcloud"},
			},
			artist: "Boris Brejcha",
			title:  "Space X",
			wantID: "ext456",
		},
		{
			name: "penalize continuous full album",
			candidates: []Candidate{
				{ID: "album999", Title: "Boris Brejcha - Full Album 2024", Duration: 3600, Source: "youtube"},
				{ID: "orig123", Title: "Boris Brejcha - Space X (Original Mix)", Duration: 480, Source: "bandcamp"},
			},
			artist: "Boris Brejcha",
			title:  "Space X",
			wantID: "orig123",
		},
		{
			name: "quality score boosts high fidelity track",
			candidates: []Candidate{
				{ID: "low1", Title: "Boris Brejcha - Space X (Extended Mix)", Duration: 500, Source: "youtube", QualityScore: -20},
				{ID: "high2", Title: "Boris Brejcha - Space X (Extended Mix)", Duration: 500, Source: "soundcloud", QualityScore: 30},
			},
			artist: "Boris Brejcha",
			title:  "Space X",
			wantID: "high2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RankCandidates(tt.candidates, tt.artist, tt.title)
			if tt.wantID == "" {
				if got != nil {
					t.Errorf("RankCandidates() = %v, want nil", got)
				}
				return
			}
			if got == nil || got.ID != tt.wantID {
				gotID := ""
				if got != nil {
					gotID = got.ID
				}
				t.Errorf("RankCandidates() ID = %q, want %q", gotID, tt.wantID)
			}
		})
	}
}
