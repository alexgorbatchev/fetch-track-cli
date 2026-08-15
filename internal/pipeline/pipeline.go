package pipeline

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/dj/fetch-track-cli/internal/downloader"
	"github.com/dj/fetch-track-cli/internal/metadata"
	"github.com/dj/fetch-track-cli/internal/verifier"
)

// Options configures the track acquisition pipeline execution.
type Options struct {
	OutDir       string
	Sources      []string
	SkipVerify   bool
	SkipMetadata bool
	Verbose      bool
}

// Run executes the full single-track acquisition pipeline.
func Run(ctx context.Context, urlOrQuery string, opts Options) error {
	if opts.OutDir == "" {
		opts.OutDir = "tracks"
	}
	if len(opts.Sources) == 0 {
		opts.Sources = []string{"youtube", "soundcloud", "bandcamp"}
	}

	isURL := verifier.IsURL(urlOrQuery)
	targetURL := urlOrQuery

	fmt.Println("\n=======================================================")
	fmt.Println("🎧 DJ FULL MIX TRACK ACQUISITION PIPELINE")
	fmt.Println("=======================================================")
	fmt.Printf("Target: %s\n", urlOrQuery)

	artist := ""
	title := urlOrQuery
	rawSearchQuery := urlOrQuery

	var initialCandidates []downloader.Candidate

	if isURL {
		fmt.Println("\n🔍 Inspecting provided URL metadata & extracting track search terms...")
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

			fmt.Printf("  URL Title: \"%s\" (Uploader: %s)\n", meta.Title, meta.Uploader)

			// Include direct URL as candidate in pool
			initialCandidates = append(initialCandidates, downloader.Candidate{
				ID:         "direct_url",
				Title:      meta.Title,
				Duration:   meta.DurationSeconds,
				Source:     "direct_url",
				WebpageURL: urlOrQuery,
			})
		} else {
			fmt.Printf("  ⚠️ Could not probe direct URL metadata: %v. Proceeding with URL.\n", err)
		}
	} else {
		if strings.Contains(urlOrQuery, " - ") {
			parts := strings.SplitN(urlOrQuery, " - ", 2)
			artist = strings.TrimSpace(parts[0])
			title = strings.TrimSpace(parts[1])
		}
	}

	// ALWAYS search across configured sources in parallel for best DJ MIX track
	fmt.Printf("\n🔍 Searching sources (%s) in parallel for best Extended DJ MIX track...\n", strings.Join(opts.Sources, ", "))
	foundCandidates, searchErr := downloader.SearchSourcesInParallel(ctx, opts.Sources, artist, title, rawSearchQuery)

	var candidatePool []downloader.Candidate
	candidatePool = append(candidatePool, initialCandidates...)
	if searchErr == nil {
		candidatePool = append(candidatePool, foundCandidates...)
	}

	if len(candidatePool) > 0 {
		bestCandidate, evalErr := downloader.EvaluateAndInspectCandidatesInParallel(ctx, candidatePool, artist, title)
		if evalErr == nil && bestCandidate != nil {
			targetURL = bestCandidate.WebpageURL
			if bestCandidate.Source == "direct_url" {
				fmt.Printf("  ✅ Selected Direct URL Candidate (already best Extended DJ Mix): %s\n", targetURL)
			} else {
				fmt.Printf("  ✅ Selected Best Full Extended DJ Mix Candidate [%s]: %s\n", strings.ToUpper(bestCandidate.Source), targetURL)
			}
			if bestCandidate.BandwidthHz > 0 {
				fmt.Printf("  📊 Candidate Spectrum: %d kHz bandwidth | Rank Score: %d\n", bestCandidate.BandwidthHz/1000, bestCandidate.Score)
			}
		}
	}

	// Step 2: Download audio stream
	fmt.Println("\n📥 Step 2: Downloading audio stream & artwork...")
	downloadedPath, err := downloader.DownloadAudioStream(ctx, targetURL, opts.OutDir)
	if err != nil {
		return fmt.Errorf("downloading audio stream for %s: %w", targetURL, err)
	}

	downloadedFilename := filepath.Base(downloadedPath)
	fmt.Printf("  Saved: %s\n", downloadedFilename)

	// Step 3: Full Verification
	var report *verifier.VerificationReport
	if !opts.SkipVerify {
		fmt.Println("\n🔍 Step 3: Running Final DJ Audio Quality & Spectrum Inspection...")
		rep, err := verifier.VerifyAudioTrack(ctx, downloadedPath)
		if err != nil {
			fmt.Printf("  ⚠️ Audio verification notice: %v\n", err)
		} else {
			report = rep
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
				fmt.Println("  ⚠️ WARNING: Downloaded track appears to be a short radio edit.")
			}
		}
	} else {
		fmt.Println("\n🔍 Step 3: Skipped DJ Audio Quality & Spectrum Inspection (-skipVerify)")
	}

	// Step 4: Metadata & High-Res Cover Art Enrichment
	finalPath := downloadedPath
	if !opts.SkipMetadata {
		fmt.Println("\n🖼️ Step 4: Enriching metadata & 1400x1400 cover art via API fallback...")
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
		metaRes := metaClient.ResolveTrackMetadata(ctx, cleanTitle, artist, title)

		fmt.Printf("  Matched  : \"%s - %s\" (%s, %s)\n", metaRes.Artist, metaRes.Title, metaRes.Album, metaRes.ReleaseYear)
		fmt.Printf("  Source   : %s\n", metaRes.Source)

		taggedPath, tagErr := metadata.ApplyMetadataToLocalTrack(ctx, downloadedPath, metaRes, opts.OutDir)
		if tagErr != nil {
			fmt.Printf("  ⚠️ Tagging notice: %v\n", tagErr)
		} else {
			finalPath = taggedPath
		}
	} else {
		fmt.Println("\n🖼️ Step 4: Skipped metadata & cover art enrichment (-skipMetadata)")
	}

	finalFilename := filepath.Base(finalPath)
	fmt.Println("\n=======================================================")
	fmt.Printf("✅ TRACK ACQUISITION COMPLETE: %s/%s\n", opts.OutDir, finalFilename)
	fmt.Println("=======================================================")

	return nil
}
