package verifier

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/alexgorbatchev/fetch-track-cli/internal/cache"
	"github.com/alexgorbatchev/fetch-track-cli/internal/deps"
)

// TrackMetadata holds probed information about a track.
type TrackMetadata struct {
	Title           string  `json:"title"`
	Uploader        string  `json:"uploader"`
	DurationSeconds float64 `json:"durationSeconds"`
	Format          string  `json:"format"`
	Date            string  `json:"date,omitempty"`
	Comment         string  `json:"comment,omitempty"`
	SourceURLOrPath string  `json:"sourceUrlOrPath"`
	Cached          bool    `json:"cached,omitempty"`
}

// VerificationReport contains full structural and audio quality diagnostics.
type VerificationReport struct {
	Metadata        TrackMetadata      `json:"metadata"`
	MixStructure    MixStructureReport `json:"mixStructure"`
	Quality         AudioQualityReport `json:"quality"`
	SummaryStatus   string             `json:"summaryStatus"` // PASS, WARNING, FAIL
	Recommendations []string           `json:"recommendations"`
}

// IsURL checks if input is an HTTP(S) link or web domain.
func IsURL(input string) bool {
	lower := strings.ToLower(input)
	return strings.HasPrefix(lower, "http://") ||
		strings.HasPrefix(lower, "https://") ||
		strings.Contains(lower, "youtube.com") ||
		strings.Contains(lower, "youtu.be") ||
		strings.Contains(lower, "soundcloud.com") ||
		strings.Contains(lower, "bandcamp.com") ||
		strings.Contains(lower, "mixcloud.com")
}

// IsYouTubeURL maintains backwards compatibility.
func IsYouTubeURL(input string) bool {
	return IsURL(input)
}

// CommandRunner abstracts command execution for testability.
type CommandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

// defaultRunner stops tools gracefully on cancellation and timeout.
var defaultRunner CommandRunner = deps.RunCommand

var defaultRunnerVar = defaultRunner

// GetDefaultRunner returns the active default command runner for verifier.
func GetDefaultRunner() CommandRunner {
	return defaultRunnerVar
}

// SetDefaultRunner overrides the default command runner and returns a restore cleanup function.
func SetDefaultRunner(runner CommandRunner) func() {
	orig := defaultRunnerVar
	defaultRunnerVar = runner
	return func() {
		defaultRunnerVar = orig
	}
}

// FetchURLMetadata fetches metadata for any supported URL via yt-dlp dump-json.
func FetchURLMetadata(ctx context.Context, url string, c ...*cache.Cache) (*TrackMetadata, error) {
	return FetchURLMetadataWithRunner(ctx, defaultRunnerVar, url, c...)
}

// FetchURLMetadataWithRunner fetches metadata using a provided CommandRunner.
func FetchURLMetadataWithRunner(ctx context.Context, runner CommandRunner, url string, c ...*cache.Cache) (*TrackMetadata, error) {
	var cacheInst *cache.Cache
	if len(c) > 0 && c[0] != nil {
		cacheInst = c[0]
	}

	var cachedMeta TrackMetadata
	if cacheInst != nil && cacheInst.Get("url_meta", url, &cachedMeta) {
		cachedMeta.Cached = true
		return &cachedMeta, nil
	}

	if runner == nil {
		runner = defaultRunner
	}

	cmdCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	stdout, err := runner(cmdCtx, "yt-dlp",
		"--dump-json",
		"--no-warnings",
		"--quiet",
		"--js-runtimes", "node",
		url,
	)
	if err != nil || len(stdout) == 0 {
		if err != nil {
			return nil, fmt.Errorf("fetching URL metadata for %s: %w", url, err)
		}
		return nil, fmt.Errorf("fetching URL metadata for %s: empty output", url)
	}

	var rawData struct {
		Title    string  `json:"title"`
		Uploader string  `json:"uploader"`
		Artist   string  `json:"artist"`
		Duration float64 `json:"duration"`
		Ext      string  `json:"ext"`
	}

	if err := json.Unmarshal(stdout, &rawData); err != nil {
		return nil, fmt.Errorf("parsing URL metadata JSON: %w", err)
	}

	title := rawData.Title
	if title == "" {
		title = "Unknown Title"
	}
	uploader := rawData.Uploader
	if uploader == "" {
		uploader = rawData.Artist
	}
	if uploader == "" {
		uploader = "Unknown Uploader"
	}
	format := rawData.Ext
	if format == "" {
		format = "m4a"
	}

	res := &TrackMetadata{
		Title:           title,
		Uploader:        uploader,
		DurationSeconds: rawData.Duration,
		Format:          format,
		SourceURLOrPath: url,
	}

	if cacheInst != nil {
		_ = cacheInst.Put("url_meta", url, res, 24*time.Hour)
	}

	return res, nil
}

// FetchYouTubeMetadata maintains backwards compatibility.
func FetchYouTubeMetadata(ctx context.Context, url string) (*TrackMetadata, error) {
	return FetchURLMetadata(ctx, url)
}

// FetchLocalMetadata probes a local file with ffprobe.
func FetchLocalMetadata(ctx context.Context, filePath string) (*TrackMetadata, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	stdout, err := deps.RunCommand(cmdCtx, "ffprobe",
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		filePath,
	)
	if err != nil {
		return nil, fmt.Errorf("probing local file %s: %w", filePath, err)
	}
	if len(stdout) == 0 {
		return nil, fmt.Errorf("probing local file %s: empty ffprobe output", filePath)
	}

	var rawData struct {
		Format struct {
			Duration   string `json:"duration"`
			FormatName string `json:"format_name"`
			Tags       struct {
				Title       string `json:"title"`
				Artist      string `json:"artist"`
				Date        string `json:"date"`
				Year        string `json:"year"`
				Comment     string `json:"comment"`
				Description string `json:"description"`
			} `json:"tags"`
		} `json:"format"`
	}

	if err := json.Unmarshal(stdout, &rawData); err != nil {
		return nil, fmt.Errorf("parsing ffprobe JSON output: %w", err)
	}

	durationSec, _ := strconv.ParseFloat(rawData.Format.Duration, 64)
	filename := filepath.Base(filePath)

	title := rawData.Format.Tags.Title
	if title == "" {
		title = filename
	}
	uploader := rawData.Format.Tags.Artist
	if uploader == "" {
		uploader = "Local File"
	}
	dateVal := rawData.Format.Tags.Date
	if dateVal == "" {
		dateVal = rawData.Format.Tags.Year
	}
	commentVal := rawData.Format.Tags.Comment
	if commentVal == "" {
		commentVal = rawData.Format.Tags.Description
	}

	return &TrackMetadata{
		Title:           title,
		Uploader:        uploader,
		DurationSeconds: durationSec,
		Format:          rawData.Format.FormatName,
		Date:            dateVal,
		Comment:         commentVal,
		SourceURLOrPath: filePath,
	}, nil
}

// VerifyAudioTrack analyzes a local audio file or remote URL for mix structure and bandwidth.
func VerifyAudioTrack(ctx context.Context, target string, verbose bool, minBandwidthHz ...int) (*VerificationReport, error) {
	return VerifyAudioTrackWithRunner(ctx, defaultRunnerVar, target, verbose, minBandwidthHz...)
}

// VerifyAudioTrackWithRunner analyzes audio track using a provided CommandRunner.
func VerifyAudioTrackWithRunner(ctx context.Context, runner CommandRunner, target string, verbose bool, minBandwidthHz ...int) (*VerificationReport, error) {
	isVerbose := verbose
	minHz := DefaultMinBandwidthHz
	if len(minBandwidthHz) > 0 && minBandwidthHz[0] > 0 {
		minHz = minBandwidthHz[0]
	}
	isURL := IsURL(target)
	var metadata *TrackMetadata
	var err error

	if isVerbose {
		fmt.Fprintf(os.Stderr, "verify: %s\n", deps.FoldHome(target))
	}

	if runner == nil {
		runner = defaultRunner
	}

	if isURL {
		metadata, err = FetchURLMetadataWithRunner(ctx, runner, target)
	} else {
		metadata, err = FetchLocalMetadata(ctx, target)
	}
	if err != nil {
		return nil, fmt.Errorf("fetching metadata for %s: %w", target, err)
	}

	if isVerbose && metadata != nil {
		sourceLabel := "file"
		if isURL {
			lower := strings.ToLower(target)
			switch {
			case strings.Contains(lower, "soundcloud.com"):
				sourceLabel = "soundcloud"
			case strings.Contains(lower, "youtube.com"), strings.Contains(lower, "youtu.be"):
				sourceLabel = "youtube"
			case strings.Contains(lower, "bandcamp.com"):
				sourceLabel = "bandcamp"
			default:
				sourceLabel = "url"
			}
		}
		fmt.Fprintf(os.Stderr, "probed: %q [%s] (%s)\n", metadata.Title, sourceLabel, FormatDuration(metadata.DurationSeconds))
	}

	localAudioPath := target
	tmpDir := ".tmp"

	_, errStat := os.Stat(".tmp")
	dotTmpExisted := !os.IsNotExist(errStat)

	if isURL {
		_ = os.MkdirAll(tmpDir, 0755)
		downloadBase := filepath.Join(tmpDir, fmt.Sprintf("verify_%d", time.Now().UnixNano()))
		defer func() {
			downloads, _ := filepath.Glob(downloadBase + ".*")
			for _, path := range downloads {
				_ = os.Remove(path)
			}
			if !dotTmpExisted {
				_ = os.Remove(tmpDir)
			}
		}()

		if isVerbose {
			fmt.Fprintf(os.Stderr, "sample download: %s\n", deps.FoldHome(downloadBase))
		}

		// Measure the stream acquisition downloads, in its own container.
		// Forcing m4a would select YouTube's 128 kbps AAC stream or re-encode
		// other streams, and the measurement would describe that instead.
		dlCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		_, dlErr := runner(dlCtx, "yt-dlp",
			"--no-warnings",
			"--quiet",
			"--js-runtimes", "node",
			"-f", "bestaudio/best",
			"-x",
			"-o", downloadBase+".%(ext)s",
			target,
		)
		cancel()
		if dlErr != nil {
			return nil, fmt.Errorf("downloading audio sample for verification from %s: %w", target, dlErr)
		}
		downloads, _ := filepath.Glob(downloadBase + ".*")
		if len(downloads) == 0 {
			return nil, fmt.Errorf("downloading audio sample for verification from %s: no audio file was written", target)
		}
		localAudioPath = downloads[0]
	}

	if isVerbose {
		fmt.Fprintf(os.Stderr, "analyzing pcm: spectrum + dynamics\n")
	}

	mixStructure := AnalyzeMixStructure(metadata.Title, metadata.DurationSeconds)
	pcmReport, err := AnalyzePCMAudio(ctx, localAudioPath, metadata.DurationSeconds, minHz)
	if err != nil {
		return nil, fmt.Errorf("analyzing PCM audio for %s: %w", localAudioPath, err)
	}

	mixStructure.HasIntroBeats = metadata.DurationSeconds > 210
	mixStructure.HasOutroBeats = metadata.DurationSeconds > 210

	var recommendations []string
	if mixStructure.IsRadioEditWarning {
		recommendations = append(recommendations, "RADIO EDIT: This track is a radio edit.")
	} else {
		recommendations = append(recommendations, "MIX LENGTH: Track duration and mix structure are suitable for DJ mixing.")
	}

	recommendations = append(recommendations, qualityRecommendations(*pcmReport)...)

	return &VerificationReport{
		Metadata:        *metadata,
		MixStructure:    mixStructure,
		Quality:         *pcmReport,
		SummaryStatus:   fidelityStatus(mixStructure.IsRadioEditWarning, *pcmReport),
		Recommendations: recommendations,
	}, nil
}

// qualityRecommendations describes the report's cutoff against its minimum
// and, when the bitrate misses its codec's floor, the bitrate. A cutoff that
// reaches the minimum is called clean only when the audio meets it.
func qualityRecommendations(report AudioQualityReport) []string {
	var recommendations []string
	switch {
	case report.HasLowBandwidthWarning:
		recommendations = append(recommendations, fmt.Sprintf("LOW BANDWIDTH: Frequency cutoff (%d Hz) below minimum threshold (%d Hz).", report.EstimatedBandwidthHz, report.MinBandwidthHz))
	case report.MeetsMinimum:
		recommendations = append(recommendations, fmt.Sprintf("AUDIO BANDWIDTH: Clean frequency response (%s).", report.BandwidthRating))
	default:
		recommendations = append(recommendations, fmt.Sprintf("AUDIO BANDWIDTH: Frequency cutoff %.1f kHz reaches the %.1f kHz minimum; see LOW BITRATE.", report.BandwidthKHz(), kilohertz(report.MinBandwidthHz)))
	}
	if slices.Contains(report.LowQualityReasons, ReasonBitrateBelowMinimum) {
		recommendations = append(recommendations, fmt.Sprintf("LOW BITRATE: %s at a %.1f kHz minimum. %s frequency cutoffs do not reflect bitrate.", bitrateShortfall(report), kilohertz(report.MinBandwidthHz), bitrateFloors[report.Codec].label))
	}
	return recommendations
}

// fidelityStatus summarises the verdict and whether the track is a radio edit.
// Audio that meets the requested minimum but not the default bar, including
// its codec's bitrate floor there, is standard fidelity.
func fidelityStatus(isRadioEdit bool, report AudioQualityReport) string {
	reasons := strings.Join(lowQualityReasonText(report), "; ")
	switch {
	case isRadioEdit && !report.MeetsMinimum:
		return fmt.Sprintf("STATUS: Low audio quality detected (%s) and is a radio edit.", reasons)
	case isRadioEdit:
		return "STATUS: Downloaded track is a radio edit."
	case !report.MeetsMinimum:
		return fmt.Sprintf("STATUS: Low audio quality detected (%s).", reasons)
	case !meetsDefaultBar(report):
		return "STATUS: Standard fidelity audio suitable for mixing."
	}
	return "STATUS: High fidelity audio suitable for mixing."
}

// meetsDefaultBar grades a copy of the report against DefaultMinBandwidthHz.
func meetsDefaultBar(report AudioQualityReport) bool {
	EvaluateQuality(&report, DefaultMinBandwidthHz)
	return report.MeetsMinimum
}

// lowQualityReasonText describes each of the report's low-quality reasons.
func lowQualityReasonText(report AudioQualityReport) []string {
	var text []string
	for _, reason := range report.LowQualityReasons {
		switch reason {
		case ReasonBandwidthBelowMinimum:
			text = append(text, fmt.Sprintf("frequency cutoff %.1f kHz is below the %.1f kHz minimum", report.BandwidthKHz(), kilohertz(report.MinBandwidthHz)))
		case ReasonBitrateBelowMinimum:
			text = append(text, bitrateShortfall(report))
		}
	}
	return text
}

// bitrateShortfall states how the report's average bitrate misses its codec's
// floor, or that it could not be measured.
func bitrateShortfall(report AudioQualityReport) string {
	label := bitrateFloors[report.Codec].label
	floorKbps := report.MinBitrateBps / 1000
	if report.BitrateBps <= 0 {
		return fmt.Sprintf("%s average bitrate could not be measured; it needs %d kbps", label, floorKbps)
	}
	return fmt.Sprintf("%s average bitrate %.1f kbps is below %d kbps", label, report.BitrateKbps(), floorKbps)
}

// kilohertz converts hertz to kilohertz for display.
func kilohertz(hz int) float64 {
	return float64(hz) / 1000
}
