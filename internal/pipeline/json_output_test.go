package pipeline

import (
	"encoding/json"
	"testing"

	"github.com/dj/fetch-track-cli/internal/downloader"
	"github.com/dj/fetch-track-cli/internal/verifier"
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
