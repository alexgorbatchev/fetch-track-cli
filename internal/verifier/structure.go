package verifier

import (
	"fmt"
	"strings"
)

// MixStructureReport details the track's duration and mix structure for DJ compatibility.
type MixStructureReport struct {
	IsRadioEditWarning      bool     `json:"isRadioEditWarning"`
	IsOriginalOrExtendedMix bool     `json:"isOriginalOrExtendedMix"`
	DurationFormatted       string   `json:"durationFormatted"`
	MixTypeDescription     string   `json:"mixTypeDescription"`
	DetectedKeywords        []string `json:"detectedKeywords"`
	HasIntroBeats           bool     `json:"hasIntroBeats"`
	HasOutroBeats           bool     `json:"hasOutroBeats"`
}

// FormatDuration converts duration in seconds to M:SS format.
func FormatDuration(seconds float64) string {
	mins := int(seconds) / 60
	secs := int(seconds) % 60
	return fmt.Sprintf("%d:%02d", mins, secs)
}

// AnalyzeMixStructure inspects the track title and duration to detect DJ mix friendliness.
func AnalyzeMixStructure(title string, durationSec float64) MixStructureReport {
	lowerTitle := strings.ToLower(title)

	radioEditKeywords := []string{"radio edit", "short edit", "radio version", "single edit", "short version", "tiktok edit"}
	extendedMixKeywords := []string{"original mix", "extended mix", "club mix", "dub mix", "extended version", "12 inch", "vip mix"}

	var detectedRadioKeywords []string
	for _, kw := range radioEditKeywords {
		if strings.Contains(lowerTitle, kw) {
			detectedRadioKeywords = append(detectedRadioKeywords, kw)
		}
	}

	var detectedExtendedKeywords []string
	for _, kw := range extendedMixKeywords {
		if strings.Contains(lowerTitle, kw) {
			detectedExtendedKeywords = append(detectedExtendedKeywords, kw)
		}
	}

	isRadioKeywordPresent := len(detectedRadioKeywords) > 0
	isExtendedKeywordPresent := len(detectedExtendedKeywords) > 0

	isShortDuration := durationSec < 150
	isMediumDuration := durationSec >= 150 && durationSec < 240
	isLongDuration := durationSec >= 240

	isRadioEditWarning := isRadioKeywordPresent || (isShortDuration && !isExtendedKeywordPresent)
	isOriginalOrExtendedMix := isExtendedKeywordPresent || (isLongDuration && !isRadioKeywordPresent)

	mixTypeDescription := "Standard Track"
	switch {
	case isRadioKeywordPresent:
		mixTypeDescription = "Radio Edit (Short)"
	case isExtendedKeywordPresent:
		mixTypeDescription = "Original / Extended DJ Mix"
	case isShortDuration:
		mixTypeDescription = "Short Snippet (< 2.5 mins)"
	case isLongDuration:
		mixTypeDescription = "Full Length Track (> 4.0 mins)"
	case isMediumDuration:
		mixTypeDescription = "Standard Track (2.5 - 4.0 mins)"
	}

	var allDetected []string
	allDetected = append(allDetected, detectedRadioKeywords...)
	allDetected = append(allDetected, detectedExtendedKeywords...)

	return MixStructureReport{
		IsRadioEditWarning:      isRadioEditWarning,
		IsOriginalOrExtendedMix: isOriginalOrExtendedMix,
		DurationFormatted:       FormatDuration(durationSec),
		MixTypeDescription:     mixTypeDescription,
		DetectedKeywords:        allDetected,
		HasIntroBeats:           durationSec > 210,
		HasOutroBeats:           durationSec > 210,
	}
}
