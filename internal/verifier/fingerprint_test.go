package verifier

import (
	"context"
	"testing"
)

func TestVerifyAcousticFingerprint(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name          string
		dryRunOutput  string
		targetArtist  string
		targetTitle   string
		wantConfirmed bool
		wantMismatch  bool
	}{
		{
			name: "exact shazam match",
			dryRunOutput: "title: The Way I Like It\nartist: Discip\nsource: Shazam API\n",
			targetArtist: "Discip",
			targetTitle:  "The Way I Like It",
			wantConfirmed: true,
			wantMismatch:  false,
		},
		{
			name: "shazam match with remix variation",
			dryRunOutput: "title: One More (Solomun Remix)\nartist: Max Styler & Ad-Apt\nsource: Shazam API\n",
			targetArtist: "Max Styler",
			targetTitle:  "One More (Solomun Extended Remix)",
			wantConfirmed: true,
			wantMismatch:  false,
		},
		{
			name: "shazam mismatch with completely different track",
			dryRunOutput: "title: Never Gonna Give You Up\nartist: Rick Astley\nsource: Shazam API\n",
			targetArtist: "Boris Brejcha",
			targetTitle:  "Space X",
			wantConfirmed: false,
			wantMismatch:  true,
		},
		{
			name: "filename fallback unverified",
			dryRunOutput: "title: Space X\nartist: Boris Brejcha\nsource: Filename Fallback\n",
			targetArtist: "Boris Brejcha",
			targetTitle:  "Space X",
			wantConfirmed: false,
			wantMismatch:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
				return []byte(tt.dryRunOutput), nil
			}

			res, err := VerifyAcousticFingerprint(ctx, runner, "test.m4a", tt.targetArtist, tt.targetTitle)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.Confirmed != tt.wantConfirmed {
				t.Errorf("Confirmed = %v, want %v", res.Confirmed, tt.wantConfirmed)
			}
			if res.Mismatch != tt.wantMismatch {
				t.Errorf("Mismatch = %v, want %v", res.Mismatch, tt.wantMismatch)
			}
		})
	}
}
