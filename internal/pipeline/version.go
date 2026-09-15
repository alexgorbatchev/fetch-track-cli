package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var versionRegex = regexp.MustCompile(`(?i)\b(instrumental|acapella|a cappella|vip|dub mix|dub|extended mix|club mix)\b`)

// PreserveVersionInTitle ensures key version tags in sourceTitle (like Instrumental or VIP) are preserved if enrichment stripped them.
func PreserveVersionInTitle(sourceTitle, enrichedTitle string) string {
	if enrichedTitle == "" {
		return sourceTitle
	}

	sourceLower := strings.ToLower(sourceTitle)
	enrichedLower := strings.ToLower(enrichedTitle)

	matches := versionRegex.FindAllString(sourceLower, -1)
	for _, m := range matches {
		cleanTag := strings.ToLower(strings.TrimSpace(m))
		if cleanTag != "" && !strings.Contains(enrichedLower, cleanTag) {
			// Title casing the tag
			tagFormatted := strings.Title(cleanTag)
			if cleanTag == "vip" {
				tagFormatted = "VIP"
			}
			enrichedTitle = fmt.Sprintf("%s (%s)", enrichedTitle, tagFormatted)
			enrichedLower = strings.ToLower(enrichedTitle)
		}
	}
	return enrichedTitle
}

// ResolveCollisionSafePath ensures that if a destination file already exists and is not the same file, a safe unique path is returned.
func ResolveCollisionSafePath(targetPath, currentPath string) string {
	if targetPath == currentPath {
		return targetPath
	}

	// Check if targetPath exists
	if _, err := os.Stat(targetPath); err != nil {
		// File does not exist, safe to use
		return targetPath
	}

	// Target exists and is a different file: generate unique filename
	dir := filepath.Dir(targetPath)
	ext := filepath.Ext(targetPath)
	baseName := strings.TrimSuffix(filepath.Base(targetPath), ext)

	for i := 1; i <= 100; i++ {
		candidate := filepath.Join(dir, fmt.Sprintf("%s (%d)%s", baseName, i, ext))
		if candidate == currentPath {
			return candidate
		}
		if _, err := os.Stat(candidate); err != nil {
			return candidate
		}
	}

	return targetPath
}
