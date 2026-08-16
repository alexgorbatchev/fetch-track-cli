package pipeline

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dj/fetch-track-cli/internal/downloader"
	"github.com/dj/fetch-track-cli/internal/metadata"
	"github.com/dj/fetch-track-cli/internal/spinner"
	"github.com/dj/fetch-track-cli/internal/verifier"
)

// Options configures the track acquisition pipeline execution.
type Options struct {
	OutDir       string
	Sources      []string
	SkipVerify   bool
	SkipMetadata bool
	Verbose      bool
	IsAgent      bool
}

// IsAgentMode checks if the environment variable AGENT=1 is set.
func IsAgentMode() bool {
	val := strings.TrimSpace(os.Getenv("AGENT"))
	return val == "1" || strings.ToLower(val) == "true"
}

// Run executes the full single-track acquisition pipeline.
func Run(ctx context.Context, urlOrQuery string, opts Options) error {
	if opts.OutDir == "" {
		opts.OutDir = "."
	}
	if len(opts.Sources) == 0 {
		opts.Sources = []string{"youtube", "soundcloud", "bandcamp"}
	}
	if IsAgentMode() {
		opts.IsAgent = true
	}

	isURL := verifier.IsURL(urlOrQuery)
	targetURL := urlOrQuery

	artist := ""
	title := urlOrQuery
	rawSearchQuery := urlOrQuery

	var initialCandidates []downloader.Candidate

	if isURL {
		if !opts.IsAgent {
			fmt.Println("\nInspecting provided URL metadata & extracting track search terms...")
		}
		meta, err := verifier.FetchURLMetadata(ctx, urlOrQuery)
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
				fmt.Printf("  URL Title: \"%s\" (Uploader: %s)\n", meta.Title, meta.Uploader)
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
			fmt.Printf("  Warning: Could not probe direct URL metadata: %v. Proceeding with URL.\n", err)
		}
	} else {
		if strings.Contains(urlOrQuery, " - ") {
			parts := strings.SplitN(urlOrQuery, " - ", 2)
			artist = strings.TrimSpace(parts[0])
			title = strings.TrimSpace(parts[1])
		}
	}

	var sp *spinner.Spinner
	if !opts.IsAgent {
		fmt.Printf("\nsearching sources (%s) for best extended mix\n", strings.Join(opts.Sources, ", "))
		if !opts.Verbose {
			sp = spinner.New("working...")
			sp.Start()
		}
	}

	foundCandidates, searchErr := downloader.SearchSourcesInParallel(ctx, opts.Sources, artist, title, rawSearchQuery, opts.Verbose)

	var candidatePool []downloader.Candidate
	candidatePool = append(candidatePool, initialCandidates...)
	if searchErr == nil {
		candidatePool = append(candidatePool, foundCandidates...)
	}

	var selectedCandidate *downloader.Candidate

	if len(candidatePool) > 0 {
		bestCandidate, evalErr := downloader.EvaluateAndInspectCandidatesInParallel(ctx, candidatePool, artist, title, opts.Verbose)
		if evalErr == nil && bestCandidate != nil {
			selectedCandidate = bestCandidate
			targetURL = bestCandidate.WebpageURL
			if sp != nil {
				sp.Stop()
			}
			if !opts.IsAgent {
				if bestCandidate.Source == "direct_url" {
					fmt.Printf("selected: %q [direct_url]\n", bestCandidate.Title)
				} else {
					fmt.Printf("selected: %q [%s %s] score=%d\n", bestCandidate.Title, bestCandidate.Source, verifier.FormatDuration(bestCandidate.Duration), bestCandidate.Score)
				}
			}
		}
	}

	if sp != nil {
		sp.Stop()
	}

	// Step 2: Download audio stream
	if !opts.IsAgent {
		fmt.Printf("\ndownloading audio stream & artwork (%s)\n", targetURL)
		if sp != nil {
			sp.Update("working...")
			sp.Start()
		}
	}

	downloadedPath, err := downloader.DownloadAudioStream(ctx, targetURL, opts.OutDir, opts.Verbose)
	if sp != nil {
		sp.Stop()
	}
	if err != nil {
		if opts.IsAgent {
			fmt.Printf("target: %s\nstatus: error\nerror: %v\n", urlOrQuery, err)
		}
		return fmt.Errorf("downloading audio stream for %s: %w", targetURL, err)
	}

	downloadedFilename := filepath.Base(downloadedPath)
	if !opts.IsAgent {
		fmt.Printf("  Saved: %s\n", downloadedFilename)
	}

	// Step 3: Full Verification
	var report *verifier.VerificationReport
	if !opts.SkipVerify {
		if !opts.IsAgent {
			fmt.Println("\nrunning audio quality & spectrum inspection")
			if sp != nil {
				sp.Update("working...")
				sp.Start()
			}
		}

		rep, err := verifier.VerifyAudioTrack(ctx, downloadedPath, opts.Verbose)
		if sp != nil {
			sp.Stop()
		}
		if err != nil {
			if !opts.IsAgent {
				fmt.Printf("  Audio verification notice: %v\n", err)
			}
		} else {
			report = rep
			if !opts.IsAgent {
				fmt.Printf("  Duration  : %s (%s)\n", report.MixStructure.DurationFormatted, report.MixStructure.MixTypeDescription)
				fmt.Printf("  Bandwidth : %s (%d kHz)\n", report.Quality.BandwidthRating, report.Quality.EstimatedBandwidthHz/1000)
				fmt.Printf("  Peak / RMS: %.2f dBFS / %.2f dBFS\n", report.Quality.PeakDbFS, report.Quality.RMSDbFS)
				gainSign := ""
				if report.Quality.SuggestedDJGainDb > 0 {
					gainSign = "+"
				}
				fmt.Printf("  DJ Trim   : %s%.1f dB\n", gainSign, report.Quality.SuggestedDJGainDb)
				fmt.Printf("  Status    : [ %s ]\n", report.SummaryStatus)

				if report.SummaryStatus == "FAIL" && report.MixStructure.IsRadioEditWarning {
					fmt.Println("  WARNING: Downloaded track appears to be a short radio edit.")
				}
			}
		}
	} else if !opts.IsAgent {
		fmt.Println("\nStep 3: Skipped DJ Audio Quality & Spectrum Inspection (-skipVerify)")
	}

	// Step 4: Metadata & High-Res Cover Art Enrichment
	finalPath := downloadedPath
	var metaResult *metadata.TrackMetadataResult

	if !opts.SkipMetadata {
		if !opts.IsAgent {
			fmt.Println("\nenriching metadata & cover art via API fallback")
			if sp != nil {
				sp.Update("working...")
				sp.Start()
			}
		}
		ext := filepath.Ext(downloadedFilename)
		cleanTitle := strings.TrimSuffix(downloadedFilename, ext)

		artist := ""
		title := cleanTitle
		if strings.Contains(cleanTitle, " - ") {
			parts := strings.SplitN(cleanTitle, " - ", 2)
			artist = strings.TrimSpace(parts[0])
			title = strings.TrimSpace(parts[1])
		}

		metaClient := metadata.NewClient()
		metaRes := metaClient.ResolveTrackMetadata(ctx, cleanTitle, artist, title, opts.Verbose)
		metaResult = &metaRes

		if !opts.IsAgent {
			fmt.Printf("  Matched  : \"%s - %s\" (%s, %s)\n", metaRes.Artist, metaRes.Title, metaRes.Album, metaRes.ReleaseYear)
			fmt.Printf("  Source   : %s\n", metaRes.Source)
		}

		taggedPath, tagErr := metadata.ApplyMetadataToLocalTrack(ctx, downloadedPath, metaRes, opts.OutDir, opts.Verbose)
		if sp != nil {
			sp.Stop()
		}
		if tagErr != nil {
			if !opts.IsAgent {
				fmt.Printf("  Tagging notice: %v\n", tagErr)
			}
		} else {
			finalPath = taggedPath
		}
	} else if !opts.IsAgent {
		fmt.Println("\nStep 4: Skipped metadata & cover art enrichment (-skipMetadata)")
	}

	finalFilename := filepath.Base(finalPath)
	outDisplayPath := filepath.Join(opts.OutDir, finalFilename)

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
			fmt.Printf("dynamics: peak=%.2f dBFS rms=%.2f dBFS trim=%s%.1f dB\n", report.Quality.PeakDbFS, report.Quality.RMSDbFS, gainSign, report.Quality.SuggestedDJGainDb)
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
