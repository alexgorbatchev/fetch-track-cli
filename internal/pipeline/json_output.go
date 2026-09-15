package pipeline

import (
	"encoding/json"
	"os"

	"github.com/dj/fetch-track-cli/internal/downloader"
	"github.com/dj/fetch-track-cli/internal/verifier"
)

// JSONExecutionResult represents the machine-readable output when --json is enabled.
type JSONExecutionResult struct {
	Target            string                `json:"target"`
	Status            string                `json:"status"` // "success", "skipped", "error", "quality_warning"
	Error             string                `json:"error,omitempty"`
	CandidateCount    int                   `json:"candidate_count,omitempty"`
	SelectedCandidate *downloader.Candidate `json:"selected_candidate,omitempty"`
	OutputFile        string                `json:"output_file,omitempty"`
	Verification      *JSONVerification     `json:"verification,omitempty"`
	Metadata          *JSONMetadata         `json:"metadata,omitempty"`
}

type JSONVerification struct {
	DurationSeconds float64 `json:"duration_seconds"`
	BandwidthHz     int     `json:"bandwidth_hz"`
	BandwidthRating string  `json:"bandwidth_rating"`
	PeakDbFS        float64 `json:"peak_dbfs"`
	RMSDbFS         float64 `json:"rms_dbfs"`
	GainOffsetDb    float64 `json:"gain_offset_db"`
	Status          string  `json:"status"`
}

type JSONMetadata struct {
	Title                string `json:"title,omitempty"`
	Artist               string `json:"artist,omitempty"`
	Album                string `json:"album,omitempty"`
	Year                 string `json:"year,omitempty"`
	Source               string `json:"source,omitempty"`
	CoverArtURL          string `json:"cover_art_url,omitempty"`
	FingerprintConfirmed bool   `json:"fingerprint_confirmed,omitempty"`
}

// RenderJSONOutput prints the structured JSON execution result to stdout.
func RenderJSONOutput(result *JSONExecutionResult) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}

// ConvertVerificationReport converts internal report to JSON report.
func ConvertVerificationReport(rep *verifier.VerificationReport) *JSONVerification {
	if rep == nil {
		return nil
	}
	return &JSONVerification{
		DurationSeconds: rep.Metadata.DurationSeconds,
		BandwidthHz:     rep.Quality.EstimatedBandwidthHz,
		BandwidthRating: rep.Quality.BandwidthRating,
		PeakDbFS:        rep.Quality.PeakDbFS,
		RMSDbFS:         rep.Quality.RMSDbFS,
		GainOffsetDb:    rep.Quality.SuggestedDJGainDb,
		Status:          rep.SummaryStatus,
	}
}
