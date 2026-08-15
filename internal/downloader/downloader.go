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
	"sync"
	"time"

	"github.com/dj/fetch-track-cli/internal/verifier"
)

var (
	// ErrNoCandidateFound indicates no suitable candidate was found across sources.
	ErrNoCandidateFound = errors.New("no suitable candidate found across sources")
	// ErrDownloadFailed indicates yt-dlp failed to download the audio stream.
	ErrDownloadFailed = errors.New("failed to download audio stream across client fallbacks")
)

// MapSourceSearchPrefix returns the yt-dlp search prefix for a given source name.
func MapSourceSearchPrefix(source string) string {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "youtube":
		return "ytsearch8"
	case "soundcloud":
		return "scsearch8"
	case "bandcamp":
		return "bcsearch8"
	default:
		return "ytsearch8"
	}
}

// SearchSourcesInParallel searches all configured sources concurrently for track candidates.
func SearchSourcesInParallel(ctx context.Context, sources []string, artist, title, rawQuery string) ([]Candidate, error) {
	if len(sources) == 0 {
		sources = []string{"youtube", "soundcloud", "bandcamp"}
	}

	cleanTitle := title
	for _, kw := range []string{"(radio edit)", "(edit)", "(short mix)", "(single version)"} {
		cleanTitle = strings.ReplaceAll(cleanTitle, kw, "")
	}
	cleanTitle = strings.TrimSpace(cleanTitle)

	var mu sync.Mutex
	candidatesMap := make(map[string]Candidate)

	var wg sync.WaitGroup

	for _, src := range sources {
		source := strings.TrimSpace(src)
		if source == "" {
			continue
		}

		prefix := MapSourceSearchPrefix(source)

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

		for _, query := range queries {
			wg.Add(1)
			go func(srcName, searchPrefix, q string) {
				defer wg.Done()

				cmdCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
				defer cancel()

				cmd := exec.CommandContext(cmdCtx, "yt-dlp",
					"--flat-playlist",
					"--dump-json",
					"--no-warnings",
					"--quiet",
					"--js-runtimes", "node",
					fmt.Sprintf("%s:%s", searchPrefix, q),
				)

				var stdout bytes.Buffer
				cmd.Stdout = &stdout
				_ = cmd.Run() // ignore individual query errors

				scanner := bufio.NewScanner(&stdout)
				for scanner.Scan() {
					line := scanner.Text()
					if strings.TrimSpace(line) == "" {
						continue
					}

					var cand struct {
						ID         string  `json:"id"`
						Title      string  `json:"title"`
						Duration   float64 `json:"duration"`
						WebpageURL string  `json:"webpage_url"`
						URL        string  `json:"url"`
					}

					if err := json.Unmarshal([]byte(line), &cand); err == nil && cand.Title != "" && cand.Duration > 0 {
						targetURL := cand.WebpageURL
						if targetURL == "" {
							targetURL = cand.URL
						}
						if targetURL == "" && cand.ID != "" {
							if srcName == "youtube" {
								targetURL = fmt.Sprintf("https://www.youtube.com/watch?v=%s", cand.ID)
							} else {
								targetURL = cand.ID
							}
						}

						if targetURL != "" {
							key := fmt.Sprintf("%s:%s", srcName, targetURL)
							mu.Lock()
							if _, exists := candidatesMap[key]; !exists {
								candidatesMap[key] = Candidate{
									ID:         cand.ID,
									Title:      cand.Title,
									Duration:   cand.Duration,
									Source:     srcName,
									WebpageURL: targetURL,
								}
							}
							mu.Unlock()
						}
					}
				}
			}(source, prefix, query)
		}
	}

	wg.Wait()

	if len(candidatesMap) == 0 {
		return nil, ErrNoCandidateFound
	}

	candidateList := make([]Candidate, 0, len(candidatesMap))
	for _, c := range candidatesMap {
		candidateList = append(candidateList, c)
	}

	return candidateList, nil
}

// EvaluateAndInspectCandidatesInParallel samples audio in parallel across top candidates from all sources.
func EvaluateAndInspectCandidatesInParallel(ctx context.Context, candidates []Candidate, artist, title string) (*Candidate, error) {
	if len(candidates) == 0 {
		return nil, ErrNoCandidateFound
	}

	// Step 1: Preliminary ranking based on metadata and duration
	ranked := make([]Candidate, len(candidates))
	copy(ranked, candidates)
	_ = RankCandidates(ranked, artist, title)

	// Pick top candidate per source (up to top 5 total) for parallel audio sample inspection
	selectedMap := make(map[string]Candidate)
	for _, cand := range ranked {
		if _, exists := selectedMap[cand.Source]; !exists && len(selectedMap) < 5 {
			selectedMap[cand.Source] = cand
		}
	}

	topCandidates := make([]Candidate, 0, len(selectedMap))
	for _, cand := range selectedMap {
		topCandidates = append(topCandidates, cand)
	}

	// Step 2: Inspect audio samples in parallel across top candidates
	var wg sync.WaitGroup
	var mu sync.Mutex

	for i := range topCandidates {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			cand := &topCandidates[idx]

			sampleCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
			defer cancel()

			rep, err := verifier.VerifyAudioTrack(sampleCtx, cand.WebpageURL)
			if err == nil && rep != nil {
				qualityScore := 0
				if rep.Quality.BandwidthRating == "High Fidelity (>=18.5 kHz)" {
					qualityScore += 30
				} else if rep.Quality.HasLowBandwidthWarning {
					qualityScore -= 40
				}

				if rep.MixStructure.IsRadioEditWarning {
					qualityScore -= 50
				} else if rep.MixStructure.IsOriginalOrExtendedMix {
					qualityScore += 20
				}

				mu.Lock()
				cand.BandwidthHz = rep.Quality.EstimatedBandwidthHz
				cand.PeakDbFS = rep.Quality.PeakDbFS
				cand.RMSDbFS = rep.Quality.RMSDbFS
				cand.QualityScore = qualityScore
				mu.Unlock()
			}
		}(i)
	}

	wg.Wait()

	// Step 3: Final re-ranking with parallel audio inspection quality scores included
	best := RankCandidates(topCandidates, artist, title)
	if best == nil {
		return nil, ErrNoCandidateFound
	}

	return best, nil
}

// SearchAndSelectBestCandidate coordinates searching across sources in parallel and evaluating top candidates.
func SearchAndSelectBestCandidate(ctx context.Context, sources []string, artist, title, rawQuery string) (*Candidate, error) {
	candidates, err := SearchSourcesInParallel(ctx, sources, artist, title, rawQuery)
	if err != nil {
		return nil, err
	}

	return EvaluateAndInspectCandidatesInParallel(ctx, candidates, artist, title)
}

// SearchYouTubeCandidates maintains backward compatibility with single-source YouTube calls.
func SearchYouTubeCandidates(ctx context.Context, artist, title, rawQuery string) (string, error) {
	best, err := SearchAndSelectBestCandidate(ctx, []string{"youtube"}, artist, title, rawQuery)
	if err != nil {
		return "", err
	}
	return best.WebpageURL, nil
}

// DownloadAudioStream downloads the audio stream from targetURL into outDir.
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
		"", // no extractor args fallback for non-YouTube sources (SoundCloud, Bandcamp, etc.)
	}

	outPattern := filepath.Join(outDir, "%(title)s.%(ext)s")

	for _, extractorArgs := range extractorArgsList {
		cmdCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)

		args := []string{
			"--no-warnings",
			"--quiet",
			"--js-runtimes", "node",
			"-f", "140/bestaudio[ext=m4a]/bestaudio/best",
			"-x",
			"--embed-metadata",
			"--embed-thumbnail",
			"-o", outPattern,
		}

		if extractorArgs != "" {
			args = append(args, "--extractor-args", extractorArgs)
		}
		args = append(args, targetURL)

		cmd := exec.CommandContext(cmdCtx, "yt-dlp", args...)
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
		if ext == ".m4a" || ext == ".opus" || ext == ".mp3" || ext == ".flac" {
			files[entry.Name()] = true
		}
	}
	return files, nil
}
