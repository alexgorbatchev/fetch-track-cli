package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/dj/fetch-track-cli/internal/pipeline"
	"github.com/dj/fetch-track-cli/internal/spinner"
	"github.com/dj/fetch-track-cli/internal/verifier"
)

var (
	version = "dev"

	outDir       string
	sourcesFlag  string
	skipVerify   bool
	skipMetadata bool
	verbose      bool
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	rootCmd := &cobra.Command{
		Use:          "fetch-track <youtube_url_or_search_query>",
		Short:        "Fetch and verify high-quality single tracks for DJ collections",
		Version:      version,
		SilenceUsage: true,
		Long: `fetch-track is a CLI tool designed for DJ collections.
It searches configured sources (YouTube, SoundCloud, Bandcamp) in parallel for full/extended DJ mixes,
inspects track candidates concurrently for audio bandwidth and track length, downloads native audio streams,
performs spectral bandwidth & loudness analysis, and enriches files with 1400x1400 cover art.`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			target := strings.Join(args, " ")

			var sources []string
			if strings.TrimSpace(sourcesFlag) != "" {
				for _, s := range strings.Split(sourcesFlag, ",") {
					cleaned := strings.TrimSpace(s)
					if cleaned != "" {
						sources = append(sources, cleaned)
					}
				}
			}

			opts := pipeline.Options{
				OutDir:       outDir,
				Sources:      sources,
				SkipVerify:   skipVerify,
				SkipMetadata: skipMetadata,
				Verbose:      verbose,
				IsAgent:      pipeline.IsAgentMode(),
			}
			return pipeline.Run(cmd.Context(), target, opts)
		},
	}

	rootCmd.SetVersionTemplate("{{.Version}}\n")

	rootCmd.Flags().StringVarP(&outDir, "out-dir", "o", ".", "Output directory for downloaded tracks (default: current working directory)")
	rootCmd.Flags().StringVarP(&sourcesFlag, "sources", "s", "youtube,soundcloud", "Comma-separated list of sources to search in parallel")
	rootCmd.Flags().BoolVar(&skipVerify, "skip-verify", false, "Skip DJ audio quality and spectrum inspection")
	rootCmd.Flags().BoolVar(&skipMetadata, "skip-metadata", false, "Skip metadata lookup and high-res cover art tagging")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output logging")

	verifyCmd := &cobra.Command{
		Use:          "verify <file_path_or_url>",
		Short:        "Run DJ audio quality and spectrum inspection on a track file or URL",
		SilenceUsage: true,
		Args:         cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			target := strings.Join(args, " ")
			isAgent := pipeline.IsAgentMode()

			var sp *spinner.Spinner
			if !isAgent && !verbose {
				sp = spinner.New(fmt.Sprintf("Running Audio Quality Verification on %s...", target))
				sp.Start()
			} else if !isAgent {
				fmt.Printf("Running Audio Quality Verification on: %s\n\n", target)
			}

			report, err := verifier.VerifyAudioTrack(cmd.Context(), target, verbose)
			if sp != nil {
				sp.Stop()
			}
			if err != nil {
				if isAgent {
					fmt.Printf("target: %s\nstatus: error\nerror: %v\n", target, err)
				}
				return fmt.Errorf("audio verification failed: %w", err)
			}

			if isAgent {
				fmt.Printf("target: %s\n", target)
				fmt.Printf("title: %s\n", report.Metadata.Title)
				fmt.Printf("duration: %s (%s)\n", report.MixStructure.DurationFormatted, report.MixStructure.MixTypeDescription)
				fmt.Printf("bandwidth: %d kHz (%s)\n", report.Quality.EstimatedBandwidthHz/1000, report.Quality.BandwidthRating)
				gainSign := ""
				if report.Quality.SuggestedDJGainDb > 0 {
					gainSign = "+"
				}
				fmt.Printf("dynamics: peak=%.2f dBFS rms=%.2f dBFS gain=%s%.1f dB\n", report.Quality.PeakDbFS, report.Quality.RMSDbFS, gainSign, report.Quality.SuggestedDJGainDb)
				fmt.Printf("status: %s\n", report.SummaryStatus)
				for _, rec := range report.Recommendations {
					fmt.Printf("rec: %s\n", rec)
				}
				return nil
			}

			fmt.Printf("Title: %s\n", report.Metadata.Title)
			fmt.Printf("Duration: %s (%s)\n", report.MixStructure.DurationFormatted, report.MixStructure.MixTypeDescription)
			fmt.Printf("Bandwidth: %s (%d kHz)\n", report.Quality.BandwidthRating, report.Quality.EstimatedBandwidthHz/1000)
			fmt.Printf("Peak / RMS: %.2f dBFS / %.2f dBFS\n", report.Quality.PeakDbFS, report.Quality.RMSDbFS)
			fmt.Printf("Sub / Kick: %.2f dBFS / %.2f dBFS\n", report.Quality.SubBassDbFS, report.Quality.KickBassDbFS)
			gainSign := ""
			if report.Quality.SuggestedDJGainDb > 0 {
				gainSign = "+"
			}
			fmt.Printf("Gain Offset: %s%.1f dB\n", gainSign, report.Quality.SuggestedDJGainDb)
			fmt.Printf("%s\n\n", report.SummaryStatus)

			fmt.Println("Recommendations:")
			for _, rec := range report.Recommendations {
				fmt.Printf("  %s\n", rec)
			}

			if report.MixStructure.IsRadioEditWarning {
				os.Exit(2)
			}
			return nil
		},
	}

	rootCmd.AddCommand(verifyCmd)

	if err := rootCmd.ExecuteContext(ctx); err != nil {
		os.Exit(1)
	}
}
