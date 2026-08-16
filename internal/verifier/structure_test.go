package verifier

import (
	"testing"
)

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		seconds float64
		want    string
	}{
		{0, "0:00"},
		{65, "1:05"},
		{303, "5:03"},
		{503.7, "8:23"},
	}

	for _, tt := range tests {
		got := FormatDuration(tt.seconds)
		if got != tt.want {
			t.Errorf("FormatDuration(%v) = %q, want %q", tt.seconds, got, tt.want)
		}
	}
}

func TestAnalyzeMixStructure(t *testing.T) {
	tests := []struct {
		name                   string
		title                  string
		durationSec            float64
		wantRadioEditWarning   bool
		wantExtendedMix        bool
		wantMixTypeDescription string
	}{
		{
			name:                   "Extended mix long track",
			title:                  "Boris Brejcha - Space X (Extended Mix)",
			durationSec:            503,
			wantRadioEditWarning:   false,
			wantExtendedMix:        true,
			wantMixTypeDescription: "Original / Extended DJ Mix",
		},
		{
			name:                   "Radio edit keyword short track",
			title:                  "Boris Brejcha - Space X (Radio Edit)",
			durationSec:            200,
			wantRadioEditWarning:   true,
			wantExtendedMix:        false,
			wantMixTypeDescription: "Radio Edit (Short)",
		},
		{
			name:                   "Short track without extended keyword",
			title:                  "Boris Brejcha - Space X",
			durationSec:            120,
			wantRadioEditWarning:   true,
			wantExtendedMix:        false,
			wantMixTypeDescription: "Short Snippet (< 2.5 mins)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AnalyzeMixStructure(tt.title, tt.durationSec)
			if got.IsRadioEditWarning != tt.wantRadioEditWarning {
				t.Errorf("IsRadioEditWarning = %v, want %v", got.IsRadioEditWarning, tt.wantRadioEditWarning)
			}
			if got.IsOriginalOrExtendedMix != tt.wantExtendedMix {
				t.Errorf("IsOriginalOrExtendedMix = %v, want %v", got.IsOriginalOrExtendedMix, tt.wantExtendedMix)
			}
			if got.MixTypeDescription != tt.wantMixTypeDescription {
				t.Errorf("MixTypeDescription = %q, want %q", got.MixTypeDescription, tt.wantMixTypeDescription)
			}
		})
	}
}
