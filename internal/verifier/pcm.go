package verifier

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"os/exec"
	"time"
)

const TargetRMSDb = -12.0

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

// ComputeGoertzelDb calculates the magnitude in dBFS at freqHz using Goertzel algorithm over float32 samples.
func ComputeGoertzelDb(samples []float32, startIdx, endIdx int, freqHz, sampleRate float64) float64 {
	step := 4
	count := (endIdx - startIdx) / step
	if count <= 0 {
		return -120.0
	}

	k := (2.0 * math.Pi * freqHz) / sampleRate
	var re, im float64

	for i := startIdx; i < endIdx && i < len(samples); i += step {
		s := float64(samples[i])
		re += s * math.Cos(k*float64(i))
		im += s * math.Sin(k*float64(i))
	}

	magnitude := math.Sqrt(re*re+im*im) / float64(count)
	return 20.0 * math.Log10(magnitude+1e-9)
}

// AnalyzePCMAudio extracts 30 seconds of float32 PCM audio via ffmpeg and computes dynamics and bandwidth statistics.
func AnalyzePCMAudio(ctx context.Context, filePath string, durationSec float64) (*AudioQualityReport, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Extract 30 seconds of audio starting at 30 seconds (or 0 if track is <30s)
	startTime := "30"
	if durationSec < 35 {
		startTime = "0"
	}

	cmd := exec.CommandContext(cmdCtx, "ffmpeg",
		"-v", "quiet",
		"-hide_banner",
		"-ss", startTime,
		"-i", filePath,
		"-t", "30",
		"-f", "f32le",
		"-ac", "1",
		"-ar", "48000",
		"pipe:1",
	)

	var pcmBuf bytes.Buffer
	cmd.Stdout = &pcmBuf
	if err := cmd.Run(); err != nil || pcmBuf.Len() == 0 {
		return nil, fmt.Errorf("ffmpeg PCM extraction failed for %s: %w", filePath, err)
	}

	rawBytes := pcmBuf.Bytes()
	numSamples := len(rawBytes) / 4
	if numSamples == 0 {
		return nil, fmt.Errorf("empty PCM buffer extracted from %s", filePath)
	}

	samples := make([]float32, numSamples)
	r := bytes.NewReader(rawBytes)
	if err := binary.Read(r, binary.LittleEndian, &samples); err != nil {
		return nil, fmt.Errorf("reading PCM float32 samples: %w", err)
	}

	sampleRate := 48000.0
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
	rmsDbFS := 20.0 * math.Log10(math.Sqrt(sumSq/float64(numSamples))+1e-9)

	subBassDbFS := ComputeGoertzelDb(samples, 0, len(samples), 45.0, sampleRate)
	kickBassDbFS := ComputeGoertzelDb(samples, 0, len(samples), 90.0, sampleRate)
	midRangeDbFS := ComputeGoertzelDb(samples, 0, len(samples), 1000.0, sampleRate)

	highFreqs := []float64{14000, 16000, 17000, 18000, 19000, 20000}
	estimatedBandwidthHz := 20000

	for _, f := range highFreqs {
		db := ComputeGoertzelDb(samples, 0, len(samples), f, sampleRate)
		if midRangeDbFS-db > 30.0 {
			estimatedBandwidthHz = int(f)
			break
		}
	}

	hasLowBandwidthWarning := estimatedBandwidthHz < 16000
	bandwidthRating := "Standard YouTube (16-18.5 kHz)"
	if estimatedBandwidthHz >= 18500 {
		bandwidthRating = "High Fidelity (>=18.5 kHz)"
	} else if estimatedBandwidthHz < 16000 {
		bandwidthRating = "Low Quality / Transcoded (<16 kHz)"
	}

	hasClippingWarning := peakDbFS >= -0.1
	suggestedDJGainDb := math.Round((TargetRMSDb-rmsDbFS)*10) / 10

	return &AudioQualityReport{
		EstimatedBandwidthHz:   estimatedBandwidthHz,
		BandwidthRating:        bandwidthRating,
		HasLowBandwidthWarning: hasLowBandwidthWarning,
		PeakDbFS:               math.Round(peakDbFS*100) / 100,
		RMSDbFS:                math.Round(rmsDbFS*100) / 100,
		HasClippingWarning:     hasClippingWarning,
		SubBassDbFS:            math.Round(subBassDbFS*100) / 100,
		KickBassDbFS:           math.Round(kickBassDbFS*100) / 100,
		SuggestedDJGainDb:      suggestedDJGainDb,
	}, nil
}
