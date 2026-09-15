package pipeline

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/alexgorbatchev/godeps"
	"github.com/dj/fetch-track-cli/internal/cache"
	"github.com/dj/fetch-track-cli/internal/deps"
	"github.com/dj/fetch-track-cli/internal/downloader"
	"github.com/dj/fetch-track-cli/internal/progress"
	"github.com/dj/fetch-track-cli/internal/spinner"
	"github.com/dj/fetch-track-cli/internal/ui"
	"github.com/dj/fetch-track-cli/internal/verifier"
)

// TrackMetadataResult describes metadata received from tag-track.
type TrackMetadataResult struct {
	Title       string `json:"title"`
	Artist      string `json:"artist"`
	Album       string `json:"album"`
	Genre       string `json:"genre,omitempty"`
	ReleaseDate string `json:"releaseDate,omitempty"`
	ReleaseYear string `json:"releaseYear,omitempty"`
	CoverArtURL string `json:"coverArtUrl,omitempty"`
	Source      string `json:"source,omitempty"`
	Matched     string `json:"matched,omitempty"`
}

// Options configures the track acquisition pipeline execution.
type Options struct {
	OutDir             string
	Sources            []string
	SkipVerify         bool
	SkipMetadata       bool
	SkipDepCheck       bool
	Interactive        bool
	NoCache            bool
	Verbose            bool
	Debug              bool
	BootTime           time.Time
	IsAgent            bool
	AutoInstall        bool
	ProgressTarget     string
	ProgressReporter   *progress.Reporter
	Runner             deps.CommandRunner
	JSRuntime          string
	ConfirmFingerprint bool
	MinBandwidthHz     int
	JSONOutput         bool
}

type logger struct {
	debug    bool
	bootTime time.Time
	sp       *spinner.Spinner
}

func newLogger(debug bool, bootTime time.Time, sp *spinner.Spinner) *logger {
	if bootTime.IsZero() {
		bootTime = time.Now()
	}
	return &logger{
		debug:    debug,
		bootTime: bootTime,
		sp:       sp,
	}
}

func (l *logger) formatLine(line string) string {
	if l.debug {
		elapsed := time.Since(l.bootTime).Milliseconds()
		if line == "" {
			return ""
		}
		return fmt.Sprintf("[%dms] %s", elapsed, line)
	}
	return line
}

func (l *logger) Println(a ...any) {
	msg := fmt.Sprint(a...)
	lines := strings.Split(msg, "\n")
	for i, line := range lines {
		formatted := l.formatLine(line)
		if l.sp != nil {
			l.sp.PrintAbove(formatted)
		} else {
			if i == 0 && line == "" {
				fmt.Println()
			} else {
				fmt.Println(formatted)
			}
		}
	}
}

func (l *logger) Printf(formatStr string, a ...any) {
	msg := fmt.Sprintf(formatStr, a...)
	lines := strings.Split(msg, "\n")
	for i, line := range lines {
		if i == len(lines)-1 && line == "" && strings.HasSuffix(formatStr, "\n") {
			continue
		}
		formatted := l.formatLine(line)
		if l.sp != nil {
			l.sp.PrintAbove(formatted)
		} else {
			if i == 0 && line == "" {
				fmt.Println()
			} else {
				fmt.Println(formatted)
			}
		}
	}
}

// IsAgentMode checks if the environment variable AGENT=1 or AGENT=true is set.
func IsAgentMode() bool {
	return deps.IsAgentMode()
}

// Run executes the full single-track acquisition pipeline.
func Run(ctx context.Context, urlOrQuery string, opts Options) error {
	if opts.OutDir == "" {
		opts.OutDir = "."
	}
	if len(opts.Sources) == 0 {
		opts.Sources = []string{"youtube", "soundcloud"}
	}
	if IsAgentMode() {
		opts.IsAgent = true
	}

	_ = deps.InitManagedPath()
	cacheInst, _ := cache.New(!opts.NoCache)

	if !opts.SkipDepCheck {
		reports, err := deps.VerifyDependencies(ctx, cacheInst)
		if err != nil {
			if opts.IsAgent {
				for _, r := range reports {
					if !r.Satisfied {
						if !r.Installed {
							fmt.Printf("%s: missing\n", r.Name)
						} else {
							fmt.Printf("%s: fail (%s)\n", r.Name, r.Error)
						}
					}
				}
				fmt.Printf("target: %s\nstatus: error\nerror: %v\n", urlOrQuery, err)
			}
			return fmt.Errorf("dependency verification failed: %w", err)
		}
	}

	isURL := verifier.IsURL(urlOrQuery)
	targetURL := urlOrQuery

	artist := ""
	title := urlOrQuery
	rawSearchQuery := urlOrQuery

	var sp *spinner.Spinner
	if !opts.IsAgent && !opts.Verbose {
		sp = spinner.New("working...")
	}
	log := newLogger(opts.Debug, opts.BootTime, sp)

	runner := opts.Runner
	if runner == nil {
		runner = deps.GetRunner()
	}

	var initialCandidates []downloader.Candidate

	if isURL {
		if !opts.IsAgent {
			log.Println("Inspecting provided URL metadata & extracting track search terms...")
			if sp != nil {
				sp.Update("working... inspecting URL metadata")
				sp.Start()
			}
		}
		meta, err := verifier.FetchURLMetadata(ctx, urlOrQuery, cacheInst)
		if sp != nil {
			sp.Stop()
		}
		if err == nil && meta != nil && meta.Title != "" {
			title = meta.Title
			artist = meta.Uploader
			rawSearchQuery = meta.Title

			if strings.Contains(meta.Title, " - ") {
				parts := strings.SplitN(meta.Title, " - ", 2)
				artist = strings.TrimSpace(parts[0])
				title = strings.TrimSpace(parts[1])
			}

			if !opts.IsAgent {
				cachedTag := ""
				if meta.Cached {
					cachedTag = " [cached]"
				}
				log.Printf("  - URL Title: \"%s\" (uploader: %s)%s\n", meta.Title, meta.Uploader, cachedTag)
			}

			// Include direct URL as candidate in pool
			initialCandidates = append(initialCandidates, downloader.Candidate{
				ID:         "direct_url",
				Title:      meta.Title,
				Duration:   meta.DurationSeconds,
				Source:     "direct_url",
				WebpageURL: urlOrQuery,
			})
		} else if !opts.IsAgent {
			log.Printf("  - Warning: could not probe direct URL metadata: %v. Proceeding with URL.\n", err)
		}
	} else {
		if strings.Contains(urlOrQuery, " - ") {
			parts := strings.SplitN(urlOrQuery, " - ", 2)
			artist = strings.TrimSpace(parts[0])
			title = strings.TrimSpace(parts[1])
		}
	}

	if !opts.IsAgent {
		if sp != nil {
			sp.Update(fmt.Sprintf("working... searching %s", strings.Join(opts.Sources, ", ")))
			sp.Start()
		}
	}

	_ = opts.ProgressReporter.Emit(progress.Event{
		Type:       progress.EventPhaseStart,
		Phase:      "search",
		Step:       2,
		TotalSteps: 5,
		Message:    fmt.Sprintf("searching sources: %s", strings.Join(opts.Sources, ", ")),
	})

	var headerOnce sync.Once
	var obsMu sync.Mutex
	var searchObserver *downloader.SearchObserver

	if !opts.IsAgent && sp != nil {
		searchObserver = &downloader.SearchObserver{
			OnQueryStart: func(src string) {
				obsMu.Lock()
				defer obsMu.Unlock()
				log.Printf("Searching %s...", src)
			},
			OnQueryComplete: func(src string, count int) {
				obsMu.Lock()
				defer obsMu.Unlock()
				log.Printf("Got %d results from %s", count, src)
			},
			OnCandidate: func(cand downloader.Candidate) {
				obsMu.Lock()
				defer obsMu.Unlock()
				headerOnce.Do(func() {
					log.Println("Candidates:")
				})
				log.Printf("  - %q [%s %s]", cand.Title, cand.Source, verifier.FormatDuration(cand.Duration))
			},
		}
	}

	foundCandidates, searchErr := downloader.SearchSourcesInParallelWithObserver(ctx, downloader.GetDefaultRunner(), opts.Sources, artist, title, rawSearchQuery, cacheInst, searchObserver, opts.Verbose)

	var candidatePool []downloader.Candidate
	candidatePool = append(candidatePool, initialCandidates...)
	if searchErr == nil {
		candidatePool = append(candidatePool, foundCandidates...)
	}
	candidatePool = downloader.DeduplicateCandidates(candidatePool)

	for _, cand := range candidatePool {
		_ = opts.ProgressReporter.Emit(progress.Event{
			Type:  progress.EventCandidateFound,
			Phase: "search",
			Candidate: &progress.CandidateInfo{
				ID:         cand.ID,
				Title:      cand.Title,
				Source:     cand.Source,
				Duration:   cand.Duration,
				Score:      cand.Score,
				WebpageURL: cand.WebpageURL,
			},
		})
	}

	var selectedCandidate *downloader.Candidate

	if len(candidatePool) > 0 {
		candidatePool = downloader.RankAllCandidates(candidatePool, artist, title)

		bestCandidate, evalErr := downloader.EvaluateAndInspectCandidatesInParallel(ctx, candidatePool, artist, title, opts.Verbose)
		if evalErr == nil && bestCandidate != nil {
			selectedCandidate = bestCandidate
			targetURL = bestCandidate.WebpageURL
			if sp != nil {
				sp.Stop()
			}
			if !opts.IsAgent && !opts.Interactive {
				if bestCandidate.Source == "direct_url" {
					log.Printf("Selected: %q [direct_url]\n", bestCandidate.Title)
				} else {
					log.Printf("Selected: %q [%s %s] score=%d\n", bestCandidate.Title, bestCandidate.Source, verifier.FormatDuration(bestCandidate.Duration), bestCandidate.Score)
				}
			}
			_ = opts.ProgressReporter.Emit(progress.Event{
				Type:  progress.EventCandidateSelected,
				Phase: "search",
				Candidate: &progress.CandidateInfo{
					ID:         bestCandidate.ID,
					Title:      bestCandidate.Title,
					Source:     bestCandidate.Source,
					Duration:   bestCandidate.Duration,
					Score:      bestCandidate.Score,
					WebpageURL: bestCandidate.WebpageURL,
				},
			})
		}

		if opts.Interactive && !opts.IsAgent {
			approved, err := ui.PromptCandidateSelection(candidatePool, selectedCandidate)
			if err != nil {
				return err
			}
			if approved != nil {
				selectedCandidate = approved
				targetURL = approved.WebpageURL
			}
		}
	}

	if !isURL {
		if len(candidatePool) == 0 || selectedCandidate == nil {
			if sp != nil {
				sp.Stop()
			}
			err := fmt.Errorf("no matching track candidates found for query: %q", urlOrQuery)
			_ = opts.ProgressReporter.Emit(progress.Event{
				Type:  progress.EventError,
				Phase: "search",
				Error: err.Error(),
			})
			if opts.IsAgent {
				fmt.Printf("target: %s\nstatus: error\nerror: no matching track candidates found\n", urlOrQuery)
			}
			return err
		}
	}

	if sp != nil {
		sp.Stop()
	}

	// Step 2: Download audio stream
	if !opts.IsAgent {
		log.Println("\nDownloading audio stream & artwork")
		if sp != nil {
			sp.Update("working... downloading audio stream")
			sp.Start()
		}
	}

	_ = opts.ProgressReporter.Emit(progress.Event{
		Type:       progress.EventPhaseStart,
		Phase:      "download",
		Step:       3,
		TotalSteps: 5,
		Message:    "downloading audio stream",
	})

	var candidatesToTry []downloader.Candidate
	if selectedCandidate != nil {
		candidatesToTry = append(candidatesToTry, *selectedCandidate)
		for _, c := range candidatePool {
			if c.WebpageURL != selectedCandidate.WebpageURL && (c.ID == "" || c.ID != selectedCandidate.ID) {
				candidatesToTry = append(candidatesToTry, c)
			}
		}
	} else {
		candidatesToTry = append(candidatesToTry, downloader.Candidate{
			WebpageURL: targetURL,
			Title:      title,
		})
	}

	var downloadedPath string
	var lastErr error

	for tryIdx, cand := range candidatesToTry {
		currentURL := cand.WebpageURL
		if currentURL == "" {
			currentURL = targetURL
		}

		if tryIdx > 0 && !opts.IsAgent {
			log.Printf("Trying candidate #%d: %q [%s] (%s)\n", tryIdx+1, cand.Title, cand.Source, currentURL)
		}

		dlPath, err := downloader.DownloadAudioStreamWithRunner(ctx, downloader.GetDefaultRunner(), currentURL, opts.OutDir, opts.Verbose)
		if err != nil {
			lastErr = err
			if tryIdx < len(candidatesToTry)-1 {
				continue
			}
			if sp != nil {
				sp.Stop()
			}
			_ = opts.ProgressReporter.Emit(progress.Event{
				Type:  progress.EventError,
				Phase: "download",
				Error: err.Error(),
			})
			if opts.IsAgent {
				fmt.Printf("target: %s\nstatus: error\nerror: %v\n", urlOrQuery, err)
			}
			return fmt.Errorf("downloading audio stream: %w", err)
		}

		// Optional Acoustic Fingerprint Confirmation
		if opts.ConfirmFingerprint && !isURL {
			fpResult, _ := verifier.VerifyAcousticFingerprint(ctx, verifier.CommandRunner(runner), dlPath, artist, title)
			if fpResult != nil && fpResult.Mismatch {
				if !opts.IsAgent {
					log.Printf("  - Notice: %s\n", fpResult.Reason)
				}
				_ = os.Remove(dlPath)
				if tryIdx < len(candidatesToTry)-1 {
					continue
				}
				if sp != nil {
					sp.Stop()
				}
				return fmt.Errorf("acoustic fingerprint confirmation failed across candidates: %s", fpResult.Reason)
			}
			if fpResult != nil && fpResult.Confirmed && !opts.IsAgent {
				log.Printf("  - Acoustic Fingerprint: Confirmed (%s)\n", fpResult.Source)
			}
		}

		downloadedPath = dlPath
		selectedCandidate = &cand
		break
	}

	if downloadedPath == "" {
		if lastErr != nil {
			return fmt.Errorf("downloading audio stream: %w", lastErr)
		}
		return fmt.Errorf("no candidate audio stream could be downloaded")
	}

	if sp != nil {
		sp.Stop()
	}

	downloadedFilename := filepath.Base(downloadedPath)
	if !opts.IsAgent {
		log.Printf("  - Saved: %s\n", downloadedFilename)
	}

	// Step 3: Full Verification
	var report *verifier.VerificationReport
	if !opts.SkipVerify {
		if !opts.IsAgent {
			log.Println("\nRunning audio quality & spectrum inspection")
			if sp != nil {
				sp.Update("working... inspecting audio spectrum & dynamics")
				sp.Start()
			}
		}

		_ = opts.ProgressReporter.Emit(progress.Event{
			Type:       progress.EventPhaseStart,
			Phase:      "verify",
			Step:       4,
			TotalSteps: 5,
			Message:    "running audio quality & spectrum inspection",
		})

		rep, err := verifier.VerifyAudioTrack(ctx, downloadedPath, opts.Verbose, opts.MinBandwidthHz)
		if sp != nil {
			sp.Stop()
		}
		if err != nil {
			if !opts.IsAgent {
				log.Printf("  - Notice: %v\n", err)
			}
		} else {
			report = rep
			if !opts.IsAgent {
				log.Printf("  - Duration: %s (%s)\n", report.MixStructure.DurationFormatted, report.MixStructure.MixTypeDescription)
				log.Printf("  - Bandwidth: %s (%d kHz)\n", report.Quality.BandwidthRating, report.Quality.EstimatedBandwidthHz/1000)
				log.Printf("  - Peak / RMS: %.2f dBFS / %.2f dBFS\n", report.Quality.PeakDbFS, report.Quality.RMSDbFS)
				gainSign := ""
				if report.Quality.SuggestedDJGainDb > 0 {
					gainSign = "+"
				}
				log.Printf("  - Gain Offset: %s%.1f dB\n", gainSign, report.Quality.SuggestedDJGainDb)
				log.Printf("  - Status: %s\n", strings.TrimPrefix(report.SummaryStatus, "STATUS: "))
			}
		}
	} else if !opts.IsAgent {
		log.Println("\nStep 3: Skipped DJ Audio Quality & Spectrum Inspection (-skipVerify)")
	}

	// Step 4: Metadata & High-Res Cover Art Enrichment via tag-track
	finalPath := downloadedPath
	var metaResult *TrackMetadataResult

	if !opts.SkipMetadata {
		if !opts.IsAgent {
			log.Println("\nEnriching metadata & cover art via tag-track")
			if sp != nil {
				sp.Update("working... identifying track metadata & artwork")
				sp.Start()
			}
		}

		_ = opts.ProgressReporter.Emit(progress.Event{
			Type:       progress.EventPhaseStart,
			Phase:      "metadata",
			Step:       5,
			TotalSteps: 5,
			Message:    "enriching metadata & cover art via tag-track",
		})

		runner := opts.Runner
		if runner == nil {
			runner = deps.GetRunner()
		}

		tagArgs := []string{"track", "update", downloadedPath, "-o", opts.OutDir, "--in-place"}
		if targetURL != "" {
			tagArgs = append(tagArgs, "--source-url", targetURL)
		}
		if opts.ProgressTarget != "" {
			tagArgs = append(tagArgs, "--progress-target", opts.ProgressTarget)
		}
		if opts.NoCache {
			tagArgs = append(tagArgs, "--no-cache")
		}
		if opts.Verbose {
			tagArgs = append(tagArgs, "-v")
		}

		tagOutBytes, tagErr := runner(ctx, "tag-track", tagArgs...)
		if sp != nil {
			sp.Stop()
		}

		if tagErr != nil {
			if !opts.IsAgent {
				cleanErr := godeps.SanitizeStderr(tagErr.Error())
				if cleanErr == "" {
					cleanErr = tagErr.Error()
				}
				log.Printf("  - Notice: tagging via tag-track failed: %s\n", cleanErr)
			}
		} else {
			tagOut := string(tagOutBytes)
			for _, rawLine := range strings.Split(tagOut, "\n") {
				line := strings.TrimSpace(rawLine)
				if strings.HasPrefix(line, "DONE: ") {
					finalPath = strings.TrimPrefix(line, "DONE: ")
				} else if strings.HasPrefix(line, "output: ") {
					finalPath = strings.TrimPrefix(line, "output: ")
				} else if strings.HasPrefix(line, "title: ") {
					if metaResult == nil {
						metaResult = &TrackMetadataResult{}
					}
					metaResult.Title = strings.TrimPrefix(line, "title: ")
				} else if strings.HasPrefix(line, "artist: ") {
					if metaResult == nil {
						metaResult = &TrackMetadataResult{}
					}
					metaResult.Artist = strings.TrimPrefix(line, "artist: ")
				} else if strings.HasPrefix(line, "album: ") {
					if metaResult == nil {
						metaResult = &TrackMetadataResult{}
					}
					metaResult.Album = strings.TrimPrefix(line, "album: ")
				} else if strings.HasPrefix(line, "year: ") {
					if metaResult == nil {
						metaResult = &TrackMetadataResult{}
					}
					metaResult.ReleaseYear = strings.TrimPrefix(line, "year: ")
				} else if strings.HasPrefix(line, "source: ") {
					if metaResult == nil {
						metaResult = &TrackMetadataResult{}
					}
					metaResult.Source = strings.TrimPrefix(line, "source: ")
				} else if strings.HasPrefix(line, "- matched: ") || strings.HasPrefix(line, "matched: ") || strings.HasPrefix(line, "- Matched: ") || strings.HasPrefix(line, "Matched: ") {
					if metaResult == nil {
						metaResult = &TrackMetadataResult{}
					}
					cleanMatched := strings.TrimPrefix(line, "- matched: ")
					cleanMatched = strings.TrimPrefix(cleanMatched, "matched: ")
					cleanMatched = strings.TrimPrefix(cleanMatched, "- Matched: ")
					cleanMatched = strings.TrimPrefix(cleanMatched, "Matched: ")
					metaResult.Matched = cleanMatched
				} else if strings.HasPrefix(line, "- source: ") || strings.HasPrefix(line, "- Source: ") {
					if metaResult == nil {
						metaResult = &TrackMetadataResult{}
					}
					cleanSource := strings.TrimPrefix(line, "- source: ")
					cleanSource = strings.TrimPrefix(cleanSource, "- Source: ")
					metaResult.Source = cleanSource
				}
			}
			if metaResult != nil && metaResult.Title != "" {
				metaResult.Title = PreserveVersionInTitle(urlOrQuery, metaResult.Title)
			}
			safeFinalPath := ResolveCollisionSafePath(finalPath, downloadedPath)
			if safeFinalPath != finalPath {
				_ = os.Rename(finalPath, safeFinalPath)
				finalPath = safeFinalPath
			} else if finalPath != downloadedPath {
				_ = os.Remove(downloadedPath)
			}
			if !opts.IsAgent && metaResult != nil {
				if metaResult.Matched != "" {
					log.Printf("  - Matched: %s\n", metaResult.Matched)
				} else if metaResult.Title != "" {
					log.Printf("  - Matched: \"%s - %s\" (%s, %s)\n", metaResult.Artist, metaResult.Title, metaResult.Album, metaResult.ReleaseYear)
				}
				if metaResult.Source != "" {
					log.Printf("  - Source: %s\n", metaResult.Source)
				}
			}
		}
	} else if !opts.IsAgent {
		log.Println("\nStep 4: Skipped Metadata & Cover Art Enrichment (-skipMetadata)")
	}

	finalFilename := filepath.Base(finalPath)
	outDisplayPath := filepath.Join(opts.OutDir, finalFilename)

	var resultInfo *progress.ResultInfo
	if metaResult != nil || report != nil {
		rInfo := &progress.ResultInfo{
			Path: outDisplayPath,
		}
		if metaResult != nil {
			rInfo.Artist = metaResult.Artist
			rInfo.Title = metaResult.Title
			rInfo.Album = metaResult.Album
			rInfo.ReleaseYear = metaResult.ReleaseYear
		}
		if report != nil {
			rInfo.Duration = report.Metadata.DurationSeconds
			rInfo.BandwidthHz = report.Quality.EstimatedBandwidthHz
			rInfo.BandwidthRating = report.Quality.BandwidthRating
			rInfo.SuggestedGainDb = report.Quality.SuggestedDJGainDb
			rInfo.Status = report.SummaryStatus
		}
		resultInfo = rInfo
	}

	_ = opts.ProgressReporter.Emit(progress.Event{
		Type:       progress.EventComplete,
		Phase:      "complete",
		Step:       5,
		TotalSteps: 5,
		Message:    "track acquisition complete",
		Result:     resultInfo,
	})

	if opts.JSONOutput {
		jsonRes := &JSONExecutionResult{
			Target:            urlOrQuery,
			Status:            "success",
			CandidateCount:    len(candidatePool),
			SelectedCandidate: selectedCandidate,
			OutputFile:        outDisplayPath,
			Verification:      ConvertVerificationReport(report),
		}
		if metaResult != nil {
			jsonRes.Metadata = &JSONMetadata{
				Title:       metaResult.Title,
				Artist:      metaResult.Artist,
				Album:       metaResult.Album,
				Year:        metaResult.ReleaseYear,
				Source:      metaResult.Source,
				CoverArtURL: metaResult.CoverArtURL,
			}
		}
		return RenderJSONOutput(jsonRes)
	}

	if opts.IsAgent {
		fmt.Printf("target: %s\n", urlOrQuery)
		if selectedCandidate != nil {
			fmt.Printf("candidate: %s [%s] (%s)\n", selectedCandidate.Title, selectedCandidate.Source, selectedCandidate.WebpageURL)
		}
		if report != nil {
			fmt.Printf("duration: %s (%s)\n", report.MixStructure.DurationFormatted, report.MixStructure.MixTypeDescription)
			fmt.Printf("bandwidth: %d kHz (%s)\n", report.Quality.EstimatedBandwidthHz/1000, report.Quality.BandwidthRating)
			gainSign := ""
			if report.Quality.SuggestedDJGainDb > 0 {
				gainSign = "+"
			}
			fmt.Printf("dynamics: peak=%.2f dBFS rms=%.2f dBFS gain=%s%.1f dB\n", report.Quality.PeakDbFS, report.Quality.RMSDbFS, gainSign, report.Quality.SuggestedDJGainDb)
			fmt.Printf("status: %s\n", report.SummaryStatus)
		}
		if metaResult != nil {
			if metaResult.Matched != "" {
				fmt.Printf("metadata: %s [%s]\n", metaResult.Matched, metaResult.Source)
			} else if metaResult.Title != "" {
				fmt.Printf("metadata: \"%s - %s\" (%s, %s) [%s]\n", metaResult.Artist, metaResult.Title, metaResult.Album, metaResult.ReleaseYear, metaResult.Source)
			}
		}
		fmt.Printf("output: %s\n", outDisplayPath)
		return nil
	}

	log.Printf("\nDONE: %s\n", outDisplayPath)

	return nil
}
