package downloader

import (
	"sort"
	"strings"
)

// YouTubeCandidate represents a YouTube search candidate with metadata for mix ranking.
type YouTubeCandidate struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Duration float64 `json:"duration"` // in seconds
	Score    int    `json:"score,omitempty"`
}

// RankYouTubeCandidates scores and ranks YouTube search candidates to find the best full/extended DJ mix.
func RankYouTubeCandidates(candidates []YouTubeCandidate, artist, title string) *YouTubeCandidate {
	if len(candidates) == 0 {
		return nil
	}

	artistLower := strings.ToLower(artist)
	cleanTitle := strings.ToLower(title)
	for _, kw := range []string{"(radio edit)", "(edit)", "(short mix)", "(single version)"} {
		cleanTitle = strings.ReplaceAll(cleanTitle, kw, "")
	}
	cleanTitle = strings.TrimSpace(cleanTitle)

	scored := make([]YouTubeCandidate, len(candidates))
	copy(scored, candidates)

	for i := range scored {
		cand := &scored[i]
		score := 0
		candTitleLower := strings.ToLower(cand.Title)

		// 1. Duration Scoring (Prefer full-length DJ mix 4.5m - 13m)
		switch {
		case cand.Duration >= 300 && cand.Duration <= 780:
			score += 60 // Ideal 5m - 13m full mix length
		case cand.Duration >= 240 && cand.Duration < 300:
			score += 30 // Acceptable 4m - 5m length
		case cand.Duration < 220:
			score -= 60 // Penalty for short radio edit (< 3.6m)
		case cand.Duration > 900:
			score -= 100 // Penalty for 15m+ full album / continuous mix
		}

		// 2. Keyword Scoring for Full / Extended Mixes
		for _, kw := range []string{"extended", "original mix", "club mix", "dub mix", "12 inch", `12"`, "full version"} {
			if strings.Contains(candTitleLower, kw) {
				score += 40
				break
			}
		}

		for _, kw := range []string{"radio edit", "short edit", "single edit", "official video"} {
			if strings.Contains(candTitleLower, kw) {
				score -= 40
				break
			}
		}

		// 3. Artist/Title Match
		if artistLower != "" && strings.Contains(candTitleLower, artistLower) {
			score += 20
		}
		if cleanTitle != "" && strings.Contains(candTitleLower, cleanTitle) {
			score += 20
		}

		cand.Score = score
	}

	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].Score > scored[j].Score
	})

	best := scored[0]
	return &best
}
