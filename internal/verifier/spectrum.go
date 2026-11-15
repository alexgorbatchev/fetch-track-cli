package verifier

import (
	"math"
	"slices"

	"gonum.org/v1/gonum/dsp/fourier"
)

// Lossy encoders remove everything above a fixed low-pass frequency, so the
// averaged spectrum of decoded audio ends in a cliff. The bandwidth is the
// highest frequency whose level stays within cutoffDropDb of the program level
// in the reference band (10-14 kHz for sources at 44.1 kHz and above), measured
// in 50 Hz bands of an averaged power spectrum.
const (
	spectrumFrameSize = 8192
	spectrumHop       = spectrumFrameSize / 2
	cutoffBandHz      = 50
	cutoffDropDb      = 25.0
	// stopbandFloorDb limits how far below the 1-4 kHz level a band may fall
	// and still count as program content. It applies when the reference band
	// itself lies in a codec's stopband (cutoffs below about 12 kHz), where a
	// threshold relative to that band would accept silence.
	stopbandFloorDb = 70.0
	// cutoffSmoothingBands is the width of the median filter applied across
	// bands. A tone, or a cluster of up to four 50 Hz bands, cannot move the
	// cliff, while a sustained edge passes through unchanged.
	cutoffSmoothingBands = 9
	bandwidthStepHz      = 100
	// referenceTopHz scales the reference bands: 10-14 kHz and 1-4 kHz below
	// 22 kHz, proportionally lower for sources whose Nyquist frequency is lower.
	referenceTopHz = 22000.0
	silentPower    = 1e-30
)

// silentLevelDb is the level of a band that holds no signal at all.
var silentLevelDb = 10 * math.Log10(silentPower)

// estimateBandwidthHz returns the frequency, in 100 Hz steps, where the averaged
// spectrum of mono samples at sampleRate falls off its high-frequency cliff.
// Audio without a cliff reports the highest frequency the sample rate can carry;
// digital silence reports 0.
func estimateBandwidthHz(samples []float32, sampleRate int) int {
	nyquist := float64(sampleRate) / 2
	levels := bandLevelsDb(averagedPowerSpectrum(samples), sampleRate)
	if len(levels) == 0 {
		return 0
	}
	top := min(nyquist, referenceTopHz)
	reference := medianBandLevel(levels, top*10/22, top*14/22)
	program := medianBandLevel(levels, top*1/22, top*4/22)
	if max(reference, program) <= silentLevelDb {
		return 0
	}
	threshold := max(reference-cutoffDropDb, program-stopbandFloorDb)

	smoothed := medianFilter(levels, cutoffSmoothingBands)
	edge := 0
	for b := len(smoothed) - 1; b >= 0; b-- {
		if smoothed[b] >= threshold {
			edge = (b + 1) * cutoffBandHz
			break
		}
	}
	reported := int(math.Round(float64(edge)/bandwidthStepHz)) * bandwidthStepHz
	return min(reported, int(nyquist)/bandwidthStepHz*bandwidthStepHz)
}

// averagedPowerSpectrum returns the mean power of Hann-windowed frames with 50%
// overlap (Welch's method) for frequency bins 0 through the Nyquist frequency.
// Input shorter than one frame is zero-padded.
func averagedPowerSpectrum(samples []float32) []float64 {
	fft := fourier.NewFFT(spectrumFrameSize)
	window := make([]float64, spectrumFrameSize)
	for i := range window {
		window[i] = 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/spectrumFrameSize)
	}
	frame := make([]float64, spectrumFrameSize)
	coefficients := make([]complex128, spectrumFrameSize/2+1)
	power := make([]float64, len(coefficients))
	frames := 0
	for start := 0; start <= max(len(samples)-spectrumFrameSize, 0); start += spectrumHop {
		clear(frame)
		for i, s := range samples[start:min(start+spectrumFrameSize, len(samples))] {
			frame[i] = float64(s) * window[i]
		}
		fft.Coefficients(coefficients, frame)
		for k, c := range coefficients {
			power[k] += real(c)*real(c) + imag(c)*imag(c)
		}
		frames++
	}
	for k := range power {
		power[k] /= float64(frames)
	}
	return power
}

// bandLevelsDb averages spectrum bins into consecutive 50 Hz bands below the
// Nyquist frequency and returns each band's level in dB.
func bandLevelsDb(power []float64, sampleRate int) []float64 {
	binHz := float64(sampleRate) / spectrumFrameSize
	bands := int(float64(sampleRate) / 2 / cutoffBandHz)
	sums := make([]float64, bands)
	counts := make([]int, bands)
	for k, p := range power {
		b := int(float64(k) * binHz / cutoffBandHz)
		if b >= bands {
			break
		}
		sums[b] += p
		counts[b]++
	}
	levels := make([]float64, bands)
	for b := range levels {
		levels[b] = 10 * math.Log10(sums[b]/float64(max(counts[b], 1))+silentPower)
	}
	return levels
}

// medianBandLevel returns the median level of the bands between lowHz and highHz.
func medianBandLevel(levels []float64, lowHz, highHz float64) float64 {
	low := min(int(lowHz/cutoffBandHz), len(levels)-1)
	high := min(max(int(highHz/cutoffBandHz), low+1), len(levels))
	return median(levels[low:high])
}

func medianFilter(levels []float64, width int) []float64 {
	half := width / 2
	smoothed := make([]float64, len(levels))
	for i := range levels {
		smoothed[i] = median(levels[max(i-half, 0):min(i+half+1, len(levels))])
	}
	return smoothed
}

func median(values []float64) float64 {
	sorted := slices.Clone(values)
	slices.Sort(sorted)
	return sorted[len(sorted)/2]
}
