package verifier

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alexgorbatchev/fetch-track-cli/internal/deps"
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
	if _, err := AnalyzePCMAudio(context.Background(), path, 60); err == nil || !strings.Contains(err.Error(), "probing audio stream") {
		t.Fatalf("error = %v, want an audio stream probe failure", err)
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

type floorVerdict int

const (
	fails floorVerdict = iota
	passes
	// bitrateDecides marks VBR encodes at the floor: libopus spends more or
	// fewer bits than requested depending on the content, so the measured
	// average decides.
	bitrateDecides
)

// bitrateFloorLadder encodes one full-band fixture at the Opus bitrates the
// floor separates, in VBR and CBR, with MP3 and AAC controls that bandwidth
// alone grades.
var bitrateFloorLadder = []struct {
	name, encoder, extension, codec string
	args                            []string
	nominalBps                      int
	// bitrateTolerance bounds the measured average against nominalBps:
	// constant bitrates land on it, VBR averages depend on the content.
	bitrateTolerance float64
	at19, at18       floorVerdict
}{
	{"opus-cbr-96", "libopus", ".opus", "opus", []string{"-b:a", "96k", "-vbr", "off"}, 96000, 0.01, fails, passes},
	{"opus-cbr-128", "libopus", ".opus", "opus", []string{"-b:a", "128k", "-vbr", "off"}, 128000, 0.01, fails, passes},
	{"opus-cbr-160", "libopus", ".opus", "opus", []string{"-b:a", "160k", "-vbr", "off"}, 160000, 0.01, passes, passes},
	{"opus-cbr-192", "libopus", ".opus", "opus", []string{"-b:a", "192k", "-vbr", "off"}, 192000, 0.01, passes, passes},
	{"opus-vbr-96", "libopus", ".opus", "opus", []string{"-b:a", "96k", "-vbr", "on"}, 96000, 0.2, fails, passes},
	{"opus-vbr-128", "libopus", ".opus", "opus", []string{"-b:a", "128k", "-vbr", "on"}, 128000, 0.2, fails, passes},
	{"opus-vbr-160", "libopus", ".opus", "opus", []string{"-b:a", "160k", "-vbr", "on"}, 160000, 0.2, bitrateDecides, passes},
	{"opus-vbr-192", "libopus", ".opus", "opus", []string{"-b:a", "192k", "-vbr", "on"}, 192000, 0.2, passes, passes},
	{"opus-vbr-128-webm", "libopus", ".webm", "opus", []string{"-b:a", "128k", "-vbr", "on"}, 128000, 0.2, fails, passes},
	{"mp3-128", "libmp3lame", ".mp3", "mp3", []string{"-b:a", "128k"}, 128000, 0.01, fails, fails},
	{"mp3-320", "libmp3lame", ".mp3", "mp3", []string{"-b:a", "320k"}, 320000, 0.01, passes, passes},
	{"aac-256", "aac", ".m4a", "aac", []string{"-b:a", "256k"}, 256000, 0.1, passes, passes},
}

func TestAnalyzePCMAudioAppliesTheOpusBitrateFloor(t *testing.T) {
	requireFFmpeg(t)
	dir := t.TempDir()
	source := calibrationFixture(t, "full-band", 44100, fullBand)
	duration := float64(calibrationSamples) / 44100
	encoded := 0
	for _, rung := range bitrateFloorLadder {
		if !hasEncoder(t, rung.encoder) {
			t.Logf("skipping %s: ffmpeg lacks %s", rung.name, rung.encoder)
			continue
		}
		encoded++
		path := filepath.Join(dir, rung.name+rung.extension)
		encode(t, source, path, rung.encoder, rung.args...)
		for _, minimum := range []struct {
			hz   int
			want floorVerdict
		}{{19000, rung.at19}, {18000, rung.at18}} {
			report, err := AnalyzePCMAudio(context.Background(), path, duration, minimum.hz)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("%s at %d Hz: %s %d bps, %d Hz, meets=%t reasons=%v", rung.name, minimum.hz, report.Codec, report.BitrateBps, report.EstimatedBandwidthHz, report.MeetsMinimum, report.LowQualityReasons)
			if report.Codec != rung.codec {
				t.Errorf("%s codec %q, want %q", rung.name, report.Codec, rung.codec)
			}
			if deviation := math.Abs(float64(report.BitrateBps-rung.nominalBps)) / float64(rung.nominalBps); deviation > rung.bitrateTolerance {
				t.Errorf("%s measured %d bps, want %d bps +/- %.0f%%", rung.name, report.BitrateBps, rung.nominalBps, rung.bitrateTolerance*100)
			}
			want := minimum.want == passes
			if minimum.want == bitrateDecides {
				want = report.BitrateBps >= opusMinBitrateBps
			}
			if report.MeetsMinimum != want {
				t.Errorf("%s at a %d Hz minimum: meets=%t reasons=%v, want meets=%t", rung.name, minimum.hz, report.MeetsMinimum, report.LowQualityReasons, want)
			}
			belowFloor := slices.Contains(report.LowQualityReasons, ReasonBitrateBelowMinimum)
			if wantBelowFloor := rung.codec == "opus" && minimum.hz >= 19000 && report.BitrateBps < opusMinBitrateBps; belowFloor != wantBelowFloor {
				t.Errorf("%s at a %d Hz minimum: bitrate reason %t, want %t", rung.name, minimum.hz, belowFloor, wantBelowFloor)
			}
		}
	}
	if encoded == 0 {
		t.Skip("ffmpeg has none of the ladder's encoders")
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

func TestProbeAudioStreamReadsProgramContainers(t *testing.T) {
	requireFFmpeg(t)
	source := filepath.Join(t.TempDir(), "source.wav")
	writeFloatWAV(t, source, 44100, make([]float32, 44100))
	path := filepath.Join(t.TempDir(), "program.ts")
	if out, err := exec.Command("ffmpeg", "-hide_banner", "-v", "error", "-y", "-i", source, "-c:a", "aac", "-f", "mpegts", path).CombinedOutput(); err != nil {
		t.Fatalf("building fixture: %v %s", err, out)
	}
	got, err := probeAudioStream(context.Background(), deps.RunCommand, path, 1)
	if err != nil || got.SampleRate != 44100 || got.Codec != "aac" || got.BitrateBps <= 0 {
		t.Errorf("MPEG-TS stream %+v, %v; want 44100 Hz AAC with a bitrate", got, err)
	}
}

// One second of mono 32-bit float PCM carries exactly 32 bits per sample.
func TestProbeAudioStreamReportsSourceRateCodecAndBitrate(t *testing.T) {
	requireFFmpeg(t)
	for _, rate := range []int{22050, 44100, 96000} {
		path := filepath.Join(t.TempDir(), strconv.Itoa(rate)+".wav")
		writeFloatWAV(t, path, rate, make([]float32, rate))
		got, err := probeAudioStream(context.Background(), deps.RunCommand, path, 1)
		if err != nil || got.SampleRate != rate || got.Codec != "pcm_f32le" || got.BitrateBps != 32*rate {
			t.Errorf("rate %d: got %+v, %v; want pcm_f32le at %d bps", rate, got, err, 32*rate)
		}
	}
}

// Cover art and container overhead stay out of an Opus bitrate: only the audio
// stream's packets count.
func TestProbeAudioStreamIgnoresCoverArt(t *testing.T) {
	requireFFmpeg(t)
	for _, encoder := range []string{"mjpeg", "libopus"} {
		if !hasEncoder(t, encoder) {
			t.Skipf("ffmpeg lacks %s", encoder)
		}
	}
	dir := t.TempDir()
	source := calibrationFixture(t, "full-band", 44100, fullBand)
	audio := filepath.Join(dir, "plain.opus")
	encode(t, source, audio, "libopus", "-b:a", "128k", "-vbr", "off")
	cover := filepath.Join(dir, "cover.jpg")
	if out, err := exec.Command("ffmpeg", "-hide_banner", "-v", "error", "-y", "-f", "lavfi", "-i", "testsrc2=size=1400x1400:duration=1", "-frames:v", "1", "-q:v", "1", cover).CombinedOutput(); err != nil {
		t.Fatalf("building cover: %v %s", err, out)
	}
	tagged := filepath.Join(dir, "tagged.mka")
	if out, err := exec.Command("ffmpeg", "-hide_banner", "-v", "error", "-y", "-i", audio, "-i", cover, "-map", "0:a", "-map", "1:v", "-c", "copy", "-disposition:v", "attached_pic", tagged).CombinedOutput(); err != nil {
		t.Fatalf("attaching cover: %v %s", err, out)
	}
	duration := float64(calibrationSamples) / 44100
	plain, err := probeAudioStream(context.Background(), deps.RunCommand, audio, duration)
	if err != nil {
		t.Fatal(err)
	}
	withCover, err := probeAudioStream(context.Background(), deps.RunCommand, tagged, duration)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(float64(withCover.BitrateBps-plain.BitrateBps)) > 1000 || math.Abs(float64(plain.BitrateBps-128000)) > 1300 {
		t.Errorf("bitrate with cover %d bps, without %d bps; want the same bitrate near 128000 bps", withCover.BitrateBps, plain.BitrateBps)
	}
	// The container's overall bitrate includes the cover, which is what a
	// format-level measurement would report.
	out, err := exec.Command("ffprobe", "-v", "error", "-show_entries", "format=bit_rate", "-of", "default=noprint_wrappers=1:nokey=1", tagged).Output()
	if err != nil {
		t.Fatal(err)
	}
	if containerBps, err := strconv.Atoi(strings.TrimSpace(string(out))); err != nil || containerBps < plain.BitrateBps+20000 {
		t.Fatalf("fixture container bitrate %q does not include the cover (%v)", out, err)
	}
}

func TestParseStreamHeader(t *testing.T) {
	tests := []struct {
		name         string
		data         string
		want         audioStream
		wantTimeBase string
		wantErr      string
	}{
		{"stated bitrate", `{"streams":[{"codec_name":"mp3","sample_rate":"44100","time_base":"1/14112000","bit_rate":"320000"}]}`, audioStream{Codec: "mp3", SampleRate: 44100, BitrateBps: 320000}, "1/14112000", ""},
		{"numeric values", `{"streams":[{"codec_name":"aac","sample_rate":44100,"time_base":"1/44100","bit_rate":256297}]}`, audioStream{Codec: "aac", SampleRate: 44100, BitrateBps: 256297}, "1/44100", ""},
		{"no stated bitrate", `{"streams":[{"codec_name":"opus","sample_rate":"48000","time_base":"1/48000","bit_rate":"N/A"}]}`, audioStream{Codec: "opus", SampleRate: 48000}, "1/48000", ""},
		{"missing bitrate", `{"streams":[{"codec_name":"flac","sample_rate":"44100","time_base":"1/44100"}]}`, audioStream{Codec: "flac", SampleRate: 44100}, "1/44100", ""},
		{"first of repeated streams", `{"streams":[{"codec_name":"aac","sample_rate":"44100","bit_rate":"128000"},{"codec_name":"mp3","sample_rate":"48000"}]}`, audioStream{Codec: "aac", SampleRate: 44100, BitrateBps: 128000}, "", ""},
		{"no stream", `{"streams":[]}`, audioStream{}, "", "no audio stream with a sample rate"},
		{"no sample rate", `{"streams":[{"codec_name":"opus","sample_rate":"0"}]}`, audioStream{}, "", "no audio stream with a sample rate"},
		{"invalid output", `not json`, audioStream{}, "", "reading ffprobe output"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, timeBase, err := parseStreamHeader([]byte(tt.data))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil || got != tt.want || timeBase != tt.wantTimeBase {
				t.Fatalf("got %+v %q, %v; want %+v %q", got, timeBase, err, tt.want, tt.wantTimeBase)
			}
		})
	}
}

// ffprobe prints packet fields in its own order and adds a trailing separator
// when a packet carries side data, so fields are read by key.
func TestPacketBitrate(t *testing.T) {
	tests := []struct {
		name            string
		data            string
		timeBase        string
		fallbackSeconds float64
		want            int
	}{
		{"packet durations", "duration=960|size=400|\nduration=960|size=400\n", "1/48000", 0, 160000},
		{"fields in either order", "size=400|duration=960\n", "1/48000", 0, 160000},
		{"no trailing newline", "duration=960|size=400", "1/48000", 0, 160000},
		{"missing durations use the stream duration", "size=1000\nduration=2400|size=1000\n", "1/48000", 0.1, 160000},
		{"unknown durations use the stream duration", "duration=N/A|size=2000\n", "1/48000", 0.1, 160000},
		{"unreadable time base uses the stream duration", "duration=960|size=2000\n", "0/0", 0.1, 160000},
		{"missing time base uses the stream duration", "duration=960|size=2000\n", "", 0.1, 160000},
		{"no duration leaves the bitrate unmeasured", "size=2000\n", "1/48000", 0, 0},
		{"no packets leave the bitrate unmeasured", "", "1/48000", 10, 0},
		{"unreadable sizes count nothing", "duration=960|size=N/A\n", "1/48000", 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := packetBitrate([]byte(tt.data), tt.timeBase, tt.fallbackSeconds); got != tt.want {
				t.Fatalf("packetBitrate = %d, want %d", got, tt.want)
			}
		})
	}
}

// Listing packets reads the whole file, so only codecs whose verdict needs
// their measured bitrate pay for it. Other codecs report the bitrate their
// container states.
func TestProbeAudioStreamListsPacketsOnlyForCodecsWithAFloor(t *testing.T) {
	const opusHeader = `{"streams":[{"codec_name":"opus","sample_rate":"48000","time_base":"1/48000","bit_rate":"N/A"}]}`
	tests := []struct {
		name, header string
		want         audioStream
		wantScan     bool
	}{
		{"mp3", `{"streams":[{"codec_name":"mp3","sample_rate":"44100","time_base":"1/14112000","bit_rate":"320000"}]}`, audioStream{Codec: "mp3", SampleRate: 44100, BitrateBps: 320000}, false},
		{"aac", `{"streams":[{"codec_name":"aac","sample_rate":"44100","time_base":"1/44100","bit_rate":"256297"}]}`, audioStream{Codec: "aac", SampleRate: 44100, BitrateBps: 256297}, false},
		{"flac", `{"streams":[{"codec_name":"flac","sample_rate":"44100","time_base":"1/44100","bit_rate":"N/A"}]}`, audioStream{Codec: "flac", SampleRate: 44100}, false},
		{"opus", opusHeader, audioStream{Codec: "opus", SampleRate: 48000, BitrateBps: 160000}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls [][]string
			run := func(ctx context.Context, name string, args ...string) ([]byte, error) {
				if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > probeTimeout {
					t.Errorf("%s runs without the probe's own %v deadline", name, probeTimeout)
				}
				calls = append(calls, args)
				if slices.Contains(args, "packet=size,duration") {
					if !slices.Contains(args, "compact=p=0") {
						t.Errorf("packet listing %q is not in the compact format", args)
					}
					return []byte("duration=960|size=400|\nduration=960|size=400\n"), nil
				}
				return []byte(tt.header), nil
			}
			got, err := probeAudioStream(context.Background(), run, "track", 300)
			if err != nil || got != tt.want {
				t.Fatalf("got %+v, %v; want %+v", got, err, tt.want)
			}
			if scans := len(calls) == 2; scans != tt.wantScan || len(calls) > 2 {
				t.Fatalf("ffprobe calls %q, want a packet listing %t", calls, tt.wantScan)
			}
			if strings.Contains(strings.Join(calls[0], " "), "packet") {
				t.Errorf("the header probe lists packets: %q", calls[0])
			}
		})
	}

	t.Run("failed packet listing", func(t *testing.T) {
		run := func(_ context.Context, _ string, args ...string) ([]byte, error) {
			if slices.Contains(args, "packet=size,duration") {
				return nil, errors.New("signal: terminated")
			}
			return []byte(opusHeader), nil
		}
		if _, err := probeAudioStream(context.Background(), run, "track.opus", 300); err == nil || !strings.Contains(err.Error(), "measuring the average bitrate of track.opus") {
			t.Fatalf("error = %v, want the packet listing failure", err)
		}
	})
}

// The probe runs on its own deadline, so a slow packet listing cannot spend
// the time the decode needs.
func TestAnalyzePCMAudioDecodesOnItsOwnDeadline(t *testing.T) {
	const probeDelay = 50 * time.Millisecond
	var decodeBudget time.Duration
	run := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "ffprobe" {
			time.Sleep(probeDelay)
			if slices.Contains(args, "packet=size,duration") {
				return []byte("duration=960|size=400\n"), nil
			}
			return []byte(`{"streams":[{"codec_name":"opus","sample_rate":"48000","time_base":"1/48000"}]}`), nil
		}
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("the decode has no deadline")
		}
		decodeBudget = time.Until(deadline)
		return make([]byte, 4*48000), nil
	}
	report, err := analyzePCMAudio(context.Background(), run, "track.opus", 300, DefaultMinBandwidthHz)
	if err != nil {
		t.Fatal(err)
	}
	if decodeBudget < analysisTimeout-probeDelay/2 {
		t.Errorf("decode budget %v, want the full %v after probing", decodeBudget, analysisTimeout)
	}
	if report.Codec != "opus" || report.BitrateBps != 160000 {
		t.Errorf("report %+v, want the probed Opus stream at 160000 bps", report)
	}
}
