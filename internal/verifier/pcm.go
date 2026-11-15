package verifier

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
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
	// 20 kHz at every music bitrate, so at this bar it also needs
	// opusMinBitrateBps.
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
	// probeTimeout bounds reading the stream header and, for codecs with a
	// bitrate floor, listing every packet, which reads the whole file. It is
	// separate from analysisTimeout so a slow listing cannot spend the
	// decode's time.
	probeTimeout = 60 * time.Second

	// opusMinBitrateBps is the average bitrate Opus needs at minimums of
	// DefaultMinBandwidthHz and above: libopus codes about 20.2 kHz at every
	// bitrate from 48 to 192 kbps, so its cutoff does not grade it.
	opusMinBitrateBps = 160000
)

// Low-quality reasons, reported in AudioQualityReport.LowQualityReasons.
const (
	// ReasonBandwidthBelowMinimum: the measured cutoff is below the minimum.
	ReasonBandwidthBelowMinimum = "bandwidth_below_minimum"
	// ReasonBitrateBelowMinimum: the codec's average bitrate is below the
	// floor it needs at this minimum, or could not be measured.
	ReasonBitrateBelowMinimum = "bitrate_below_minimum"
)

// bitrateFloor is the average bitrate a codec needs at minimums of at least
// fromMinimumHz because its measured cutoff does not reflect its quality.
type bitrateFloor struct {
	label         string
	fromMinimumHz int
	minBitrateBps int
}

// bitrateFloors lists codecs, by ffprobe codec name, whose measured cutoff does
// not grade them. Add a codec only with measured encoder evidence.
var bitrateFloors = map[string]bitrateFloor{
	"opus": {label: "Opus", fromMinimumHz: DefaultMinBandwidthHz, minBitrateBps: opusMinBitrateBps},
}

// AudioQualityReport contains frequency spectrum and dynamics analysis results.
type AudioQualityReport struct {
	// Codec is the ffprobe codec name of the analysed audio stream.
	Codec string `json:"codec"`
	// BitrateBps is the stream's average bitrate. Codecs with a bitrate floor
	// are measured from their packets, so VBR streams report their actual
	// average and cover art and container overhead are excluded; other codecs
	// report the bitrate their container states for the stream. Zero when it
	// is unknown.
	BitrateBps           int    `json:"bitrateBps"`
	EstimatedBandwidthHz int    `json:"estimatedBandwidthHz"`
	BandwidthRating      string `json:"bandwidthRating"`
	// HasLowBandwidthWarning reports a cutoff below MinBandwidthHz only;
	// MeetsMinimum also applies the codec's bitrate floor.
	HasLowBandwidthWarning bool `json:"hasLowBandwidthWarning"`
	MinBandwidthHz         int  `json:"minBandwidthHz"`
	// MinBitrateBps is the bitrate floor applied at MinBandwidthHz, or zero.
	MinBitrateBps      int      `json:"minBitrateBps,omitempty"`
	MeetsMinimum       bool     `json:"meetsMinimum"`
	LowQualityReasons  []string `json:"lowQualityReasons"`
	PeakDbFS           float64  `json:"peakDbFS"`
	RMSDbFS            float64  `json:"rmsDbFS"`
	HasClippingWarning bool     `json:"hasClippingWarning"`
	SubBassDbFS        float64  `json:"subBassDbFS"`
	KickBassDbFS       float64  `json:"kickBassDbFS"`
	SuggestedDJGainDb  float64  `json:"suggestedDjGainDb"`
}

// BandwidthKHz returns the estimated bandwidth in kHz for display; it keeps the
// estimate's 100 Hz resolution when printed with one decimal.
func (r AudioQualityReport) BandwidthKHz() float64 {
	return float64(r.EstimatedBandwidthHz) / 1000
}

// BitrateKbps returns the average bitrate in kbps, truncated to 0.1 kbps so a
// bitrate below a floor never displays as the floor.
func (r AudioQualityReport) BitrateKbps() float64 {
	return float64(r.BitrateBps/100) / 10
}

// CodecSummary names the codec and its average bitrate for display, such as
// "opus 128.0 kbps", or says the bitrate is unmeasured rather than printing
// zero.
func (r AudioQualityReport) CodecSummary() string {
	if r.BitrateBps <= 0 {
		return r.Codec + ", bitrate unmeasured"
	}
	return fmt.Sprintf("%s %.1f kbps", r.Codec, r.BitrateKbps())
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

// EvaluateQuality grades the report's codec, bitrate and bandwidth against
// minBandwidthHz; zero or less applies DefaultMinBandwidthHz. Audio meets the
// minimum when its measured cutoff reaches it and, for codecs listed in
// bitrateFloors at minimums of at least their fromMinimumHz, its average
// bitrate reaches the codec's floor. The bandwidth rating describes the cutoff
// alone.
func EvaluateQuality(report *AudioQualityReport, minBandwidthHz int) {
	if minBandwidthHz <= 0 {
		minBandwidthHz = DefaultMinBandwidthHz
	}
	report.MinBandwidthHz = minBandwidthHz
	report.HasLowBandwidthWarning = report.EstimatedBandwidthHz < minBandwidthHz

	switch {
	case report.EstimatedBandwidthHz >= DefaultMinBandwidthHz:
		report.BandwidthRating = "High Fidelity (>=19 kHz)"
	case report.EstimatedBandwidthHz >= reducedBandwidthHz:
		report.BandwidthRating = "Reduced Bandwidth (16-19 kHz)"
	default:
		report.BandwidthRating = "Low Quality / Transcoded (<16 kHz)"
	}

	report.MinBitrateBps = 0
	if floor, ok := bitrateFloors[report.Codec]; ok && minBandwidthHz >= floor.fromMinimumHz {
		report.MinBitrateBps = floor.minBitrateBps
	}

	report.LowQualityReasons = []string{}
	if report.HasLowBandwidthWarning {
		report.LowQualityReasons = append(report.LowQualityReasons, ReasonBandwidthBelowMinimum)
	}
	if report.BitrateBps < report.MinBitrateBps {
		report.LowQualityReasons = append(report.LowQualityReasons, ReasonBitrateBelowMinimum)
	}
	report.MeetsMinimum = len(report.LowQualityReasons) == 0
}

// AnalyzePCMAudio probes the first audio stream's codec and average bitrate,
// decodes an excerpt of up to 60 seconds at the source sample rate (at most
// 48 kHz), measures its bandwidth and dynamics, and grades it against the
// minimum.
func AnalyzePCMAudio(ctx context.Context, filePath string, durationSec float64, minBandwidthHz ...int) (*AudioQualityReport, error) {
	minHz := DefaultMinBandwidthHz
	if len(minBandwidthHz) > 0 && minBandwidthHz[0] > 0 {
		minHz = minBandwidthHz[0]
	}
	return analyzePCMAudio(ctx, deps.RunCommand, filePath, durationSec, minHz)
}

// analyzePCMAudio is AnalyzePCMAudio with the tool runner as a parameter. The
// probe and the decode each run on their own deadline.
func analyzePCMAudio(ctx context.Context, run CommandRunner, filePath string, durationSec float64, minHz int) (*AudioQualityReport, error) {
	stream, err := probeAudioStream(ctx, run, filePath, durationSec)
	if err != nil {
		return nil, err
	}
	sampleRate := min(stream.SampleRate, maxAnalysisRate)

	startTime := strconv.Itoa(excerptStartSeconds)
	if durationSec < shortTrackSeconds {
		startTime = "0"
	}

	cmdCtx, cancel := context.WithTimeout(ctx, analysisTimeout)
	defer cancel()
	rawBytes, err := run(cmdCtx, "ffmpeg",
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
	report.Codec = stream.Codec
	report.BitrateBps = stream.BitrateBps
	report.EstimatedBandwidthHz = estimateBandwidthHz(samples, sampleRate)
	EvaluateQuality(report, minHz)

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

// audioStream describes the first audio stream of a file.
type audioStream struct {
	Codec      string
	SampleRate int
	// BitrateBps is zero when the bitrate is unknown.
	BitrateBps int
}

// probeAudioStream reads the codec, sample rate and bitrate of the first audio
// stream in filePath, on its own probeTimeout deadline. Codecs listed in
// bitrateFloors report the average of that stream's packets, which needs a
// listing of the whole file; durationSec stands in for packet durations the
// container omits. Other codecs report the bitrate their container states,
// which only describes them, so their files are read no further than the
// header.
func probeAudioStream(ctx context.Context, run CommandRunner, filePath string, durationSec float64) (audioStream, error) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	out, err := run(ctx, "ffprobe",
		"-v", "error",
		"-select_streams", "a:0",
		"-show_entries", "stream=codec_name,sample_rate,time_base,bit_rate",
		"-of", "json",
		filePath,
	)
	if err != nil {
		return audioStream{}, fmt.Errorf("probing audio stream of %s: %w", filePath, err)
	}
	stream, timeBase, err := parseStreamHeader(out)
	if err != nil {
		return audioStream{}, fmt.Errorf("probing audio stream of %s: %w", filePath, err)
	}
	if _, ok := bitrateFloors[stream.Codec]; !ok {
		return stream, nil
	}

	// ffprobe prints packet fields in its own order whatever the request
	// order, so the compact format's key=value items are read by key.
	packets, err := run(ctx, "ffprobe",
		"-v", "error",
		"-select_streams", "a:0",
		"-show_entries", "packet=size,duration",
		"-of", "compact=p=0",
		filePath,
	)
	if err != nil {
		return audioStream{}, fmt.Errorf("measuring the average bitrate of %s: %w", filePath, err)
	}
	stream.BitrateBps = packetBitrate(packets, timeBase, durationSec)
	return stream, nil
}

// probeValue holds an ffprobe JSON value, which ffprobe prints as a number or
// a string depending on the field, and as "N/A" when it is unknown.
type probeValue string

func (v *probeValue) UnmarshalJSON(data []byte) error {
	*v = probeValue(strings.Trim(string(data), `"`))
	return nil
}

func (v probeValue) int64() (int64, bool) {
	n, err := strconv.ParseInt(string(v), 10, 64)
	return n, err == nil
}

// parseStreamHeader reads ffprobe's JSON description of the first audio stream:
// its codec, sample rate, stated bitrate (zero when the container states none)
// and time base.
func parseStreamHeader(data []byte) (audioStream, string, error) {
	var probe struct {
		Streams []struct {
			CodecName  string     `json:"codec_name"`
			SampleRate probeValue `json:"sample_rate"`
			TimeBase   string     `json:"time_base"`
			BitRate    probeValue `json:"bit_rate"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return audioStream{}, "", fmt.Errorf("reading ffprobe output: %w", err)
	}
	// Containers with programs, such as MPEG-TS, can list the stream more than once.
	if len(probe.Streams) == 0 {
		return audioStream{}, "", errors.New("no audio stream with a sample rate")
	}
	first := probe.Streams[0]
	rate, ok := first.SampleRate.int64()
	if !ok || rate <= 0 {
		return audioStream{}, "", errors.New("no audio stream with a sample rate")
	}
	stream := audioStream{Codec: first.CodecName, SampleRate: int(rate)}
	if bitrate, ok := first.BitRate.int64(); ok && bitrate > 0 {
		stream.BitrateBps = int(bitrate)
	}
	return stream, first.TimeBase, nil
}

// packetBitrate reads ffprobe's compact listing of one stream's packets, one
// "key=value|key=value" line per packet, and divides the packets' bytes by
// their summed durations, counted exactly in timeBase, when every packet has
// one, and by fallbackSeconds otherwise. It returns zero when neither gives a
// duration or no bytes were listed.
func packetBitrate(data []byte, timeBase string, fallbackSeconds float64) int {
	var totalBytes, ticks int64
	packets, timed := 0, true
	for line := range bytes.Lines(data) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		packets++
		var duration int64
		for item := range bytes.SplitSeq(line, []byte("|")) {
			key, value, _ := bytes.Cut(item, []byte("="))
			n, err := strconv.ParseInt(string(value), 10, 64)
			if err != nil {
				continue
			}
			switch string(key) {
			case "size":
				totalBytes += n
			case "duration":
				duration = n
			}
		}
		if duration <= 0 {
			timed = false
			continue
		}
		ticks += duration
	}
	seconds := fallbackSeconds
	if numerator, denominator, ok := parseTimeBase(timeBase); ok && timed && packets > 0 {
		seconds = float64(ticks) * float64(numerator) / float64(denominator)
	}
	if totalBytes <= 0 || seconds <= 0 {
		return 0
	}
	return int(math.Round(float64(totalBytes) * 8 / seconds))
}

// parseTimeBase reads an ffprobe time base such as "1/48000".
func parseTimeBase(timeBase string) (numerator, denominator int64, ok bool) {
	n, d, found := strings.Cut(timeBase, "/")
	if !found {
		return 0, 0, false
	}
	numerator, errN := strconv.ParseInt(n, 10, 64)
	denominator, errD := strconv.ParseInt(d, 10, 64)
	if errN != nil || errD != nil || numerator <= 0 || denominator <= 0 {
		return 0, 0, false
	}
	return numerator, denominator, true
}
