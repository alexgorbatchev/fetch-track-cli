package downloader

import (
	"sort"
	"strings"
)

// Candidate represents a candidate track from any source (YouTube, SoundCloud, Bandcamp, etc.).
type Candidate struct {
	ID           string  `json:"id"`
	Title        string  `json:"title"`
	Duration     float64 `json:"duration"` // in seconds
	Source       string  `json:"source"`   // e.g., "youtube", "soundcloud", "bandcamp"
	WebpageURL   string  `json:"webpage_url"`
	Score        int     `json:"score,omitempty"`
	BandwidthHz  int     `json:"bandwidth_hz,omitempty"`
	PeakDbFS     float64 `json:"peak_dbfs,omitempty"`
	RMSDbFS      float64 `json:"rms_dbfs,omitempty"`
	QualityScore int     `json:"quality_score,omitempty"`
}

// RankCandidates scores and ranks candidates across all sources to select the best full/extended DJ mix.
func RankCandidates(candidates []Candidate, artist, title string) *Candidate {
	if len(candidates) == 0 {
		return nil
	}

	artistLower := strings.ToLower(artist)
	cleanTitle := strings.ToLower(title)
	for _, kw := range []string{"(radio edit)", "(edit)", "(short mix)", "(single version)"} {
		cleanTitle = strings.ReplaceAll(cleanTitle, kw, "")
	}
	cleanTitle = strings.TrimSpace(cleanTitle)

	scored := make([]Candidate, len(candidates))
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

		// 4. Source preference / Quality adjustment
		if cand.QualityScore != 0 {
			score += cand.QualityScore
		}

		cand.Score = score
	}

	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].Score > scored[j].Score
	})

	best := scored[0]
	return &best
}

// Deprecated alias for backwards compatibility with earlier tests.
type YouTubeCandidate = Candidate

// Deprecated wrapper for backwards compatibility with earlier code.
func RankYouTubeCandidates(candidates []Candidate, artist, title string) *Candidate {
	return RankCandidates(candidates, artist, title)
}
