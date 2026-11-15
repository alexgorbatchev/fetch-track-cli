package verifier

import (
	"math"
	"math/rand/v2"
	"testing"

	"gonum.org/v1/gonum/dsp/fourier"
)

// calibrationToleranceHz bounds the error of a measured cutoff against the
// cutoff the fixture was filtered at.
const calibrationToleranceHz = 150

const (
	calibrationRate    = 44100
	calibrationSamples = 1 << 20 // about 24 seconds at 44.1 kHz
)

// synthesizeNoise returns deterministic pink noise whose amplitude spectrum is
// multiplied by gain. The noise is one periodic block built in the frequency
// domain, so a filter applied through gain is exact and analysis frames never
// see a discontinuity.
func synthesizeNoise(t *testing.T, sampleRate, n int, seed uint64, gain func(hz float64) float64) []float32 {
	t.Helper()
	rng := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	coefficients := make([]complex128, n/2+1)
	for k := 1; k < len(coefficients); k++ {
		hz := float64(k) * float64(sampleRate) / float64(n)
		amplitude := gain(hz) / math.Sqrt(hz)
		phase := rng.Float64() * 2 * math.Pi
		coefficients[k] = complex(amplitude*math.Cos(phase), amplitude*math.Sin(phase))
	}
	coefficients[len(coefficients)-1] = complex(real(coefficients[len(coefficients)-1]), 0)
	sequence := fourier.NewFFT(n).Sequence(nil, coefficients)
	var peak float64
	for _, v := range sequence {
		peak = max(peak, math.Abs(v))
	}
	samples := make([]float32, n)
	for i, v := range sequence {
		samples[i] = float32(v * 0.5 / peak)
	}
	return samples
}

func lowPass(cutoffHz float64) func(float64) float64 {
	return func(hz float64) float64 {
		if hz < cutoffHz {
			return 1
		}
		return 0
	}
}

func fullBand(float64) float64 { return 1 }

func addTones(samples []float32, sampleRate int, amplitude float64, frequencies ...float64) {
	for _, f := range frequencies {
		step := 2 * math.Pi * f / float64(sampleRate)
		for i := range samples {
			samples[i] += float32(amplitude * math.Sin(step*float64(i)))
		}
	}
}

func assertCutoff(t *testing.T, name string, got int, wantHz float64) {
	t.Helper()
	if math.Abs(float64(got)-wantHz) > calibrationToleranceHz {
		t.Errorf("%s: measured %d Hz, want %.0f Hz +/- %d Hz", name, got, wantHz, calibrationToleranceHz)
	}
}

// The ladder reproduces measured encoder cutoffs: MP3 128 kbps (16.5 kHz),
// AAC 128 kbps (17.5 kHz), MP3 192 kbps (18.7 kHz), MP3 256 kbps (19.4 kHz),
// MP3 320 kbps (20.1 kHz) and lossless audio.
func TestEstimateBandwidthCalibrationLadder(t *testing.T) {
	ladder := []float64{16500, 17500, 18700, 19400, 20100}
	previous := 0
	for i, cutoff := range ladder {
		got := estimateBandwidthHz(synthesizeNoise(t, calibrationRate, calibrationSamples, uint64(i+1), lowPass(cutoff)), calibrationRate)
		assertCutoff(t, "low-pass", got, cutoff)
		if got <= previous {
			t.Errorf("cutoff %.0f Hz measured %d Hz, not above the previous rung (%d Hz)", cutoff, got, previous)
		}
		previous = got
	}
	got := estimateBandwidthHz(synthesizeNoise(t, calibrationRate, calibrationSamples, 99, fullBand), calibrationRate)
	if got != 22000 || got <= previous {
		t.Errorf("full band measured %d Hz, want 22000 Hz above every low-pass rung (%d Hz)", got, previous)
	}
}

// Encoders roll off over a few hundred hertz rather than at a single bin.
func TestEstimateBandwidthGradualRollOff(t *testing.T) {
	const cutoff = 18700.0
	rollOff := func(hz float64) float64 {
		switch {
		case hz < cutoff-200:
			return 1
		case hz > cutoff+200:
			return 0
		default:
			return math.Pow(10, -90*(hz-(cutoff-200))/400/20)
		}
	}
	assertCutoff(t, "roll-off", estimateBandwidthHz(synthesizeNoise(t, calibrationRate, calibrationSamples, 7, rollOff), calibrationRate), cutoff)
}

func TestEstimateBandwidthIgnoresTones(t *testing.T) {
	tests := []struct {
		name   string
		cutoff float64
		tones  []float64
	}{
		{"tones above the cutoff", 16500, []float64{17800, 18000, 19500, 21000}},
		{"tones inside the reference band", 17500, []float64{10500, 11000, 12000, 13000, 13500}},
		{"bass and midrange tones", 19400, []float64{45, 90, 440, 1000, 2500, 5000}},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			samples := synthesizeNoise(t, calibrationRate, calibrationSamples, uint64(20+i), lowPass(tt.cutoff))
			// Each tone is far louder than the noise in any single 50 Hz band.
			addTones(samples, calibrationRate, 0.2, tt.tones...)
			assertCutoff(t, tt.name, estimateBandwidthHz(samples, calibrationRate), tt.cutoff)
		})
	}
}

// Below about 12 kHz the 10-14 kHz reference band lies in the codec's stopband;
// silence there must not make the remaining stopband count as program content.
func TestEstimateBandwidthLowBandwidthAudio(t *testing.T) {
	for i, cutoff := range []float64{8000, 11000, 13000} {
		got := estimateBandwidthHz(synthesizeNoise(t, calibrationRate, calibrationSamples, uint64(40+i), lowPass(cutoff)), calibrationRate)
		assertCutoff(t, "low bandwidth", got, cutoff)
	}
}

func TestEstimateBandwidthSampleRates(t *testing.T) {
	tests := []struct {
		rate   int
		cutoff float64
		want   float64
	}{
		{48000, 19400, 19400},
		{48000, 0, 24000},
		{32000, 0, 16000},
		{22050, 8000, 8000},
	}
	for i, tt := range tests {
		gain := fullBand
		if tt.cutoff > 0 {
			gain = lowPass(tt.cutoff)
		}
		samples := synthesizeNoise(t, tt.rate, calibrationSamples, uint64(60+i), gain)
		assertCutoff(t, "sample rate", estimateBandwidthHz(samples, tt.rate), tt.want)
	}
}

func TestEstimateBandwidthSilenceAndShortInput(t *testing.T) {
	if got := estimateBandwidthHz(make([]float32, calibrationRate), calibrationRate); got != 0 {
		t.Errorf("silence measured %d Hz, want 0", got)
	}
	if got := estimateBandwidthHz(nil, calibrationRate); got != 0 {
		t.Errorf("empty input measured %d Hz, want 0", got)
	}
	// A single zero-padded frame has no averaging and half a window, so it only
	// has to land near the cutoff and well below full band.
	short := synthesizeNoise(t, calibrationRate, 4096, 80, lowPass(16500))
	if got := estimateBandwidthHz(short, calibrationRate); got < 15500 || got > 17500 {
		t.Errorf("a single zero-padded frame measured %d Hz, want 16500 Hz +/- 1000 Hz", got)
	}
}
