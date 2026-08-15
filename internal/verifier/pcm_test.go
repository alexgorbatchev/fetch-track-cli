package verifier

import (
	"math"
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
