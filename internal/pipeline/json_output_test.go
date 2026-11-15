package pipeline

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/alexgorbatchev/fetch-track-cli/internal/downloader"
	"github.com/alexgorbatchev/fetch-track-cli/internal/verifier"
)

func TestConvertVerificationReport(t *testing.T) {
	rep := &verifier.VerificationReport{
		Metadata: verifier.TrackMetadata{
			Title:           "Space X",
			DurationSeconds: 284,
		},
		Quality: verifier.AudioQualityReport{
			EstimatedBandwidthHz: 20000,
			BandwidthRating:      "High Fidelity (>=18.5 kHz)",
			PeakDbFS:             -0.5,
			RMSDbFS:              -7.2,
			SuggestedDJGainDb:    -4.8,
		},
		SummaryStatus: "High fidelity audio suitable for mixing.",
	}

	converted := ConvertVerificationReport(rep)
	if converted == nil {
		t.Fatal("expected non-nil converted JSON verification report")
	}

	if converted.BandwidthHz != 20000 {
		t.Errorf("BandwidthHz = %d, want 20000", converted.BandwidthHz)
	}
	if converted.DurationSeconds != 284 {
		t.Errorf("DurationSeconds = %f, want 284", converted.DurationSeconds)
	}
}

// The verdict travels with bandwidth_hz: codec, measured bitrate, the
// minimums applied and why the audio falls short. Existing fields keep their
// names and meanings.
func TestConvertVerificationReportCarriesTheVerdict(t *testing.T) {
	tests := []struct {
		name    string
		quality verifier.AudioQualityReport
		want    map[string]any
	}{
		{
			name: "opus below the floor",
			quality: verifier.AudioQualityReport{
				Codec: "opus", BitrateBps: 128019, EstimatedBandwidthHz: 20200, BandwidthRating: "High Fidelity (>=19 kHz)",
				MinBandwidthHz: 19000, MinBitrateBps: 160000, LowQualityReasons: []string{verifier.ReasonBitrateBelowMinimum},
			},
			want: map[string]any{
				"codec": "opus", "bitrate_bps": 128019.0, "bandwidth_hz": 20200.0, "bandwidth_rating": "High Fidelity (>=19 kHz)",
				"min_bandwidth_hz": 19000.0, "min_bitrate_bps": 160000.0, "meets_minimum": false,
				"low_quality_reasons": []any{"bitrate_below_minimum"},
			},
		},
		{
			name: "mp3 that meets the minimum",
			quality: verifier.AudioQualityReport{
				Codec: "mp3", BitrateBps: 320005, EstimatedBandwidthHz: 20100, MinBandwidthHz: 19000, MeetsMinimum: true,
			},
			want: map[string]any{
				"codec": "mp3", "bitrate_bps": 320005.0, "bandwidth_hz": 20100.0, "min_bandwidth_hz": 19000.0,
				"meets_minimum": true, "low_quality_reasons": []any{},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(ConvertVerificationReport(&verifier.VerificationReport{Quality: tt.quality, SummaryStatus: "STATUS: fixture"}))
			if err != nil {
				t.Fatal(err)
			}
			var got map[string]any
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatal(err)
			}
			for key, want := range tt.want {
				if !reflect.DeepEqual(got[key], want) {
					t.Errorf("%s = %#v, want %#v", key, got[key], want)
				}
			}
			if _, present := got["min_bitrate_bps"]; present != (tt.quality.MinBitrateBps != 0) {
				t.Errorf("min_bitrate_bps present = %t, want it only when a floor applies", present)
			}
			for _, key := range []string{"duration_seconds", "bandwidth_hz", "bandwidth_rating", "peak_dbfs", "rms_dbfs", "gain_offset_db", "status"} {
				if _, ok := got[key]; !ok {
					t.Errorf("existing field %s is missing from %s", key, data)
				}
			}
		})
	}
}

func TestJSONExecutionResult_Marshal(t *testing.T) {
	res := &JSONExecutionResult{
		Target:         "Boris Brejcha - Space X",
		Status:         "success",
		CandidateCount: 5,
		SelectedCandidate: &downloader.Candidate{
			Title:      "Space X (Extended Mix)",
			Duration:   284,
			Source:     "youtube",
			WebpageURL: "https://youtube.com/watch?v=123",
		},
		OutputFile: "tracks/Boris Brejcha - Space X.m4a",
	}

	data, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("failed to marshal JSONExecutionResult: %v", err)
	}

	var unmarshaled map[string]interface{}
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	if unmarshaled["status"] != "success" {
		t.Errorf("status = %v, want success", unmarshaled["status"])
	}
}
