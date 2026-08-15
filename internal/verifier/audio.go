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
)

// TrackMetadata holds probed information about a track.
type TrackMetadata struct {
	Title           string  `json:"title"`
	Uploader        string  `json:"uploader"`
	DurationSeconds float64 `json:"durationSeconds"`
	Format          string  `json:"format"`
	SourceURLOrPath string  `json:"sourceUrlOrPath"`
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

// FetchURLMetadata fetches metadata for any supported URL via yt-dlp dump-json.
func FetchURLMetadata(ctx context.Context, url string) (*TrackMetadata, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "yt-dlp",
		"--dump-json",
		"--no-warnings",
		"--quiet",
		"--js-runtimes", "node",
		url,
	)

	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil || stdout.Len() == 0 {
		return nil, fmt.Errorf("fetching URL metadata for %s: %w", url, err)
	}

	var rawData struct {
		Title    string  `json:"title"`
		Uploader string  `json:"uploader"`
		Artist   string  `json:"artist"`
		Duration float64 `json:"duration"`
		Ext      string  `json:"ext"`
	}

	if err := json.Unmarshal(stdout.Bytes(), &rawData); err != nil {
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

	return &TrackMetadata{
		Title:           title,
		Uploader:        uploader,
		DurationSeconds: rawData.Duration,
		Format:          format,
		SourceURLOrPath: url,
	}, nil
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
				Title  string `json:"title"`
				Artist string `json:"artist"`
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

	return &TrackMetadata{
		Title:           title,
		Uploader:        uploader,
		DurationSeconds: durationSec,
		Format:          rawData.Format.FormatName,
		SourceURLOrPath: filePath,
	}, nil
}

// VerifyAudioTrack analyzes a local audio file or remote URL for mix structure and bandwidth.
func VerifyAudioTrack(ctx context.Context, target string) (*VerificationReport, error) {
	isURL := IsURL(target)
	var metadata *TrackMetadata
	var err error

	if isURL {
		metadata, err = FetchURLMetadata(ctx, target)
	} else {
		metadata, err = FetchLocalMetadata(ctx, target)
	}
	if err != nil {
		return nil, fmt.Errorf("fetching metadata for %s: %w", target, err)
	}

	localAudioPath := target
	isTempDownloadedFile := false

	if isURL {
		tmpDir := filepath.Join(os.TempDir(), "fetch-track-cli-tmp")
		_ = os.MkdirAll(tmpDir, 0755)
		localAudioPath = filepath.Join(tmpDir, fmt.Sprintf("verify_%d.m4a", time.Now().UnixNano()))
		isTempDownloadedFile = true

		dlCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		cmd := exec.CommandContext(dlCtx, "yt-dlp",
			"--no-warnings",
			"--quiet",
			"--js-runtimes", "node",
			"-f", "140/bestaudio[ext=m4a]/bestaudio/best",
			"-x",
			"-o", localAudioPath,
			target,
		)
		dlErr := cmd.Run()
		cancel()
		if dlErr != nil {
			return nil, fmt.Errorf("downloading audio sample for verification from %s: %w", target, dlErr)
		}
	}

	defer func() {
		if isTempDownloadedFile {
			_ = os.Remove(localAudioPath)
		}
	}()

	mixStructure := AnalyzeMixStructure(metadata.Title, metadata.DurationSeconds)
	pcmReport, err := AnalyzePCMAudio(ctx, localAudioPath, metadata.DurationSeconds)
	if err != nil {
		return nil, fmt.Errorf("analyzing PCM audio for %s: %w", localAudioPath, err)
	}

	mixStructure.HasIntroBeats = metadata.DurationSeconds > 210
	mixStructure.HasOutroBeats = metadata.DurationSeconds > 210

	var recommendations []string
	if mixStructure.IsRadioEditWarning {
		recommendations = append(recommendations, "⚠️ RADIO EDIT WARNING: This track appears to be a short radio edit.")
	} else {
		recommendations = append(recommendations, "✅ MIX LENGTH: Track duration and mix structure are suitable for DJ mixing.")
	}

	if pcmReport.HasLowBandwidthWarning {
		recommendations = append(recommendations, "⚠️ LOW BANDWIDTH: High frequencies roll off below 16 kHz.")
	} else {
		recommendations = append(recommendations, fmt.Sprintf("✅ AUDIO BANDWIDTH: Clean frequency response (%s).", pcmReport.BandwidthRating))
	}

	summaryStatus := "PASS"
	if mixStructure.IsRadioEditWarning || pcmReport.HasLowBandwidthWarning {
		if mixStructure.IsRadioEditWarning {
			summaryStatus = "FAIL"
		} else {
			summaryStatus = "WARNING"
		}
	}

	return &VerificationReport{
		Metadata:        *metadata,
		MixStructure:    mixStructure,
		Quality:         *pcmReport,
		SummaryStatus:   summaryStatus,
		Recommendations: recommendations,
	}, nil
}
