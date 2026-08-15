package downloader

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var (
	// ErrNoCandidateFound indicates no suitable candidate was found on YouTube.
	ErrNoCandidateFound = errors.New("no suitable candidate found on YouTube search")
	// ErrDownloadFailed indicates yt-dlp failed to download the audio stream.
	ErrDownloadFailed = errors.New("failed to download audio stream across client fallbacks")
)

// SearchYouTubeCandidates searches YouTube for candidates matching the query and ranks them.
func SearchYouTubeCandidates(ctx context.Context, artist, title, rawQuery string) (string, error) {
	cleanTitle := title
	for _, kw := range []string{"(radio edit)", "(edit)", "(short mix)", "(single version)"} {
		cleanTitle = strings.ReplaceAll(cleanTitle, kw, "")
	}
	cleanTitle = strings.TrimSpace(cleanTitle)

	var queries []string
	if artist != "" && cleanTitle != "" {
		queries = []string{
			fmt.Sprintf("%s %s", artist, cleanTitle),
			fmt.Sprintf("%s %s Extended Mix", artist, cleanTitle),
		}
	} else {
		queries = []string{
			rawQuery,
			fmt.Sprintf("%s Extended Mix", rawQuery),
		}
	}

	candidatesMap := make(map[string]YouTubeCandidate)

	for _, query := range queries {
		cmd := exec.CommandContext(ctx, "yt-dlp",
			"--flat-playlist",
			"--dump-json",
			"--no-warnings",
			"--quiet",
			"--js-runtimes", "node",
			"--extractor-args", "youtube:player_client=android_vr,web",
			fmt.Sprintf("ytsearch8:%s", query),
		)

		var stdout bytes.Buffer
		cmd.Stdout = &stdout
		_ = cmd.Run() // ignore errors for individual search queries

		scanner := bufio.NewScanner(&stdout)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.TrimSpace(line) == "" {
				continue
			}

			var cand struct {
				ID       string  `json:"id"`
				Title    string  `json:"title"`
				Duration float64 `json:"duration"`
			}
			if err := json.Unmarshal([]byte(line), &cand); err == nil && cand.ID != "" && cand.Title != "" && cand.Duration > 0 {
				if _, exists := candidatesMap[cand.ID]; !exists {
					candidatesMap[cand.ID] = YouTubeCandidate{
						ID:       cand.ID,
						Title:    cand.Title,
						Duration: cand.Duration,
					}
				}
			}
		}
	}

	if len(candidatesMap) == 0 {
		return "", ErrNoCandidateFound
	}

	candidateList := make([]YouTubeCandidate, 0, len(candidatesMap))
	for _, c := range candidatesMap {
		candidateList = append(candidateList, c)
	}

	best := RankYouTubeCandidates(candidateList, artist, title)
	if best == nil {
		return "", ErrNoCandidateFound
	}

	return fmt.Sprintf("https://www.youtube.com/watch?v=%s", best.ID), nil
}

// DownloadAudioStream downloads the M4A/audio stream from targetURL into outDir.
// Returns the full path of the newly downloaded audio file.
func DownloadAudioStream(ctx context.Context, targetURL, outDir string) (string, error) {
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return "", fmt.Errorf("creating output directory: %w", err)
	}

	initialFiles, err := listAudioFiles(outDir)
	if err != nil {
		return "", fmt.Errorf("listing initial files in %s: %w", outDir, err)
	}

	extractorArgsList := []string{
		"youtube:player_client=android_vr,web",
		"youtube:player_client=android_vr,mweb",
	}

	outPattern := filepath.Join(outDir, "%(title)s.%(ext)s")

	for _, extractorArgs := range extractorArgsList {
		cmdCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		cmd := exec.CommandContext(cmdCtx, "yt-dlp",
			"--no-warnings",
			"--quiet",
			"--js-runtimes", "node",
			"--extractor-args", extractorArgs,
			"--match-filter", "duration <= 900",
			"-f", "140/bestaudio[ext=m4a]/bestaudio",
			"-x",
			"--embed-metadata",
			"--embed-thumbnail",
			"-o", outPattern,
			targetURL,
		)

		err := cmd.Run()
		cancel()

		if err == nil {
			currentFiles, err := listAudioFiles(outDir)
			if err == nil {
				for file := range currentFiles {
					if !initialFiles[file] {
						return filepath.Join(outDir, file), nil
					}
				}
			}
		}
	}

	return "", ErrDownloadFailed
}

func listAudioFiles(dir string) (map[string]bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	files := make(map[string]bool)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext == ".m4a" || ext == ".opus" || ext == ".mp3" {
			files[entry.Name()] = true
		}
	}
	return files, nil
}
