package verifier

import (
	"bytes"
	"context"
	"encoding/binary"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// writeFloatWAV writes 32-bit float PCM with one channel per sample slice,
// which ffmpeg decodes losslessly, so a fixture's spectrum reaches the
// analyser unchanged.
func writeFloatWAV(t *testing.T, path string, sampleRate int, channels ...[]float32) {
	t.Helper()
	interleaved := make([]float32, 0, len(channels)*len(channels[0]))
	for i := range channels[0] {
		for _, channel := range channels {
			interleaved = append(interleaved, channel[i])
		}
	}
	var data bytes.Buffer
	if err := binary.Write(&data, binary.LittleEndian, interleaved); err != nil {
		t.Fatal(err)
	}
	frameBytes := 4 * len(channels)
	var file bytes.Buffer
	for _, v := range []any{
		[]byte("RIFF"), uint32(36 + data.Len()), []byte("WAVE"),
		[]byte("fmt "), uint32(16), uint16(3), uint16(len(channels)), uint32(sampleRate), uint32(sampleRate * frameBytes), uint16(frameBytes), uint16(32),
		[]byte("data"), uint32(data.Len()),
	} {
		if err := binary.Write(&file, binary.LittleEndian, v); err != nil {
			t.Fatal(err)
		}
	}
	file.Write(data.Bytes())
	if err := os.WriteFile(path, file.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

func requireFFmpeg(t *testing.T) {
	t.Helper()
	for _, tool := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not installed", tool)
		}
	}
}

// calibrationFixture writes stereo noise with independent channels. Encoders
// choose their low-pass from the bitrate per channel, so a mono fixture would
// get the cutoff of twice the bitrate.
func calibrationFixture(t *testing.T, name string, sampleRate int, gain func(float64) float64) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name+".wav")
	left := synthesizeNoise(t, sampleRate, calibrationSamples, 1, gain)
	right := synthesizeNoise(t, sampleRate, calibrationSamples, 2, gain)
	writeFloatWAV(t, path, sampleRate, left, right)
	return path
}

// Decoding must keep the source rate: decimating the PCM, or analysing it at
// a lower rate, folds or removes everything above the new Nyquist frequency.
func TestAnalyzePCMAudioMeasuresCutoffAtSourceRate(t *testing.T) {
	requireFFmpeg(t)
	tests := []struct {
		name       string
		sampleRate int
		gain       func(float64) float64
		min, max   int
		warning    bool
		rating     string
	}{
		{"mp3 128 kbps cutoff", 44100, lowPass(16500), 16350, 16650, true, "Reduced Bandwidth (16-19 kHz)"},
		{"lossless 44.1 kHz", 44100, fullBand, 22000, 22000, false, "High Fidelity (>=19 kHz)"},
		{"lossless 96 kHz analysed at 48 kHz", 96000, fullBand, 22000, 24000, false, "High Fidelity (>=19 kHz)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := calibrationFixture(t, strings.ReplaceAll(tt.name, " ", "-"), tt.sampleRate, tt.gain)
			report, err := AnalyzePCMAudio(context.Background(), path, float64(calibrationSamples)/float64(tt.sampleRate))
			if err != nil {
				t.Fatal(err)
			}
			if report.EstimatedBandwidthHz < tt.min || report.EstimatedBandwidthHz > tt.max {
				t.Errorf("bandwidth %d Hz, want %d-%d Hz", report.EstimatedBandwidthHz, tt.min, tt.max)
			}
			if report.HasLowBandwidthWarning != tt.warning || report.BandwidthRating != tt.rating {
				t.Errorf("warning=%t rating=%q, want %t %q", report.HasLowBandwidthWarning, report.BandwidthRating, tt.warning, tt.rating)
			}
		})
	}
}

func TestAnalyzePCMAudioSilence(t *testing.T) {
	requireFFmpeg(t)
	path := filepath.Join(t.TempDir(), "silence.wav")
	writeFloatWAV(t, path, 44100, make([]float32, 44100*2))
	report, err := AnalyzePCMAudio(context.Background(), path, 2)
	if err != nil {
		t.Fatal(err)
	}
	if report.EstimatedBandwidthHz != 0 || !report.HasLowBandwidthWarning || report.PeakDbFS > -170 {
		t.Errorf("silence report %+v", report)
	}
}

func TestAnalyzePCMAudioRejectsFilesWithoutAudio(t *testing.T) {
	requireFFmpeg(t)
	path := filepath.Join(t.TempDir(), "not-audio.wav")
	if err := os.WriteFile(path, []byte("not audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := AnalyzePCMAudio(context.Background(), path, 60); err == nil || !strings.Contains(err.Error(), "sample rate") {
		t.Fatalf("error = %v, want a sample rate probe failure", err)
	}
}

// encoderCutoffs are the cutoffs the ladder must measure within
// encoderToleranceHz, from LAME and libopus encodes of real music. A zero
// wantHz only checks the side of the default minimum: ffmpeg's native AAC
// encoder differs from the encoders those measurements used.
var encoderCutoffs = []struct {
	name         string
	encoder      string
	extension    string
	args         []string
	wantHz       int
	meetsMinimum bool
}{
	{"mp3-128", "libmp3lame", ".mp3", []string{"-b:a", "128k"}, 16650, false},
	{"mp3-192", "libmp3lame", ".mp3", []string{"-b:a", "192k"}, 18700, false},
	{"mp3-224", "libmp3lame", ".mp3", []string{"-b:a", "224k"}, 19400, true},
	{"mp3-256", "libmp3lame", ".mp3", []string{"-b:a", "256k"}, 19400, true},
	{"mp3-320", "libmp3lame", ".mp3", []string{"-b:a", "320k"}, 20150, true},
	{"opus-160", "libopus", ".opus", []string{"-b:a", "160k"}, 20050, true},
	{"aac-256", "aac", ".m4a", []string{"-b:a", "256k"}, 0, true},
}

const encoderToleranceHz = 300

func hasEncoder(t *testing.T, name string) bool {
	t.Helper()
	out, err := exec.Command("ffmpeg", "-hide_banner", "-encoders").Output()
	return err == nil && bytes.Contains(out, []byte(" "+name+" "))
}

func encode(t *testing.T, source, target, encoder string, args ...string) {
	t.Helper()
	command := append([]string{"-hide_banner", "-v", "error", "-y", "-i", source, "-c:a", encoder}, args...)
	if out, err := exec.Command("ffmpeg", append(command, target)...).CombinedOutput(); err != nil {
		t.Fatalf("encoding %s: %v %s", target, err, out)
	}
}

// The ladder encodes one full-band fixture with real encoders. It runs when
// ffmpeg provides them and skips otherwise.
func TestAnalyzePCMAudioEncoderLadder(t *testing.T) {
	requireFFmpeg(t)
	dir := t.TempDir()
	source := calibrationFixture(t, "full-band", 44100, fullBand)
	duration := float64(calibrationSamples) / 44100
	measure := func(path string) int {
		t.Helper()
		report, err := AnalyzePCMAudio(context.Background(), path, duration)
		if err != nil {
			t.Fatal(err)
		}
		return report.EstimatedBandwidthHz
	}
	if got := measure(source); got != 22000 {
		t.Errorf("full-band source measured %d Hz, want 22000 Hz", got)
	}
	previousMP3, encoded := 0, 0
	for _, rung := range encoderCutoffs {
		if !hasEncoder(t, rung.encoder) {
			t.Logf("skipping %s: ffmpeg lacks %s", rung.name, rung.encoder)
			continue
		}
		path := filepath.Join(dir, rung.name+rung.extension)
		encode(t, source, path, rung.encoder, rung.args...)
		encoded++
		got := measure(path)
		t.Logf("%s: %d Hz", rung.name, got)
		if rung.wantHz != 0 && math.Abs(float64(got-rung.wantHz)) > encoderToleranceHz {
			t.Errorf("%s measured %d Hz, want %d Hz +/- %d Hz", rung.name, got, rung.wantHz, encoderToleranceHz)
		}
		if got >= DefaultMinBandwidthHz != rung.meetsMinimum {
			t.Errorf("%s at %d Hz is on the wrong side of the %d Hz default minimum", rung.name, got, DefaultMinBandwidthHz)
		}
		if rung.encoder == "libmp3lame" {
			if got < previousMP3 {
				t.Errorf("%s measured %d Hz, below the lower bitrate (%d Hz)", rung.name, got, previousMP3)
			}
			previousMP3 = got
		}
	}
	if encoded == 0 {
		t.Skip("ffmpeg has none of the ladder's encoders")
	}
	if !hasEncoder(t, "libmp3lame") {
		return
	}
	// Re-encoding at a higher bitrate cannot restore what the first encode removed.
	transcode := filepath.Join(dir, "mp3-128-to-320.mp3")
	encode(t, filepath.Join(dir, "mp3-128.mp3"), transcode, "libmp3lame", "-b:a", "320k")
	got := measure(transcode)
	t.Logf("mp3-128-to-320: %d Hz", got)
	if got >= 17000 {
		t.Errorf("128 kbps re-encoded at 320 kbps measured %d Hz, want below 17000 Hz", got)
	}
}

func TestBandwidthKHzKeepsHundredHertzSteps(t *testing.T) {
	if got := (AudioQualityReport{EstimatedBandwidthHz: 18800}).BandwidthKHz(); got != 18.8 {
		t.Errorf("BandwidthKHz() = %v, want 18.8", got)
	}
}

func TestComputeGoertzelDbRejectsAliases(t *testing.T) {
	// 12045 Hz folds onto 45 Hz when 48 kHz audio is decimated by four
	// without a low-pass filter.
	const sampleRate = 48000.0
	samples := make([]float32, int(sampleRate)*2)
	for i := range samples {
		samples[i] = float32(0.5 * math.Sin(2*math.Pi*12045*float64(i)/sampleRate))
	}
	if level := ComputeGoertzelDb(samples, 0, len(samples), 45, sampleRate); level > -60 {
		t.Errorf("45 Hz level %.1f dBFS from a 12045 Hz tone, want below -60 dBFS", level)
	}
	if level := ComputeGoertzelDb(samples, 0, len(samples), 12045, sampleRate); math.Abs(level-20*math.Log10(0.25)) > 0.5 {
		t.Errorf("12045 Hz level %.1f dBFS, want %.1f dBFS", level, 20*math.Log10(0.25))
	}
	if level := ComputeGoertzelDb(samples, 10, 5, 45, sampleRate); level != -120 {
		t.Errorf("empty range level %.1f dBFS, want -120", level)
	}
}

// Probing and decoding must use the same stream. ffmpeg's default stream
// selection prefers the stereo second stream here, which would be resampled to
// the first stream's probed rate and read as a 11 kHz file.
func TestAnalyzePCMAudioDecodesTheProbedStream(t *testing.T) {
	requireFFmpeg(t)
	dir := t.TempDir()
	first := filepath.Join(dir, "first.wav")
	writeFloatWAV(t, first, 22050, synthesizeNoise(t, 22050, calibrationSamples, 3, lowPass(6000)))
	second := calibrationFixture(t, "second", 44100, fullBand)
	path := filepath.Join(dir, "two-streams.mka")
	command := []string{"-hide_banner", "-v", "error", "-y", "-i", first, "-i", second, "-map", "0:a", "-map", "1:a", "-c:a", "flac", "-disposition:a:0", "0", "-disposition:a:1", "default", path}
	if out, err := exec.Command("ffmpeg", command...).CombinedOutput(); err != nil {
		t.Fatalf("building fixture: %v %s", err, out)
	}
	report, err := AnalyzePCMAudio(context.Background(), path, 30)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(float64(report.EstimatedBandwidthHz-6000)) > calibrationToleranceHz {
		t.Errorf("bandwidth %d Hz, want the first stream's 6000 Hz", report.EstimatedBandwidthHz)
	}
}

func TestProbeSampleRateReadsProgramContainers(t *testing.T) {
	requireFFmpeg(t)
	source := filepath.Join(t.TempDir(), "source.wav")
	writeFloatWAV(t, source, 44100, make([]float32, 44100))
	path := filepath.Join(t.TempDir(), "program.ts")
	if out, err := exec.Command("ffmpeg", "-hide_banner", "-v", "error", "-y", "-i", source, "-c:a", "aac", "-f", "mpegts", path).CombinedOutput(); err != nil {
		t.Fatalf("building fixture: %v %s", err, out)
	}
	if got, err := probeSampleRate(context.Background(), path); err != nil || got != 44100 {
		t.Errorf("MPEG-TS rate %d, %v; want 44100", got, err)
	}
}

func TestProbeSampleRateReportsSourceRate(t *testing.T) {
	requireFFmpeg(t)
	for _, rate := range []int{22050, 44100, 96000} {
		path := filepath.Join(t.TempDir(), strconv.Itoa(rate)+".wav")
		writeFloatWAV(t, path, rate, make([]float32, 1024))
		got, err := probeSampleRate(context.Background(), path)
		if err != nil || got != rate {
			t.Errorf("rate %d: got %d, %v", rate, got, err)
		}
	}
}
