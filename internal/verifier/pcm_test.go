package verifier

import (
	"math"
	"slices"
	"testing"
)

func TestComputeGoertzelDb(t *testing.T) {
	// Generate a 1-second sine wave at 1000 Hz, sample rate 48000 Hz
	sampleRate := 48000.0
	freq := 1000.0
	numSamples := int(sampleRate)
	samples := make([]float32, numSamples)

	for i := 0; i < numSamples; i++ {
		samples[i] = float32(0.5 * math.Sin(2.0*math.Pi*freq*float64(i)/sampleRate))
	}

	// Measure at 1000 Hz (target freq)
	db1000 := ComputeGoertzelDb(samples, 0, len(samples), 1000.0, sampleRate)
	// Measure at 20000 Hz (off target)
	db20000 := ComputeGoertzelDb(samples, 0, len(samples), 20000.0, sampleRate)

	if db1000 < -20.0 {
		t.Errorf("expected strong signal at 1000Hz, got %f dBFS", db1000)
	}

	if db1000-db20000 < 30.0 {
		t.Errorf("expected >30dB difference between 1000Hz and 20000Hz, got diff = %f", db1000-db20000)
	}
}

// Opus codes about 20.2 kHz at every bitrate, so at minimums of 19 kHz and
// above it also needs an average bitrate of 160 kbps. Below a 19 kHz minimum
// and for other codecs, the measured cutoff alone decides.
func TestEvaluateQualityAppliesCodecBitrateFloors(t *testing.T) {
	tests := []struct {
		name                 string
		codec                string
		bandwidthHz, minHz   int
		bitrateBps           int
		wantMinBitrateBps    int
		wantMeets            bool
		wantReasons          []string
		wantLowBandwidthFlag bool
	}{
		{"opus 128 kbps at 19 kHz", "opus", 20200, 19000, 128019, 160000, false, []string{ReasonBitrateBelowMinimum}, false},
		{"opus just under the floor", "opus", 20200, 19000, 159999, 160000, false, []string{ReasonBitrateBelowMinimum}, false},
		{"opus at the floor", "opus", 20200, 19000, 160000, 160000, true, []string{}, false},
		{"opus 192 kbps at 19 kHz", "opus", 20200, 19000, 192029, 160000, true, []string{}, false},
		{"opus at a 20 kHz minimum", "opus", 20200, 20000, 128019, 160000, false, []string{ReasonBitrateBelowMinimum}, false},
		{"opus at the default minimum", "opus", 20200, 0, 96014, 160000, false, []string{ReasonBitrateBelowMinimum}, false},
		{"opus 96 kbps at 18 kHz", "opus", 20200, 18000, 96014, 0, true, []string{}, false},
		{"opus just under a 19 kHz minimum", "opus", 20200, 18999, 96014, 0, true, []string{}, false},
		{"transcoded opus", "opus", 16700, 19000, 192029, 160000, false, []string{ReasonBandwidthBelowMinimum}, true},
		{"low cutoff and bitrate", "opus", 16700, 19000, 96014, 160000, false, []string{ReasonBandwidthBelowMinimum, ReasonBitrateBelowMinimum}, true},
		{"unmeasured opus bitrate", "opus", 20200, 19000, 0, 160000, false, []string{ReasonBitrateBelowMinimum}, false},
		{"mp3 has no floor", "mp3", 19400, 19000, 128002, 0, true, []string{}, false},
		{"mp3 below the cutoff", "mp3", 16700, 19000, 320005, 0, false, []string{ReasonBandwidthBelowMinimum}, true},
		{"aac has no floor", "aac", 22000, 19000, 96000, 0, true, []string{}, false},
		{"lossless", "flac", 22000, 19000, 1139928, 0, true, []string{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := AudioQualityReport{Codec: tt.codec, EstimatedBandwidthHz: tt.bandwidthHz, BitrateBps: tt.bitrateBps}
			EvaluateQuality(&report, tt.minHz)
			wantMinHz := tt.minHz
			if wantMinHz <= 0 {
				wantMinHz = DefaultMinBandwidthHz
			}
			if report.MinBandwidthHz != wantMinHz || report.MinBitrateBps != tt.wantMinBitrateBps {
				t.Errorf("minimums %d Hz / %d bps, want %d Hz / %d bps", report.MinBandwidthHz, report.MinBitrateBps, wantMinHz, tt.wantMinBitrateBps)
			}
			if report.MeetsMinimum != tt.wantMeets || !slices.Equal(report.LowQualityReasons, tt.wantReasons) || report.LowQualityReasons == nil {
				t.Errorf("meets=%t reasons=%#v, want %t %#v", report.MeetsMinimum, report.LowQualityReasons, tt.wantMeets, tt.wantReasons)
			}
			if report.HasLowBandwidthWarning != tt.wantLowBandwidthFlag {
				t.Errorf("low-bandwidth warning %t, want %t: the flag keeps describing the cutoff alone", report.HasLowBandwidthWarning, tt.wantLowBandwidthFlag)
			}
		})
	}
}

func TestBitrateKbpsNeverRoundsUpToTheFloor(t *testing.T) {
	for _, tt := range []struct {
		bps  int
		want float64
	}{{159999, 159.9}, {160024, 160}, {128019, 128}, {0, 0}} {
		if got := (AudioQualityReport{BitrateBps: tt.bps}).BitrateKbps(); got != tt.want {
			t.Errorf("BitrateKbps(%d) = %v, want %v", tt.bps, got, tt.want)
		}
	}
}

// An unknown bitrate must not read as a measured zero.
func TestCodecSummary(t *testing.T) {
	for _, tt := range []struct {
		report AudioQualityReport
		want   string
	}{
		{AudioQualityReport{Codec: "opus", BitrateBps: 128019}, "opus 128.0 kbps"},
		{AudioQualityReport{Codec: "flac"}, "flac, bitrate unmeasured"},
		{AudioQualityReport{Codec: "opus", BitrateBps: -1}, "opus, bitrate unmeasured"},
	} {
		if got := tt.report.CodecSummary(); got != tt.want {
			t.Errorf("CodecSummary(%+v) = %q, want %q", tt.report, got, tt.want)
		}
	}
}

func TestAnalyzePCMAudio_MinBandwidthThreshold(t *testing.T) {
	report := &AudioQualityReport{
		EstimatedBandwidthHz: 18000,
	}
	EvaluateQuality(report, 18500)
	if !report.HasLowBandwidthWarning {
		t.Errorf("expected HasLowBandwidthWarning = true when bandwidth 18000Hz < min 18500Hz")
	}

	report2 := &AudioQualityReport{
		EstimatedBandwidthHz: 18500,
	}
	EvaluateQuality(report2, 18500)
	if report2.HasLowBandwidthWarning {
		t.Errorf("expected HasLowBandwidthWarning = false when bandwidth 18500Hz >= min 18500Hz")
	}
}
