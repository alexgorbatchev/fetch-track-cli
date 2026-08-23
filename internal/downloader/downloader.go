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

	"github.com/dj/fetch-track-cli/internal/cache"
	"github.com/dj/fetch-track-cli/internal/verifier"
	"github.com/lithammer/fuzzysearch/fuzzy"
)

var (
	// ErrNoCandidateFound indicates no suitable candidate was found across sources.
	ErrNoCandidateFound = errors.New("no suitable candidate found across sources")
	// ErrDownloadFailed indicates yt-dlp failed to download the audio stream.
	ErrDownloadFailed = errors.New("failed to download audio stream across client fallbacks")
)

// CommandRunner abstracts execution of external commands for testability.
type CommandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

func defaultRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		if stderr.Len() > 0 {
			return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
		}
		return nil, err
	}
	return stdout.Bytes(), nil
}

var defaultRunnerVar = defaultRunner

// SetDefaultRunner overrides the default command runner and returns a restore cleanup function.
func SetDefaultRunner(runner CommandRunner) func() {
	orig := defaultRunnerVar
	defaultRunnerVar = runner
	return func() {
		defaultRunnerVar = orig
	}
}

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
func SearchSourcesInParallel(ctx context.Context, sources []string, artist, title, rawQuery string, c *cache.Cache, verbose ...bool) ([]Candidate, error) {
	return SearchSourcesInParallelWithRunner(ctx, defaultRunnerVar, sources, artist, title, rawQuery, c, verbose...)
}

// SearchSourcesInParallelWithRunner searches all configured sources concurrently using a provided CommandRunner.
func SearchSourcesInParallelWithRunner(ctx context.Context, runner CommandRunner, sources []string, artist, title, rawQuery string, c *cache.Cache, verbose ...bool) ([]Candidate, error) {
	isVerbose := len(verbose) > 0 && verbose[0]
	if isVerbose {
		fmt.Printf("search: %s\n", strings.Join(sources, ", "))
	}

	if runner == nil {
		runner = defaultRunner
	}

	if len(sources) == 0 {
		sources = []string{"youtube", "soundcloud"}
	}

	cacheKey := fmt.Sprintf("%s:%s:%s", strings.Join(sources, ","), artist, rawQuery)
	var cachedList []Candidate
	if c != nil && c.Get("searches", cacheKey, &cachedList) && len(cachedList) > 0 {
		if isVerbose {
			fmt.Printf("search cache hit for %q\n", rawQuery)
		} else if !isAgentMode() {
			fmt.Printf("candidates: [cached]\n")
			for _, cand := range cachedList {
				fmt.Printf("  - %q [%s %s]\n", cand.Title, cand.Source, verifier.FormatDuration(cand.Duration))
			}
		}
		return cachedList, nil
	}

	cleanTitle := title
	for _, kw := range []string{"(radio edit)", "(edit)", "(short mix)", "(single version)"} {
		cleanTitle = strings.ReplaceAll(cleanTitle, kw, "")
	}
	cleanTitle = strings.TrimSpace(cleanTitle)

	var mu sync.Mutex
	var headerOnce sync.Once
	candidatesMap := make(map[string]Candidate)

	var wg sync.WaitGroup

	for _, src := range sources {
		source := strings.TrimSpace(src)
		if source == "" {
			continue
		}

		prefix := MapSourceSearchPrefix(source)

		q := rawQuery
		if artist != "" && cleanTitle != "" {
			q = fmt.Sprintf("%s %s", artist, cleanTitle)
		}

		wg.Add(1)
		go func(srcName, searchPrefix, queryStr string) {
			defer wg.Done()

			if isVerbose {
				fmt.Printf("%s: %q\n", srcName, queryStr)
			}

			cmdCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
			defer cancel()

			stdoutBytes, err := runner(cmdCtx, "yt-dlp",
				"--flat-playlist",
				"--dump-json",
				"--no-warnings",
				"--quiet",
				"--js-runtimes", "node",
				fmt.Sprintf("%s:%s", searchPrefix, queryStr),
			)
			if err != nil || len(stdoutBytes) == 0 {
				return
			}

			scanner := bufio.NewScanner(bytes.NewReader(stdoutBytes))
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
					// Filter out irrelevant search hits, short snippets (< 60s), and continuous album mixes (> 900s)
					candNorm := NormalizeUnicode(cand.Title)
					titleNorm := NormalizeUnicode(cleanTitle)

					titleMatch := titleNorm == "" || strings.Contains(candNorm, titleNorm) || fuzzy.MatchFold(titleNorm, candNorm)

					if !titleMatch || cand.Duration < 60 || cand.Duration > 900 {
						continue // Discard garbage search candidate
					}

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
							candObj := Candidate{
								ID:         cand.ID,
								Title:      cand.Title,
								Duration:   cand.Duration,
								Source:     srcName,
								WebpageURL: targetURL,
							}
							candidatesMap[key] = candObj
							if !isAgentMode() {
								headerOnce.Do(func() {
									fmt.Printf("\r\033[Kcandidates:\n")
								})
								fmt.Printf("\r\033[K  - %q [%s %s]\n", cand.Title, srcName, verifier.FormatDuration(cand.Duration))
							}
						}
						mu.Unlock()
					}
				}
			}
		}(source, prefix, q)
	}

	wg.Wait()

	if len(candidatesMap) == 0 {
		return nil, ErrNoCandidateFound
	}

	candidateList := make([]Candidate, 0, len(candidatesMap))
	for _, c := range candidatesMap {
		candidateList = append(candidateList, c)
	}

	if c != nil {
		_ = c.Put("searches", cacheKey, candidateList, 12*time.Hour)
	}

	return candidateList, nil
}

// EvaluateAndInspectCandidatesInParallel ranks candidates based on duration, title, keywords, and metadata.
func EvaluateAndInspectCandidatesInParallel(ctx context.Context, candidates []Candidate, artist, title string, verbose ...bool) (*Candidate, error) {
	if len(candidates) == 0 {
		return nil, ErrNoCandidateFound
	}

	best := RankCandidates(candidates, artist, title)
	if best == nil {
		return nil, ErrNoCandidateFound
	}

	return best, nil
}

// SearchAndSelectBestCandidateWithRunner coordinates searching across sources in parallel and evaluating top candidates using a provided runner.
func SearchAndSelectBestCandidateWithRunner(ctx context.Context, runner CommandRunner, sources []string, artist, title, rawQuery string) (*Candidate, error) {
	candidates, err := SearchSourcesInParallelWithRunner(ctx, runner, sources, artist, title, rawQuery, nil)
	if err != nil {
		return nil, err
	}

	return EvaluateAndInspectCandidatesInParallel(ctx, candidates, artist, title)
}

// SearchAndSelectBestCandidate coordinates searching across sources in parallel and evaluating top candidates.
func SearchAndSelectBestCandidate(ctx context.Context, sources []string, artist, title, rawQuery string) (*Candidate, error) {
	return SearchAndSelectBestCandidateWithRunner(ctx, defaultRunnerVar, sources, artist, title, rawQuery)
}

// SearchYouTubeCandidatesWithRunner maintains backward compatibility with single-source YouTube calls using custom runner.
func SearchYouTubeCandidatesWithRunner(ctx context.Context, runner CommandRunner, artist, title, rawQuery string) (string, error) {
	best, err := SearchAndSelectBestCandidateWithRunner(ctx, runner, []string{"youtube"}, artist, title, rawQuery)
	if err != nil {
		return "", err
	}
	return best.WebpageURL, nil
}

// SearchYouTubeCandidates maintains backward compatibility with single-source YouTube calls.
func SearchYouTubeCandidates(ctx context.Context, artist, title, rawQuery string) (string, error) {
	return SearchYouTubeCandidatesWithRunner(ctx, defaultRunnerVar, artist, title, rawQuery)
}

// DownloadAudioStream downloads the audio stream from targetURL into outDir.
func DownloadAudioStream(ctx context.Context, targetURL, outDir string, verbose ...bool) (string, error) {
	return DownloadAudioStreamWithRunner(ctx, defaultRunnerVar, targetURL, outDir, verbose...)
}

// DownloadAudioStreamWithRunner downloads audio stream using a provided CommandRunner.
func DownloadAudioStreamWithRunner(ctx context.Context, runner CommandRunner, targetURL, outDir string, verbose ...bool) (string, error) {
	isVerbose := len(verbose) > 0 && verbose[0]

	if err := os.MkdirAll(outDir, 0755); err != nil {
		return "", fmt.Errorf("creating output directory: %w", err)
	}

	if isVerbose {
		fmt.Printf("downloading: %s\n", targetURL)
	}

	if runner == nil {
		runner = defaultRunner
	}

	initialFiles, err := listAudioFiles(outDir)
	if err != nil {
		return "", fmt.Errorf("listing initial files in %s: %w", outDir, err)
	}

	extractorArgsList := []string{
		"youtube:player_client=mweb,web",
		"",
	}

	outPattern := filepath.Join(outDir, "%(title)s.%(ext)s")

	for idx, extractorArgs := range extractorArgsList {
		cmdCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)

		if isVerbose {
			if extractorArgs != "" {
				fmt.Printf("download try %d/3: args=%q\n", idx+1, extractorArgs)
			} else {
				fmt.Printf("download try %d/3: standard fallback\n", idx+1)
			}
		}

		args := []string{
			"--no-warnings",
			"--quiet",
			"--js-runtimes", "node",
			"-f", "bestaudio/best",
			"-x",
			"--embed-metadata",
			"--embed-thumbnail",
			"-o", outPattern,
		}

		if extractorArgs != "" {
			args = append(args, "--extractor-args", extractorArgs)
		}
		args = append(args, targetURL)

		_, err := runner(cmdCtx, "yt-dlp", args...)
		cancel()

		if err == nil {
			currentFiles, err := listAudioFiles(outDir)
			if err == nil {
				for file := range currentFiles {
					if !initialFiles[file] {
						savedPath := filepath.Join(outDir, file)
						if isVerbose {
							fmt.Printf("downloaded: %s\n", savedPath)
						}
						return savedPath, nil
					}
				}
			}
		} else if isVerbose {
			fmt.Printf("download retry: attempt %d failed (%v)\n", idx+1, err)
		}
	}

	return "", ErrDownloadFailed
}

func isAgentMode() bool {
	val := strings.TrimSpace(os.Getenv("AGENT"))
	return val == "1" || strings.ToLower(val) == "true"
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
