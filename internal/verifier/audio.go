package verifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dj/fetch-track-cli/internal/cache"
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

	cmd := exec.CommandContext(cmdCtx, "ffprobe",
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		filePath,
	)

	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil || stdout.Len() == 0 {
		return nil, fmt.Errorf("probing local file %s: %w", filePath, err)
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

	if err := json.Unmarshal(stdout.Bytes(), &rawData); err != nil {
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
func VerifyAudioTrack(ctx context.Context, target string, verbose ...bool) (*VerificationReport, error) {
	return VerifyAudioTrackWithRunner(ctx, defaultRunnerVar, target, verbose...)
}

// VerifyAudioTrackWithRunner analyzes audio track using a provided CommandRunner.
func VerifyAudioTrackWithRunner(ctx context.Context, runner CommandRunner, target string, verbose ...bool) (*VerificationReport, error) {
	isVerbose := len(verbose) > 0 && verbose[0]
	isURL := IsURL(target)
	var metadata *TrackMetadata
	var err error

	if isVerbose {
		fmt.Printf("verify: %s\n", target)
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
		fmt.Printf("probed: %q [%s] (%s)\n", metadata.Title, sourceLabel, FormatDuration(metadata.DurationSeconds))
	}

	localAudioPath := target
	isTempDownloadedFile := false
	tmpDir := ".tmp"

	_, errStat := os.Stat(".tmp")
	dotTmpExisted := !os.IsNotExist(errStat)

	if isURL {
		_ = os.MkdirAll(tmpDir, 0755)
		localAudioPath = filepath.Join(tmpDir, fmt.Sprintf("verify_%d.m4a", time.Now().UnixNano()))
		isTempDownloadedFile = true

		if isVerbose {
			fmt.Printf("sample download: %s\n", localAudioPath)
		}

		dlCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		_, dlErr := runner(dlCtx, "yt-dlp",
			"--no-warnings",
			"--quiet",
			"--js-runtimes", "node",
			"-f", "140/bestaudio[ext=m4a]/bestaudio/best",
			"-x",
			"--audio-format", "m4a",
			"-o", localAudioPath,
			target,
		)
		cancel()
		if dlErr != nil {
			return nil, fmt.Errorf("downloading audio sample for verification from %s: %w", target, dlErr)
		}
	}

	defer func() {
		if isTempDownloadedFile {
			_ = os.Remove(localAudioPath)
			if !dotTmpExisted {
				_ = os.Remove(tmpDir)
			}
		}
	}()

	if isVerbose {
		fmt.Printf("analyzing pcm: goertzel + dynamics\n")
	}

	mixStructure := AnalyzeMixStructure(metadata.Title, metadata.DurationSeconds)
	pcmReport, err := AnalyzePCMAudio(ctx, localAudioPath, metadata.DurationSeconds)
	if err != nil {
		return nil, fmt.Errorf("analyzing PCM audio for %s: %w", localAudioPath, err)
	}

	mixStructure.HasIntroBeats = metadata.DurationSeconds > 210
	mixStructure.HasOutroBeats = metadata.DurationSeconds > 210

	var recommendations []string
	if mixStructure.IsRadioEditWarning {
		recommendations = append(recommendations, "RADIO EDIT WARNING: This track appears to be a short radio edit.")
	} else {
		recommendations = append(recommendations, "MIX LENGTH: Track duration and mix structure are suitable for DJ mixing.")
	}

	if pcmReport.HasLowBandwidthWarning {
		recommendations = append(recommendations, "LOW BANDWIDTH: High frequencies roll off below 16 kHz.")
	} else {
		recommendations = append(recommendations, fmt.Sprintf("AUDIO BANDWIDTH: Clean frequency response (%s).", pcmReport.BandwidthRating))
	}

	summaryStatus := "STATUS: High fidelity audio suitable for mixing."
	switch {
	case mixStructure.IsRadioEditWarning && pcmReport.HasLowBandwidthWarning:
		summaryStatus = "STATUS: Low audio quality detected and appears to be a short radio edit."
	case mixStructure.IsRadioEditWarning:
		summaryStatus = "STATUS: Downloaded track appears to be a short radio edit."
	case pcmReport.HasLowBandwidthWarning:
		summaryStatus = "STATUS: Low audio quality detected."
	}

	return &VerificationReport{
		Metadata:        *metadata,
		MixStructure:    mixStructure,
		Quality:         *pcmReport,
		SummaryStatus:   summaryStatus,
		Recommendations: recommendations,
	}, nil
}
