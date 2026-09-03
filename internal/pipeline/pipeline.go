package pipeline

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
}

// Options configures the track acquisition pipeline execution.
type Options struct {
	OutDir           string
	Sources          []string
	SkipVerify       bool
	SkipMetadata     bool
	SkipDepCheck     bool
	Interactive      bool
	NoCache          bool
	Verbose          bool
	IsAgent          bool
	AutoInstall      bool
	ProgressTarget   string
	ProgressReporter *progress.Reporter
	Runner           deps.CommandRunner
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

	isURL := verifier.IsURL(urlOrQuery)
	targetURL := urlOrQuery

	artist := ""
	title := urlOrQuery
	rawSearchQuery := urlOrQuery

	var sp *spinner.Spinner
	if !opts.IsAgent && !opts.Verbose {
		sp = spinner.New("working...")
	}

	var initialCandidates []downloader.Candidate

	if isURL {
		if !opts.IsAgent {
			fmt.Println("Inspecting provided URL metadata & extracting track search terms...")
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
				fmt.Printf("  - url title: \"%s\" (uploader: %s)%s\n", meta.Title, meta.Uploader, cachedTag)
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
			fmt.Printf("  - warning: could not probe direct URL metadata: %v. Proceeding with URL.\n", err)
		}
	} else {
		if strings.Contains(urlOrQuery, " - ") {
			parts := strings.SplitN(urlOrQuery, " - ", 2)
			artist = strings.TrimSpace(parts[0])
			title = strings.TrimSpace(parts[1])
		}
	}

	if !opts.IsAgent {
		if isURL {
			fmt.Printf("\nSearching: %s\n", strings.Join(opts.Sources, ", "))
		} else {
			fmt.Printf("Searching: %s\n", strings.Join(opts.Sources, ", "))
		}
		if sp != nil {
			sp.Update("working...")
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

	foundCandidates, searchErr := downloader.SearchSourcesInParallel(ctx, opts.Sources, artist, title, rawSearchQuery, cacheInst, opts.Verbose)

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
					fmt.Printf("Selected: %q [direct_url]\n", bestCandidate.Title)
				} else {
					fmt.Printf("Selected: %q [%s %s] score=%d\n", bestCandidate.Title, bestCandidate.Source, verifier.FormatDuration(bestCandidate.Duration), bestCandidate.Score)
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

	if sp != nil {
		sp.Stop()
	}

	// Step 2: Download audio stream
	if !opts.IsAgent {
		fmt.Printf("\nDownloading audio stream & artwork (%s)\n", targetURL)
		if sp != nil {
			sp.Update("working...")
			sp.Start()
		}
	}

	_ = opts.ProgressReporter.Emit(progress.Event{
		Type:       progress.EventPhaseStart,
		Phase:      "download",
		Step:       3,
		TotalSteps: 5,
		Message:    fmt.Sprintf("downloading audio stream (%s)", targetURL),
	})

	downloadedPath, err := downloader.DownloadAudioStream(ctx, targetURL, opts.OutDir, opts.Verbose)
	if sp != nil {
		sp.Stop()
	}
	if err != nil {
		_ = opts.ProgressReporter.Emit(progress.Event{
			Type:  progress.EventError,
			Phase: "download",
			Error: err.Error(),
		})
		if opts.IsAgent {
			fmt.Printf("target: %s\nstatus: error\nerror: %v\n", urlOrQuery, err)
		}
		return fmt.Errorf("downloading audio stream for %s: %w", targetURL, err)
	}

	downloadedFilename := filepath.Base(downloadedPath)
	if !opts.IsAgent {
		fmt.Printf("  - saved: %s\n", downloadedFilename)
	}

	// Step 3: Full Verification
	var report *verifier.VerificationReport
	if !opts.SkipVerify {
		if !opts.IsAgent {
			fmt.Println("\nRunning audio quality & spectrum inspection")
			if sp != nil {
				sp.Update("working...")
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

		rep, err := verifier.VerifyAudioTrack(ctx, downloadedPath, opts.Verbose)
		if sp != nil {
			sp.Stop()
		}
		if err != nil {
			if !opts.IsAgent {
				fmt.Printf("  - notice: %v\n", err)
			}
		} else {
			report = rep
			if !opts.IsAgent {
				fmt.Printf("  - duration: %s (%s)\n", report.MixStructure.DurationFormatted, report.MixStructure.MixTypeDescription)
				fmt.Printf("  - bandwidth: %s (%d kHz)\n", report.Quality.BandwidthRating, report.Quality.EstimatedBandwidthHz/1000)
				fmt.Printf("  - peak / rms: %.2f dBFS / %.2f dBFS\n", report.Quality.PeakDbFS, report.Quality.RMSDbFS)
				gainSign := ""
				if report.Quality.SuggestedDJGainDb > 0 {
					gainSign = "+"
				}
				fmt.Printf("  - gain offset: %s%.1f dB\n", gainSign, report.Quality.SuggestedDJGainDb)
				fmt.Printf("  - status: %s\n", strings.TrimPrefix(report.SummaryStatus, "STATUS: "))
			}
		}
	} else if !opts.IsAgent {
		fmt.Println("\nStep 3: Skipped DJ Audio Quality & Spectrum Inspection (-skipVerify)")
	}

	// Step 4: Metadata & High-Res Cover Art Enrichment via tag-track
	finalPath := downloadedPath
	var metaResult *TrackMetadataResult

	if !opts.SkipMetadata {
		if !opts.IsAgent {
			fmt.Println("\nEnriching metadata & cover art via tag-track")
			if sp != nil {
				sp.Update("working... running tag-track")
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
				fmt.Printf("  - notice: tagging via tag-track failed: %s\n", cleanErr)
			}
		} else {
			tagOut := string(tagOutBytes)
			for _, line := range strings.Split(tagOut, "\n") {
				line = strings.TrimSpace(line)
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
				}
			}
			if finalPath != downloadedPath {
				_ = os.Remove(downloadedPath)
			}
			if !opts.IsAgent && metaResult != nil && metaResult.Title != "" {
				fmt.Printf("  - matched: \"%s - %s\" (%s, %s)\n", metaResult.Artist, metaResult.Title, metaResult.Album, metaResult.ReleaseYear)
				fmt.Printf("  - source: %s\n", metaResult.Source)
			}
		}
	} else if !opts.IsAgent {
		fmt.Println("\nStep 4: Skipped Metadata & Cover Art Enrichment (-skipMetadata)")
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
			fmt.Printf("metadata: \"%s - %s\" (%s, %s) [%s]\n", metaResult.Artist, metaResult.Title, metaResult.Album, metaResult.ReleaseYear, metaResult.Source)
		}
		fmt.Printf("output: %s\n", outDisplayPath)
		return nil
	}

	fmt.Printf("\nDONE: %s\n", outDisplayPath)

	return nil
}
