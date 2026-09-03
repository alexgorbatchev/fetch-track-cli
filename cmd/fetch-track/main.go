package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	cobrahelptree "github.com/alexgorbatchev/cobra-help-tree"
	"github.com/dj/fetch-track-cli/internal/cache"
	"github.com/dj/fetch-track-cli/internal/deps"
	"github.com/dj/fetch-track-cli/internal/pipeline"
	"github.com/dj/fetch-track-cli/internal/progress"
	"github.com/dj/fetch-track-cli/internal/spinner"
	"github.com/dj/fetch-track-cli/internal/verifier"
)

var (
	version = "dev"

	outDir         string
	sourcesFlag    string
	skipVerify     bool
	skipMetadata   bool
	interactive    bool
	noCache        bool
	verbose        bool
	progressTarget string
	progressSocket string
	autoInstall    bool
)

func newRootCommand() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:          "fetch-track <url|query>",
		Short:        "Fetch and verify high-quality single tracks for DJ collections",
		Version:      version,
		SilenceUsage: true,
		Long: `fetch-track is a CLI tool designed for DJ collections to discover, inspect, download, and manage high-fidelity single audio tracks in native formats.

When a URL is provided, fetch-track inspects the link and downloads the native audio stream while searching across configured sources for higher-fidelity extended mixes. Supported direct streaming platforms include:
  - YouTube (https://www.youtube.com/watch?v=..., https://youtu.be/...)
  - SoundCloud (https://soundcloud.com/...)
  - Bandcamp (https://artist.bandcamp.com/track/...)
  - Mixcloud (https://mixcloud.com/...)
  - Any audio/video URL supported by yt-dlp backend extraction (http:// / https://)

When a query is provided, fetch-track executes the full acquisition pipeline:
  - Searches configured sources (YouTube, SoundCloud, Bandcamp) in parallel
  - Prioritizes full-length extended, original, club, or dub DJ mixes
  - Concurrently inspects candidates for audio bandwidth and track duration
  - Downloads native high-bitrate audio streams with zero lossy transcoding
  - Performs Goertzel spectral frequency analysis and computes mixer gain offset
  - Identifies tracks via acoustic fingerprinting (Shazam/AcoustID) with API fallbacks
  - Enriches audio files with canonical metadata and 1400x1400 square cover art`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}

			if err := ensureDependencies(cmd.Context()); err != nil {
				return err
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

			targetURI := progressTarget
			if targetURI == "" {
				targetURI = progressSocket
			}
			if targetURI == "" {
				targetURI = os.Getenv("FETCH_TRACK_PROGRESS_TARGET")
			}

			var reporter *progress.Reporter
			if strings.TrimSpace(targetURI) != "" {
				var err error
				reporter, err = progress.NewReporter(cmd.Context(), targetURI)
				if err != nil {
					return fmt.Errorf("initializing progress reporter for %q: %w", targetURI, err)
				}
				defer reporter.Close()
			}

			opts := pipeline.Options{
				OutDir:           outDir,
				Sources:          sources,
				SkipVerify:       skipVerify,
				SkipMetadata:     skipMetadata,
				Interactive:      interactive,
				NoCache:          noCache,
				Verbose:          verbose,
				IsAgent:          deps.IsAgentMode(),
				AutoInstall:      autoInstall,
				ProgressTarget:   targetURI,
				ProgressReporter: reporter,
			}
			return pipeline.Run(cmd.Context(), target, opts)
		},
	}

	rootCmd.SetVersionTemplate("{{.Version}}\n")

	rootCmd.Flags().StringVarP(&outDir, "out-dir", "o", ".", "Output directory for downloaded tracks (default: current working directory)")
	rootCmd.Flags().StringVarP(&sourcesFlag, "sources", "s", "youtube,soundcloud", "Comma-separated list of sources to search in parallel")
	rootCmd.Flags().BoolVar(&skipVerify, "skip-verify", false, "Skip DJ audio quality and spectrum inspection")
	rootCmd.Flags().BoolVar(&skipMetadata, "skip-metadata", false, "Skip metadata lookup and high-res cover art tagging")
	rootCmd.Flags().BoolVar(&skipMetadata, "no-metadata", false, "Alias for --skip-metadata")
	_ = rootCmd.Flags().MarkHidden("no-metadata")
	rootCmd.Flags().BoolVarP(&interactive, "interactive", "i", false, "Interactively approve or choose track candidate before downloading")
	rootCmd.Flags().BoolVar(&noCache, "no-cache", false, "Disable local caching for search queries, metadata, and artwork")
	rootCmd.Flags().StringVar(&progressTarget, "progress-target", "", "Target URI/address for streaming JSON progress events (e.g. unix:///path/to.sock, tcp://127.0.0.1:9099, fd://3, stdout, stderr)")
	rootCmd.Flags().StringVar(&progressSocket, "progress-socket", "", "Shorthand alias for --progress-target")
	rootCmd.Flags().BoolVar(&autoInstall, "auto-install", false, "Automatically install missing dependencies without prompting")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output logging")

	verifyCmd := &cobra.Command{
		Use:          "verify <url|path>",
		Short:        "Run DJ audio quality and spectrum inspection on a track file or URL",
		SilenceUsage: true,
		Args:         cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			target := strings.Join(args, " ")
			isAgent := deps.IsAgentMode()

			if err := ensureDependencies(cmd.Context()); err != nil {
				if isAgent {
					fmt.Printf("target: %s\nstatus: error\nerror: %v\n", target, err)
				}
				return err
			}

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

	verifyCmd.Flags().BoolVar(&autoInstall, "auto-install", false, "Automatically install missing dependencies without prompting")

	depsCmd := &cobra.Command{
		Use:          "dependencies",
		Aliases:      []string{"deps"},
		Short:        "Verify required external binary dependencies (yt-dlp, ffmpeg, ffprobe)",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			isAgent := deps.IsAgentMode()
			reports, err := deps.VerifyDependencies(cmd.Context())

			if isAgent {
				for _, r := range reports {
					if r.Satisfied {
						fmt.Printf("%s: ok (version %s, min %s)\n", r.Name, r.DetectedVersion, r.MinVersion)
					} else if !r.Installed {
						fmt.Printf("%s: missing\n", r.Name)
					} else {
						fmt.Printf("%s: fail (version %s, min %s)\n", r.Name, r.DetectedVersion, r.MinVersion)
					}
				}
				if err != nil {
					fmt.Printf("status: error\nerror: %v\n", err)
					return err
				}
				fmt.Println("status: ok")
				return nil
			}

			for _, r := range reports {
				if r.Satisfied {
					fmt.Printf("%s: %s (min %s) [OK]\n", r.Name, r.DetectedVersion, r.MinVersion)
				} else if !r.Installed {
					fmt.Printf("%s: missing in $PATH [FAIL] - %s\n", r.Name, r.Error)
				} else {
					fmt.Printf("%s: %s (min %s) [FAIL] - %s\n", r.Name, r.DetectedVersion, r.MinVersion, r.Error)
				}
			}

			if err != nil {
				return err
			}
			fmt.Println("\nAll dependencies met.")
			return nil
		},
	}

	depsInstallCmd := &cobra.Command{
		Use:          "install [dep...]",
		Aliases:      []string{"add", "get"},
		Short:        "Install missing external dependencies (yt-dlp, ffmpeg, ffprobe)",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = deps.InitManagedPath()
			if len(args) > 0 {
				for _, depName := range args {
					fmt.Printf("Installing %s...\n", depName)
					if err := deps.InstallDependency(cmd.Context(), depName); err != nil {
						return fmt.Errorf("installing %s: %w", depName, err)
					}
					fmt.Printf("%s installed successfully.\n", depName)
				}
				return nil
			}

			fmt.Println("Checking and installing missing dependencies...")
			installed, err := deps.InstallMissingDependencies(cmd.Context())
			if err != nil {
				return err
			}
			if len(installed) == 0 {
				fmt.Println("All dependencies are already satisfied.")
			} else {
				fmt.Printf("Successfully installed: %s\n", strings.Join(installed, ", "))
			}
			return nil
		},
	}

	depsUpdateCmd := &cobra.Command{
		Use:          "update [dep...]",
		Aliases:      []string{"upgrade"},
		Short:        "Update external dependencies to their latest versions",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = deps.InitManagedPath()
			if len(args) > 0 {
				for _, depName := range args {
					fmt.Printf("Updating %s...\n", depName)
					if err := deps.UpdateDependency(cmd.Context(), depName); err != nil {
						return fmt.Errorf("updating %s: %w", depName, err)
					}
					fmt.Printf("%s updated successfully.\n", depName)
				}
				return nil
			}

			fmt.Println("Updating all dependencies to latest versions...")
			updated, err := deps.UpdateAllDependencies(cmd.Context())
			if err != nil {
				return err
			}
			fmt.Printf("Successfully updated: %s\n", strings.Join(updated, ", "))
			return nil
		},
	}

	depsCmd.AddCommand(depsInstallCmd)
	depsCmd.AddCommand(depsUpdateCmd)

	upgradeCmd := &cobra.Command{
		Use:          "upgrade",
		Aliases:      []string{"self-update", "update-self"},
		Short:        "Upgrade fetch-track CLI binary to the latest released version",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			isAgent := deps.IsAgentMode()
			if !isAgent {
				fmt.Printf("Checking for newer fetch-track release (current version: %s)...\n", version)
			}
			latestVer, err := deps.UpgradeSelf(cmd.Context(), version)
			if err != nil {
				if strings.Contains(err.Error(), "already at the latest version") {
					if isAgent {
						fmt.Printf("upgrade: current\nversion: %s\n", version)
					} else {
						fmt.Printf("fetch-track is already up to date (version %s).\n", version)
					}
					return nil
				}
				return fmt.Errorf("upgrade failed: %w", err)
			}
			if isAgent {
				fmt.Printf("upgrade: ok\nversion: %s\n", latestVer)
			} else {
				fmt.Printf("Successfully upgraded fetch-track to version %s!\n", latestVer)
			}
			return nil
		},
	}

	rootCmd.AddCommand(verifyCmd)
	rootCmd.AddCommand(depsCmd)
	rootCmd.AddCommand(upgradeCmd)

	cobrahelptree.Setup(rootCmd)

	return rootCmd
}

var stdinReader = io.Reader(os.Stdin)

func run(ctx context.Context, args []string) error {
	rootCmd := newRootCommand()
	rootCmd.SetArgs(args)
	return rootCmd.ExecuteContext(ctx)
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Handle second Ctrl+C for forced immediate exit
	go func() {
		<-ctx.Done()
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
		<-sigChan
		fmt.Fprintln(os.Stderr, "\nForced termination requested. Exiting.")
		os.Exit(130)
	}()

	if err := run(ctx, os.Args[1:]); err != nil {
		if ctx.Err() != nil {
			fmt.Fprintln(os.Stderr, "Operation canceled by user.")
			os.Exit(130)
		}
		os.Exit(1)
	}
}

func ensureDependencies(ctx context.Context) error {
	_ = deps.InitManagedPath()
	c, _ := cache.New(true)
	reports, err := deps.VerifyDependencies(ctx, c)
	if err == nil {
		return nil
	}

	var missing []string
	var missingNames []string
	for _, r := range reports {
		if !r.Satisfied {
			missingNames = append(missingNames, r.Name)
			if r.Installed && r.DetectedVersion != "" {
				missing = append(missing, fmt.Sprintf("%s (installed version %s, required %s or newer)", r.Name, r.DetectedVersion, r.MinVersion))
			} else {
				missing = append(missing, fmt.Sprintf("%s (missing, required %s or newer)", r.Name, r.MinVersion))
			}
		}
	}

	if autoInstall {
		if !deps.IsAgentMode() {
			fmt.Printf("Auto-installing missing dependencies: %s...\n", strings.Join(missing, ", "))
		}
		installed, installErr := deps.InstallMissingDependencies(ctx)
		if installErr != nil {
			return fmt.Errorf("auto-installing dependencies: %w", installErr)
		}
		if len(installed) > 0 && !deps.IsAgentMode() {
			fmt.Printf("Successfully installed: %s\n", strings.Join(installed, ", "))
		}
		return nil
	}

	if !deps.IsAgentMode() {
		reader := bufio.NewReader(stdinReader)
		fmt.Printf("\nMissing required dependencies: %s\nWould you like to auto-install them to managed directory? [Y/n]: ", strings.Join(missing, ", "))
		ans, _ := reader.ReadString('\n')
		ans = strings.TrimSpace(strings.ToLower(ans))
		if ans == "" || ans == "y" || ans == "yes" {
			fmt.Printf("Installing dependencies: %s...\n", strings.Join(missingNames, ", "))
			installed, installErr := deps.InstallMissingDependencies(ctx)
			if installErr != nil {
				return fmt.Errorf("auto-installing dependencies: %w", installErr)
			}
			if len(installed) > 0 {
				fmt.Printf("Successfully installed: %s\n\n", strings.Join(installed, ", "))
			}
			return nil
		}
	}

	return err
}
