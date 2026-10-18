package verifier

import (
	"context"
	"errors"
	"testing"
)

func TestFingerprintMatchingVariations(t *testing.T) {
	tests := []struct {
		name, artist, title, matchedArtist, matchedTitle string
		confirmed                                        bool
	}{
		{"diacritics", "Beyoncé", "Été (Radio Edit)", "Beyonce", "Ete", true},
		{"title tokens", "Artist", "Night / Fever", "Artist", "Fever Dance", true},
		{"artist tokens", "Alpha Beta", "Night", "Beta Gamma", "Night", true},
		{"missing artist", "", "Night", "Unknown", "Night", true},
		{"fuzzy title", "Artist", "Ngt", "Artist", "Night", true},
		{"artist conflict", "Alpha Beta", "Night", "Gamma Delta", "Night", false},
		{"empty title", "Artist", "", "Artist", "Night", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := func(context.Context, string, ...string) ([]byte, error) {
				return []byte("title: " + tt.matchedTitle + "\nartist: " + tt.matchedArtist + "\nsource: AcoustID\n"), nil
			}
			result, err := VerifyAcousticFingerprint(context.Background(), runner, "synthetic.m4a", tt.artist, tt.title)
			if err != nil || result.Confirmed != tt.confirmed || result.Mismatch == tt.confirmed {
				t.Fatalf("result = %+v, error = %v", result, err)
			}
		})
	}
}

func TestFingerprintDefaultRunnerFailure(t *testing.T) {
	want := errors.New("recognizer unavailable")
	restore := SetDefaultRunner(func(context.Context, string, ...string) ([]byte, error) { return nil, want })
	defer restore()
	result, err := VerifyAcousticFingerprint(context.Background(), nil, "synthetic.m4a", "Artist", "Title")
	if err != nil || result.Confirmed || result.Mismatch || result.Reason != "tag-track execution failed: "+want.Error() {
		t.Fatalf("result = %+v, error = %v", result, err)
	}
}

func TestBandwidthClassificationThresholds(t *testing.T) {
	tests := []struct {
		bandwidth, threshold int
		warning              bool
		rating               string
	}{
		{20000, 0, false, "High Fidelity (>=18.5 kHz)"},
		{17000, 18000, true, "Standard YouTube (16-18.5 kHz)"},
		{14000, 16000, true, "Low Quality / Transcoded (<16 kHz)"},
	}
	for _, tt := range tests {
		report := AudioQualityReport{EstimatedBandwidthHz: tt.bandwidth}
		EvaluateBandwidthWarning(&report, tt.threshold)
		if report.HasLowBandwidthWarning != tt.warning || report.BandwidthRating != tt.rating {
			t.Fatalf("report = %+v", report)
		}
	}
}
