package verifier

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/alexgorbatchev/fetch-track-cli/internal/deps"
)

const TargetRMSDb = -12.0

const (
	// DefaultMinBandwidthHz is the club playback bar: MP3 at 224 kbps and
	// above, AAC at 256 kbps and lossless audio reach it. Opus codes about
	// 20 kHz at every music bitrate, so bandwidth cannot grade Opus.
	DefaultMinBandwidthHz = 19000
	// Below this cutoff audio is lower quality than a 128 kbps MP3, typically
	// a low-bitrate encode or a transcode of one.
	reducedBandwidthHz = 16000

	excerptStartSeconds = 30
	// Tracks shorter than this are analysed from their start.
	shortTrackSeconds = 35
	// The spectrum averages the whole excerpt; peak, loudness and bass levels
	// cover its first dynamicsSeconds.
	excerptSeconds  = 60
	dynamicsSeconds = 30
	// Rates above 48 kHz only add content above the audible band.
	maxAnalysisRate = 48000
	analysisTimeout = 30 * time.Second
)

// AudioQualityReport contains frequency spectrum and dynamics analysis results.
type AudioQualityReport struct {
	EstimatedBandwidthHz   int     `json:"estimatedBandwidthHz"`
	BandwidthRating        string  `json:"bandwidthRating"`
	HasLowBandwidthWarning bool    `json:"hasLowBandwidthWarning"`
	PeakDbFS               float64 `json:"peakDbFS"`
	RMSDbFS                float64 `json:"rmsDbFS"`
	HasClippingWarning     bool    `json:"hasClippingWarning"`
	SubBassDbFS            float64 `json:"subBassDbFS"`
	KickBassDbFS           float64 `json:"kickBassDbFS"`
	SuggestedDJGainDb      float64 `json:"suggestedDjGainDb"`
}

// BandwidthKHz returns the estimated bandwidth in kHz for display; it keeps the
// estimate's 100 Hz resolution when printed with one decimal.
func (r AudioQualityReport) BandwidthKHz() float64 {
	return float64(r.EstimatedBandwidthHz) / 1000
}

// ComputeGoertzelDb returns the level in dBFS of the freqHz component of
// samples[startIdx:endIdx] using the Goertzel recurrence over every sample.
func ComputeGoertzelDb(samples []float32, startIdx, endIdx int, freqHz, sampleRate float64) float64 {
	endIdx = min(endIdx, len(samples))
	count := endIdx - startIdx
	if count <= 0 {
		return -120.0
	}

	coefficient := 2 * math.Cos(2*math.Pi*freqHz/sampleRate)
	var previous, beforePrevious float64
	for _, s := range samples[startIdx:endIdx] {
		previous, beforePrevious = float64(s)+coefficient*previous-beforePrevious, previous
	}
	power := previous*previous + beforePrevious*beforePrevious - coefficient*previous*beforePrevious

	magnitude := math.Sqrt(max(power, 0)) / float64(count)
	return 20.0 * math.Log10(magnitude+1e-9)
}

// EvaluateBandwidthWarning updates the report bandwidth rating and low-bandwidth
// warning flag; a minBandwidthHz of zero or less applies DefaultMinBandwidthHz.
func EvaluateBandwidthWarning(report *AudioQualityReport, minBandwidthHz int) {
	if minBandwidthHz <= 0 {
		minBandwidthHz = DefaultMinBandwidthHz
	}
	report.HasLowBandwidthWarning = report.EstimatedBandwidthHz < minBandwidthHz

	switch {
	case report.EstimatedBandwidthHz >= DefaultMinBandwidthHz:
		report.BandwidthRating = "High Fidelity (>=19 kHz)"
	case report.EstimatedBandwidthHz >= reducedBandwidthHz:
		report.BandwidthRating = "Reduced Bandwidth (16-19 kHz)"
	default:
		report.BandwidthRating = "Low Quality / Transcoded (<16 kHz)"
	}
}

// AnalyzePCMAudio decodes an excerpt of up to 60 seconds at the source sample
// rate (at most 48 kHz) and measures its bandwidth and dynamics.
func AnalyzePCMAudio(ctx context.Context, filePath string, durationSec float64, minBandwidthHz ...int) (*AudioQualityReport, error) {
	minHz := DefaultMinBandwidthHz
	if len(minBandwidthHz) > 0 && minBandwidthHz[0] > 0 {
		minHz = minBandwidthHz[0]
	}
	cmdCtx, cancel := context.WithTimeout(ctx, analysisTimeout)
	defer cancel()

	sourceRate, err := probeSampleRate(cmdCtx, filePath)
	if err != nil {
		return nil, err
	}
	sampleRate := min(sourceRate, maxAnalysisRate)

	startTime := strconv.Itoa(excerptStartSeconds)
	if durationSec < shortTrackSeconds {
		startTime = "0"
	}

	rawBytes, err := deps.RunCommand(cmdCtx, "ffmpeg",
		"-hide_banner",
		"-v", "quiet",
		"-ss", startTime,
		"-i", filePath,
		// Decode the stream whose sample rate was probed.
		"-map", "0:a:0",
		"-t", strconv.Itoa(excerptSeconds),
		"-f", "f32le",
		"-ac", "1",
		"-ar", strconv.Itoa(sampleRate),
		"pipe:1",
	)
	if err != nil || len(rawBytes) == 0 {
		return nil, fmt.Errorf("ffmpeg PCM extraction failed for %s: %w", filePath, err)
	}

	numSamples := len(rawBytes) / 4
	if numSamples == 0 {
		return nil, fmt.Errorf("empty PCM buffer extracted from %s", filePath)
	}

	samples := make([]float32, numSamples)
	r := bytes.NewReader(rawBytes)
	if err := binary.Read(r, binary.LittleEndian, &samples); err != nil {
		return nil, fmt.Errorf("reading PCM float32 samples: %w", err)
	}

	report := measureDynamics(samples[:min(len(samples), dynamicsSeconds*sampleRate)], sampleRate)
	report.EstimatedBandwidthHz = estimateBandwidthHz(samples, sampleRate)
	EvaluateBandwidthWarning(report, minHz)

	return report, nil
}

// measureDynamics reports peak, loudness, bass levels and the gain that brings
// the samples to TargetRMSDb.
func measureDynamics(samples []float32, sampleRate int) *AudioQualityReport {
	var maxAbs float64
	var sumSq float64

	for _, s := range samples {
		abs := math.Abs(float64(s))
		if abs > maxAbs {
			maxAbs = abs
		}
		sumSq += float64(s) * float64(s)
	}

	peakDbFS := 20.0 * math.Log10(maxAbs+1e-9)
	rmsDbFS := 20.0 * math.Log10(math.Sqrt(sumSq/float64(len(samples)))+1e-9)

	subBassDbFS := ComputeGoertzelDb(samples, 0, len(samples), 45.0, float64(sampleRate))
	kickBassDbFS := ComputeGoertzelDb(samples, 0, len(samples), 90.0, float64(sampleRate))

	return &AudioQualityReport{
		PeakDbFS:           math.Round(peakDbFS*100) / 100,
		RMSDbFS:            math.Round(rmsDbFS*100) / 100,
		HasClippingWarning: peakDbFS >= -0.1,
		SubBassDbFS:        math.Round(subBassDbFS*100) / 100,
		KickBassDbFS:       math.Round(kickBassDbFS*100) / 100,
		SuggestedDJGainDb:  math.Round((TargetRMSDb-rmsDbFS)*10) / 10,
	}
}

// probeSampleRate reads the sample rate of the first audio stream in filePath.
func probeSampleRate(ctx context.Context, filePath string) (int, error) {
	out, err := deps.RunCommand(ctx, "ffprobe",
		"-v", "error",
		"-select_streams", "a:0",
		"-show_entries", "stream=sample_rate",
		"-of", "default=noprint_wrappers=1:nokey=1",
		filePath,
	)
	if err != nil {
		return 0, fmt.Errorf("probing sample rate of %s: %w", filePath, err)
	}
	// Containers with programs, such as MPEG-TS, list the stream once per program.
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return 0, fmt.Errorf("probing sample rate of %s: no audio stream with a sample rate", filePath)
	}
	rate, err := strconv.Atoi(fields[0])
	if err != nil || rate <= 0 {
		return 0, fmt.Errorf("probing sample rate of %s: no audio stream with a sample rate", filePath)
	}
	return rate, nil
}
