package verifier

import (
	"context"
	"strings"
	"unicode"

	"github.com/lithammer/fuzzysearch/fuzzy"
	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// normalizeText performs Unicode NFD decomposition, diacritic removal, and lowercase mapping.
func normalizeText(input string) string {
	s := strings.ToLower(input)
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

// FingerprintVerificationResult describes the outcome of acoustic fingerprint verification.
type FingerprintVerificationResult struct {
	Confirmed     bool
	Mismatch      bool
	Source        string
	MatchedTitle  string
	MatchedArtist string
	Reason        string
}

// VerifyAcousticFingerprint inspects audio file via tag-track dry-run and compares against expected artist and title.
func VerifyAcousticFingerprint(ctx context.Context, runner CommandRunner, filePath, targetArtist, targetTitle string) (*FingerprintVerificationResult, error) {
	if runner == nil {
		runner = GetDefaultRunner()
	}

	outBytes, err := runner(ctx, "tag-track", "--dry-run", filePath)
	if err != nil {
		return &FingerprintVerificationResult{
			Confirmed: false,
			Mismatch:  false,
			Reason:    "tag-track execution failed: " + err.Error(),
		}, nil
	}

	out := string(outBytes)
	var matchedTitle, matchedArtist, source string

	for _, rawLine := range strings.Split(out, "\n") {
		line := strings.TrimSpace(rawLine)
		if strings.HasPrefix(line, "title: ") {
			matchedTitle = strings.TrimPrefix(line, "title: ")
		} else if strings.HasPrefix(line, "artist: ") {
			matchedArtist = strings.TrimPrefix(line, "artist: ")
		} else if strings.HasPrefix(line, "source: ") {
			source = strings.TrimPrefix(line, "source: ")
		}
	}

	// Only evaluate if acoustic identification succeeded (Shazam API or AcoustID)
	isAcousticMatch := strings.Contains(strings.ToLower(source), "shazam") ||
		strings.Contains(strings.ToLower(source), "acoustid") ||
		strings.Contains(strings.ToLower(source), "fingerprint")

	if !isAcousticMatch {
		return &FingerprintVerificationResult{
			Confirmed:     false,
			Mismatch:      false,
			Source:        source,
			MatchedTitle:  matchedTitle,
			MatchedArtist: matchedArtist,
			Reason:        "Track not in acoustic catalog (unverified)",
		}, nil
	}

	// Compare matched title and artist against target
	cleanTargetTitle := strings.ToLower(targetTitle)
	for _, kw := range []string{"(radio edit)", "(edit)", "(short mix)", "(single version)", "(original mix)"} {
		cleanTargetTitle = strings.ReplaceAll(cleanTargetTitle, kw, "")
	}
	cleanTargetTitle = strings.TrimSpace(cleanTargetTitle)
	cleanTargetTitleNorm := normalizeText(cleanTargetTitle)

	baseTarget := cleanTargetTitle
	if idx := strings.IndexAny(baseTarget, "(["); idx > 0 {
		baseTarget = strings.TrimSpace(baseTarget[:idx])
	}
	baseTargetNorm := normalizeText(baseTarget)

	matchedTitleNorm := normalizeText(matchedTitle)
	matchedArtistNorm := normalizeText(matchedArtist)
	targetArtistNorm := normalizeText(targetArtist)

	titleMatches := false
	if baseTargetNorm != "" && (strings.Contains(matchedTitleNorm, baseTargetNorm) || strings.Contains(baseTargetNorm, matchedTitleNorm)) {
		titleMatches = true
	} else if cleanTargetTitleNorm != "" && (strings.Contains(matchedTitleNorm, cleanTargetTitleNorm) || strings.Contains(cleanTargetTitleNorm, matchedTitleNorm)) {
		titleMatches = true
	} else if cleanTargetTitleNorm != "" && fuzzy.MatchFold(cleanTargetTitleNorm, matchedTitleNorm) {
		titleMatches = true
	} else {
		// Check token match
		tokens := strings.FieldsFunc(baseTargetNorm, func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsNumber(r)
		})
		for _, tok := range tokens {
			if len(tok) >= 3 && strings.Contains(matchedTitleNorm, tok) {
				titleMatches = true
				break
			}
		}
	}

	artistMatches := false
	if targetArtistNorm == "" || matchedArtistNorm == "" {
		artistMatches = true
	} else if strings.Contains(matchedArtistNorm, targetArtistNorm) || strings.Contains(targetArtistNorm, matchedArtistNorm) || fuzzy.MatchFold(targetArtistNorm, matchedArtistNorm) {
		artistMatches = true
	} else {
		for _, tok := range strings.Fields(targetArtistNorm) {
			if len(tok) >= 3 && strings.Contains(matchedArtistNorm, tok) {
				artistMatches = true
				break
			}
		}
	}

	if titleMatches && artistMatches {
		return &FingerprintVerificationResult{
			Confirmed:     true,
			Mismatch:      false,
			Source:        source,
			MatchedTitle:  matchedTitle,
			MatchedArtist: matchedArtist,
			Reason:        "Acoustic fingerprint matched target track (" + source + ")",
		}, nil
	}

	return &FingerprintVerificationResult{
		Confirmed:     false,
		Mismatch:      true,
		Source:        source,
		MatchedTitle:  matchedTitle,
		MatchedArtist: matchedArtist,
		Reason:        "Acoustic fingerprint mismatch: identified as \"" + matchedArtist + " - " + matchedTitle + "\" (" + source + ")",
	}, nil
}
