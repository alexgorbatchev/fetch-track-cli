package downloader

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/lithammer/fuzzysearch/fuzzy"
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
	Uploader     string  `json:"uploader,omitempty"`
	Channel      string  `json:"channel,omitempty"`
	WebpageURL   string  `json:"webpage_url"`
	Score        int     `json:"score,omitempty"`
	ScoreReason  string  `json:"score_reason,omitempty"`
	BandwidthHz  int     `json:"bandwidth_hz,omitempty"`
	PeakDbFS     float64 `json:"peak_dbfs,omitempty"`
	RMSDbFS      float64 `json:"rms_dbfs,omitempty"`
	QualityScore int     `json:"quality_score,omitempty"`
}

// NormalizeUnicode performs full Unicode NFD decomposition, strips all non-spacing combining
// diacritics/accents via golang.org/x/text, maps Cyrillic Yo, and returns a clean lowercase string.
func NormalizeUnicode(input string) string {
	s := strings.ToLower(input)
	// Map Cyrillic Yo (ё -> е) for Cyrillic search compatibility
	s = strings.ReplaceAll(s, "ё", "е")

	t := transform.Chain(
		norm.NFD,
		runes.Remove(runes.In(unicode.Mn)),
		norm.NFC,
	)

	res, _, err := transform.String(t, s)
	if err != nil {
		return s
	}
	return res
}

// DeduplicateCandidates removes duplicate candidates based on WebpageURL, ID, or matching Source+Uploader+Title+Duration.
func DeduplicateCandidates(candidates []Candidate) []Candidate {
	if len(candidates) <= 1 {
		return candidates
	}

	seenURL := make(map[string]bool)
	seenID := make(map[string]bool)
	seenKey := make(map[string]bool)

	var unique []Candidate

	for _, c := range candidates {
		normURL := strings.ToLower(strings.TrimSpace(c.WebpageURL))
		if normURL != "" {
			if seenURL[normURL] {
				continue
			}
			seenURL[normURL] = true
		}

		if c.ID != "" {
			idKey := fmt.Sprintf("%s:%s", strings.ToLower(c.Source), strings.ToLower(c.ID))
			if seenID[idKey] {
				continue
			}
			seenID[idKey] = true
		}

		durSec := int(c.Duration)
		titleNorm := NormalizeUnicode(c.Title)
		uploaderNorm := NormalizeUnicode(c.Uploader)
		compositeKey := fmt.Sprintf("%s:%s:%s:%d", strings.ToLower(c.Source), uploaderNorm, titleNorm, durSec)
		if seenKey[compositeKey] {
			continue
		}
		seenKey[compositeKey] = true

		unique = append(unique, c)
	}

	return unique
}

var (
	remixRegex = regexp.MustCompile(`(?i)(?:[\(\[\-]\s*)([a-z0-9\s&._-]+?)\s+(?:remix|mix|edit|dub|flip|vip|rework|bootleg)\s*[\)\]]?`)
	negativeKeywords = []string{
		"cover", "tutorial", "how to play", "reaction", "reacting to",
		"lesson", "slowed + reverb", "slowed and reverb", "slowed & reverb",
		"nightcore", "8d audio", "1 hour loop", "10 hours", "bass boosted",
		"clean version", "censored", "karaoke", "instrumental remake",
		"acapella cover", "live at", "live from", "concert", "drum cover",
		"guitar cover", "piano cover", "synthesia", "review",
	}
	knownRecordLabels = []string{
		"defected", "armada", "spinnin", "anjunabeats", "anjunadeep", "drumcode",
		"afterlife", "ministry of sound", "ultra records", "ultra music",
		"take notes", "nu moda", "spin twist", "blue tunes", "affairs", "warner",
		"sony music", "universal music", "mau5trap", "crosstown rebels",
		"diynamic", "toolroom", "cr2 records", "kontor", "black hole recordings",
	}
)

// ExtractRemixer finds any specific remixer name specified in the title/query.
func ExtractRemixer(queryOrTitle string) string {
	lower := strings.ToLower(queryOrTitle)
	// Check patterns like "(Solomun Remix)" or "[Solomun Remix]" or "Solomun Remix"
	matches := remixRegex.FindStringSubmatch(lower)
	if len(matches) > 1 {
		remixer := strings.TrimSpace(matches[1])
		// Filter out generic descriptors
		if remixer != "original" && remixer != "extended" && remixer != "club" &&
			remixer != "radio" && remixer != "short" && remixer != "vip" &&
			remixer != "dub" && remixer != "instrumental" && remixer != "" {
			return remixer
		}
	}
	return ""
}

// IsNegativeCandidate checks if candidate title contains disqualified fan/tutorial keywords.
func IsNegativeCandidate(candTitleLower string) (bool, string) {
	for _, kw := range negativeKeywords {
		if strings.Contains(candTitleLower, kw) {
			return true, kw
		}
	}
	return false, ""
}

// RankAllCandidates ranks and scores all candidates in-place and returns the scored slice.
func RankAllCandidates(candidates []Candidate, artist, title string) []Candidate {
	candidates = DeduplicateCandidates(candidates)
	if len(candidates) == 0 {
		return nil
	}

	artistLower := strings.ToLower(artist)
	artistNorm := NormalizeUnicode(artistLower)

	cleanTitle := strings.ToLower(title)
	for _, kw := range []string{"(radio edit)", "(edit)", "(short mix)", "(single version)", "(original mix)"} {
		cleanTitle = strings.ReplaceAll(cleanTitle, kw, "")
	}
	cleanTitle = strings.TrimSpace(cleanTitle)
	cleanTitleNorm := NormalizeUnicode(cleanTitle)

	// Identify remix intent from query/title
	targetRemixer := ExtractRemixer(title)
	if targetRemixer == "" {
		targetRemixer = ExtractRemixer(cleanTitle)
	}

	isExtendedRequested := strings.Contains(strings.ToLower(title), "extended") || strings.Contains(cleanTitle, "extended")
	isRadioRequested := strings.Contains(strings.ToLower(title), "radio") || strings.Contains(cleanTitle, "radio")

	// Split individual artist tokens for collaborations (e.g. "Max Styler & Ad-Apt")
	var artistTokens []string
	if artistLower != "" {
		for _, part := range strings.FieldsFunc(artistLower, func(r rune) bool {
			return r == '&' || r == ',' || r == '/' || r == '+' || r == 'x'
		}) {
			p := strings.TrimSpace(part)
			p = strings.TrimPrefix(p, "feat.")
			p = strings.TrimPrefix(p, "ft.")
			p = strings.TrimSpace(p)
			if p != "" {
				artistTokens = append(artistTokens, NormalizeUnicode(p))
			}
		}
	}

	for i := range candidates {
		cand := &candidates[i]
		score := 0
		var reasons []string

		candTitleLower := strings.ToLower(cand.Title)
		candTitleNorm := NormalizeUnicode(cand.Title)
		candUploaderLower := strings.ToLower(cand.Uploader + " " + cand.Channel)

		// 0. Negative Keyword Check
		if isNeg, kw := IsNegativeCandidate(candTitleLower); isNeg {
			score -= 300
			reasons = append(reasons, fmt.Sprintf("penalty_negative_kw(%s)", kw))
		}

		// 1. Title Matching
		baseTitle := cleanTitle
		if idx := strings.IndexAny(baseTitle, "(["); idx > 0 {
			baseTitle = strings.TrimSpace(baseTitle[:idx])
		}
		baseTitleNorm := NormalizeUnicode(baseTitle)

		if baseTitleNorm != "" && (strings.Contains(candTitleLower, baseTitle) || strings.Contains(candTitleNorm, baseTitleNorm)) {
			score += 100
			reasons = append(reasons, "base_title_match")
		} else if cleanTitleNorm != "" && (strings.Contains(candTitleLower, cleanTitle) || strings.Contains(candTitleNorm, cleanTitleNorm)) {
			score += 100
			reasons = append(reasons, "title_match")
		} else if cleanTitleNorm != "" && fuzzy.MatchFold(cleanTitleNorm, candTitleNorm) {
			score += 60
			reasons = append(reasons, "title_fuzzy_match")
		} else {
			// Check if major tokens of base title match
			matchedTokens := 0
			tokens := strings.FieldsFunc(baseTitleNorm, func(r rune) bool {
				return !unicode.IsLetter(r) && !unicode.IsNumber(r)
			})
			for _, tok := range tokens {
				if len(tok) >= 3 && strings.Contains(candTitleNorm, tok) {
					matchedTokens++
				}
			}
			if matchedTokens > 0 {
				score += 50 * matchedTokens
				reasons = append(reasons, fmt.Sprintf("partial_title_tokens(%d)", matchedTokens))
			} else {
				score -= 300
				reasons = append(reasons, "title_mismatch")
			}
		}

		// 2. Artist Matching (Tokenized for multi-artist collaborations)
		if len(artistTokens) > 0 {
			matchedArtists := 0
			for _, token := range artistTokens {
				if strings.Contains(candTitleNorm, token) || strings.Contains(candUploaderLower, token) {
					matchedArtists++
				}
			}
			if matchedArtists == len(artistTokens) {
				score += 100
				reasons = append(reasons, "all_artists_match")
			} else if matchedArtists > 0 {
				score += 50 * matchedArtists
				reasons = append(reasons, fmt.Sprintf("partial_artist_match(%d/%d)", matchedArtists, len(artistTokens)))
			}
		} else if artistNorm != "" {
			if strings.Contains(candTitleLower, artistLower) || strings.Contains(candTitleNorm, artistNorm) {
				score += 100
				reasons = append(reasons, "artist_match")
			} else if fuzzy.MatchFold(artistNorm, candTitleNorm) {
				score += 50
				reasons = append(reasons, "artist_fuzzy_match")
			}
		}

		// 3. Exact Title + Artist Match Bonus
		if artistLower != "" && cleanTitle != "" {
			expectedFull := fmt.Sprintf("%s - %s", artistLower, cleanTitle)
			if candTitleLower == expectedFull || candTitleLower == cleanTitle {
				score += 60
				reasons = append(reasons, "exact_full_match")
			}
		}

		// 4. Version & Remix Intent Matching (Item 1)
		if targetRemixer != "" {
			if strings.Contains(candTitleNorm, targetRemixer) {
				score += 150
				reasons = append(reasons, fmt.Sprintf("target_remix_match(%s)", targetRemixer))
			} else {
				// Penalize if candidate has a different remixer
				candRemixer := ExtractRemixer(cand.Title)
				if candRemixer != "" && candRemixer != targetRemixer {
					score -= 250
					reasons = append(reasons, fmt.Sprintf("different_remix_penalty(%s)", candRemixer))
				} else {
					score -= 100
					reasons = append(reasons, "missing_target_remix")
				}
			}
		} else {
			// No specific remix requested: penalize unrequested third-party remixers
			candRemixer := ExtractRemixer(cand.Title)
			if candRemixer != "" && !strings.Contains(artistLower, candRemixer) {
				score -= 100
				reasons = append(reasons, fmt.Sprintf("unrequested_remix(%s)", candRemixer))
			}
		}

		// 5. Extended vs Radio Intent
		if isExtendedRequested {
			if strings.Contains(candTitleLower, "extended") || strings.Contains(candTitleLower, "club mix") {
				score += 80
				reasons = append(reasons, "extended_intent_match")
			}
			if strings.Contains(candTitleLower, "radio") || strings.Contains(candTitleLower, "short edit") {
				score -= 150
				reasons = append(reasons, "radio_penalty_on_extended_intent")
			}
		} else if isRadioRequested {
			if strings.Contains(candTitleLower, "radio") || strings.Contains(candTitleLower, "edit") {
				score += 80
				reasons = append(reasons, "radio_intent_match")
			}
		} else {
			// Standard DJ search: reward extended / full mixes
			for _, kw := range []string{"extended", "original mix", "club mix", "dub mix", "12 inch", `12"`, "full version"} {
				if strings.Contains(candTitleLower, kw) {
					score += 50
					reasons = append(reasons, fmt.Sprintf("dj_version_bonus(%s)", kw))
					break
				}
			}
			for _, kw := range []string{"radio edit", "short edit", "single edit"} {
				if strings.Contains(candTitleLower, kw) {
					score -= 30
					reasons = append(reasons, "radio_penalty")
					break
				}
			}
		}

		// 6. Channel & Source Authority (Item 3)
		if strings.HasSuffix(cand.Uploader, "- Topic") || strings.HasSuffix(cand.Channel, "- Topic") {
			score += 50
			reasons = append(reasons, "official_topic_channel")
		} else {
			for _, label := range knownRecordLabels {
				if strings.Contains(candUploaderLower, label) {
					score += 40
					reasons = append(reasons, fmt.Sprintf("known_label(%s)", label))
					break
				}
			}
		}

		if strings.Contains(candTitleLower, "[official audio]") || strings.Contains(candTitleLower, "(official audio)") ||
			strings.Contains(candTitleLower, "[official video]") || strings.Contains(candTitleLower, "(official video)") {
			score += 30
			reasons = append(reasons, "official_badge")
		}

		// 7. Duration-Band Scoring Curve (Item 4: DJ Optimized)
		switch {
		case cand.Duration >= 270 && cand.Duration <= 540:
			// Ideal 4.5m - 9m extended DJ mix sweet spot
			bonus := 120 + int(cand.Duration/10)
			score += bonus
			reasons = append(reasons, fmt.Sprintf("duration_extended_sweetspot(+%d)", bonus))
		case cand.Duration > 540 && cand.Duration <= 900:
			// 9m - 15m long club mix
			bonus := 60 + int(cand.Duration/15)
			score += bonus
			reasons = append(reasons, fmt.Sprintf("duration_long_club(+%d)", bonus))
		case cand.Duration >= 180 && cand.Duration < 270:
			// 3m - 4.5m standard single / radio cut
			score += 40
			reasons = append(reasons, "duration_standard_single(+40)")
		case cand.Duration >= 90 && cand.Duration < 180:
			// 1.5m - 3m short cut
			if isRadioRequested {
				score += 30
				reasons = append(reasons, "duration_short_radio(+30)")
			} else {
				score -= 50
				reasons = append(reasons, "duration_short_penalty(-50)")
			}
		case cand.Duration < 90:
			// < 1.5m preview snippet / teaser
			score -= 400
			reasons = append(reasons, "duration_teaser_disqualify(-400)")
		case cand.Duration > 900:
			// > 15m full DJ set / continuous compilation
			score -= 400
			reasons = append(reasons, "duration_full_set_disqualify(-400)")
		}

		// 8. Quality Score Adjustment
		if cand.QualityScore != 0 {
			score += cand.QualityScore
			reasons = append(reasons, fmt.Sprintf("quality_score(%d)", cand.QualityScore))
		}

		cand.Score = score
		cand.ScoreReason = strings.Join(reasons, ", ")
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].Score > candidates[j].Score
	})

	return candidates
}

// RankCandidates scores and ranks candidates across all sources to select the best full/extended DJ mix.
func RankCandidates(candidates []Candidate, artist, title string) *Candidate {
	ranked := RankAllCandidates(candidates, artist, title)
	if len(ranked) == 0 {
		return nil
	}
	return &ranked[0]
}

// Deprecated alias for backwards compatibility with earlier tests.
type YouTubeCandidate = Candidate

// Deprecated wrapper for backwards compatibility with earlier code.
func RankYouTubeCandidates(candidates []Candidate, artist, title string) *Candidate {
	return RankCandidates(candidates, artist, title)
}
