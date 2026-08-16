package downloader

import (
	"sort"
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
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

var normTransformer = transform.Chain(
	norm.NFD,
	runes.Remove(runes.In(unicode.Mn)),
	norm.NFC,
)

// NormalizeUnicode performs full Unicode NFD decomposition, strips all non-spacing combining
// diacritics/accents via golang.org/x/text, maps Cyrillic Yo, and returns a clean lowercase string.
func NormalizeUnicode(input string) string {
	s := strings.ToLower(input)
	// Map Cyrillic Yo (ё -> е) for Cyrillic search compatibility
	s = strings.ReplaceAll(s, "ё", "е")

	res, _, err := transform.String(normTransformer, s)
	if err != nil {
		return s
	}
	return res
}

// RankCandidates scores and ranks candidates across all sources to select the best full/extended DJ mix.
func RankCandidates(candidates []Candidate, artist, title string) *Candidate {
	if len(candidates) == 0 {
		return nil
	}

	artistLower := strings.ToLower(artist)
	artistNorm := NormalizeUnicode(artistLower)

	cleanTitle := strings.ToLower(title)
	for _, kw := range []string{"(radio edit)", "(edit)", "(short mix)", "(single version)"} {
		cleanTitle = strings.ReplaceAll(cleanTitle, kw, "")
	}
	cleanTitle = strings.TrimSpace(cleanTitle)
	cleanTitleNorm := NormalizeUnicode(cleanTitle)

	titleWords := strings.Fields(cleanTitle)
	titleNormWords := strings.Fields(cleanTitleNorm)

	scored := make([]Candidate, len(candidates))
	copy(scored, candidates)

	for i := range scored {
		cand := &scored[i]
		score := 0
		candTitleLower := strings.ToLower(cand.Title)
		candTitleNorm := NormalizeUnicode(cand.Title)

		// 1. Mandatory Title Matching Rule (exact or normalized)
		hasFullTitle := (cleanTitle != "" && strings.Contains(candTitleLower, cleanTitle)) ||
			(cleanTitleNorm != "" && strings.Contains(candTitleNorm, cleanTitleNorm))

		hasWordMatch := false
		if !hasFullTitle {
			if len(titleWords) > 0 {
				matchCount := 0
				for _, w := range titleWords {
					if len(w) >= 3 && (strings.Contains(candTitleLower, w) || strings.Contains(candTitleNorm, NormalizeUnicode(w))) {
						matchCount++
					}
				}
				if matchCount == len(titleWords) {
					hasWordMatch = true
				}
			}
			if !hasWordMatch && len(titleNormWords) > 0 {
				normMatchCount := 0
				for _, w := range titleNormWords {
					if len(w) >= 3 && strings.Contains(candTitleNorm, w) {
						normMatchCount++
					}
				}
				if normMatchCount == len(titleNormWords) {
					hasWordMatch = true
				}
			}
		}

		if hasFullTitle {
			score += 100
		} else if hasWordMatch {
			score += 60
		} else {
			// Heavy penalty if candidate title does not match target track title
			score -= 300
		}

		// 2. Artist Matching Rule (exact or normalized)
		if artistLower != "" {
			if strings.Contains(candTitleLower, artistLower) || (artistNorm != "" && strings.Contains(candTitleNorm, artistNorm)) {
				score += 100
			} else {
				artistWords := strings.Fields(artistNorm)
				artistMatchCount := 0
				for _, w := range artistWords {
					if len(w) >= 3 && strings.Contains(candTitleNorm, w) {
						artistMatchCount++
					}
				}
				if artistMatchCount > 0 {
					score += 50
				}
			}
		}

		// 3. Keyword Scoring for Extended / Full DJ Mixes
		for _, kw := range []string{"extended", "original mix", "club mix", "dub mix", "12 inch", `12"`, "full version"} {
			if strings.Contains(candTitleLower, kw) {
				score += 50
				break
			}
		}

		for _, kw := range []string{"radio edit", "short edit", "single edit"} {
			if strings.Contains(candTitleLower, kw) {
				score -= 30
				break
			}
		}

		// 4. Duration Scoring (Prefer full-length DJ mixes 4m - 13m; heavily penalize < 2m preview clips)
		switch {
		case cand.Duration >= 240 && cand.Duration <= 780:
			score += 50 // Ideal 4m - 13m full mix length
		case cand.Duration >= 150 && cand.Duration < 240:
			score += 20 // Acceptable single length (2.5m - 4m)
		case cand.Duration < 120:
			score -= 300 // Heavy penalty for < 2 minute short preview clips / snippets
		case cand.Duration < 150:
			score -= 60 // Penalty for short clips (2.0m - 2.5m)
		case cand.Duration > 900:
			score -= 300 // Heavy penalty for 15m+ long compilation / full album mix
		}

		// 5. External Quality Adjustment
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
