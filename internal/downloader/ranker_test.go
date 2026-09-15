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
		{
			name: "non-english diacritics and umlauts match",
			candidates: []Candidate{
				{ID: "noacc", Title: "Motley Crue - Bose", Duration: 300, Source: "youtube"},
				{ID: "acc1", Title: "Mötley Crüe - BÖSE (Extended Mix)", Duration: 480, Source: "soundcloud"},
			},
			artist: "Motley Crue",
			title:  "Bose",
			wantID: "acc1",
		},
		{
			name: "target remix intent matches requested remixer over competing remixer",
			candidates: []Candidate{
				{ID: "other_remix", Title: "Max Styler - One More (Vintage Culture Remix)", Duration: 320, Source: "youtube"},
				{ID: "solomun_remix", Title: "Max Styler - One More (feat. Ad-Apt) [Solomun Remix]", Duration: 319, Source: "youtube"},
				{ID: "original_mix", Title: "Max Styler - One More (Original Mix)", Duration: 300, Source: "youtube"},
			},
			artist: "Max Styler & Ad-Apt",
			title:  "One More (Solomun Remix)",
			wantID: "solomun_remix",
		},
		{
			name: "negative keywords penalize fan tutorials and covers",
			candidates: []Candidate{
				{ID: "tutorial", Title: "Max Styler - One More (Piano Tutorial Synthesia)", Duration: 300, Source: "youtube"},
				{ID: "slowed", Title: "Max Styler - One More (Slowed + Reverb)", Duration: 300, Source: "youtube"},
				{ID: "real_track", Title: "Max Styler - One More (Official Audio)", Duration: 300, Source: "youtube"},
			},
			artist: "Max Styler",
			title:  "One More",
			wantID: "real_track",
		},
		{
			name: "topic channel and official record label authority bonus",
			candidates: []Candidate{
				{ID: "random_user", Title: "Discip - The Way I Like It", Duration: 240, Source: "youtube", Uploader: "User12345"},
				{ID: "label_channel", Title: "Discip - The Way I Like It", Duration: 240, Source: "youtube", Uploader: "Take Notes", Channel: "Take Notes"},
			},
			artist: "Discip",
			title:  "The Way I Like It",
			wantID: "label_channel",
		},
		{
			name: "extended duration sweet spot beats short teaser",
			candidates: []Candidate{
				{ID: "short_teaser", Title: "Omiki - Maya", Duration: 30, Source: "youtube"},
				{ID: "extended_cut", Title: "Omiki & Phanatic - Maya (feat. David Trindade) [Extended Mix]", Duration: 380, Source: "youtube"},
			},
			artist: "Omiki & Phanatic",
			title:  "Maya (Extended Mix)",
			wantID: "extended_cut",
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
