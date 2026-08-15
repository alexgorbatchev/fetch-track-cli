package metadata

import (
	"regexp"
	"strings"
)

var (
	dashRe    = regexp.MustCompile(`[–—−‐‑‒―]`)
	illegalRe = regexp.MustCompile("[/\\\\?%*:|\"<>`]")
	hyphenRe  = regexp.MustCompile(`\s+-\s+`)
	spaceRe   = regexp.MustCompile(`\s+`)
)

// SanitizeFilename normalizes hyphens, replaces illegal OS characters with '_',
// cleans spaces around hyphens, and trims leading/trailing spaces.
func SanitizeFilename(input string) string {
	res := dashRe.ReplaceAllString(input, "-")
	res = illegalRe.ReplaceAllString(res, "_")
	res = hyphenRe.ReplaceAllString(res, " - ")
	res = spaceRe.ReplaceAllString(res, " ")
	return strings.TrimSpace(res)
}
