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
	SkipVerify   bool
	SkipMetadata bool
	Verbose      bool
}

// Run executes the full single-track acquisition pipeline.
func Run(ctx context.Context, urlOrQuery string, opts Options) error {
	if opts.OutDir == "" {
		opts.OutDir = "tracks"
	}

	isURL := verifier.IsYouTubeURL(urlOrQuery)
	targetURL := urlOrQuery

	fmt.Println("\n=======================================================")
	fmt.Println("🎧 DJ FULL MIX TRACK ACQUISITION PIPELINE")
	fmt.Println("=======================================================")
	fmt.Printf("Target: %s\n", urlOrQuery)

	// Step 1: Candidate resolution if input is a search query
	if !isURL {
		fmt.Println("\n🔍 Step 1: Inspecting top YouTube candidates for Full Extended DJ Mix...")
		artist := ""
		title := urlOrQuery
		if strings.Contains(urlOrQuery, " - ") {
			parts := strings.SplitN(urlOrQuery, " - ", 2)
			artist = strings.TrimSpace(parts[0])
			title = strings.TrimSpace(parts[1])
		}

		resolvedURL, err := downloader.SearchYouTubeCandidates(ctx, artist, title, urlOrQuery)
		if err == nil && resolvedURL != "" {
			targetURL = resolvedURL
			fmt.Printf("  ✅ Selected Full DJ Mix Candidate: %s\n", targetURL)
		} else {
			targetURL = fmt.Sprintf("ytsearch1:%s Extended Mix", urlOrQuery)
			fmt.Printf("  Fallback Search Query: %s\n", targetURL)
		}
	} else {
		fmt.Println("\n📥 Step 1: Specified YouTube URL accepted...")
	}

	// Step 2: Download audio stream
	fmt.Println("\n📥 Step 2: Downloading audio stream & artwork...")
	downloadedPath, err := downloader.DownloadAudioStream(ctx, targetURL, opts.OutDir)
	if err != nil {
		return fmt.Errorf("downloading audio stream for %s: %w", targetURL, err)
	}

	downloadedFilename := filepath.Base(downloadedPath)
	fmt.Printf("  Saved: %s\n", downloadedFilename)

	// Step 3: Verification
	var report *verifier.VerificationReport
	if !opts.SkipVerify {
		fmt.Println("\n🔍 Step 3: Running DJ Audio Quality & Spectrum Inspection...")
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
