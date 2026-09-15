package deps

import (
	"testing"
)

func TestResolveJSRuntimeArgs(t *testing.T) {
	tests := []struct {
		name       string
		preference string
		wantPrefix string
	}{
		{"auto detection", "auto", "--js-runtimes"},
		{"disabled none", "none", ""},
		{"disabled off", "off", ""},
		{"explicit engine", "node", "--js-runtimes"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := ResolveJSRuntimeArgs(tt.preference)
			if tt.wantPrefix == "" {
				if len(args) != 0 {
					t.Errorf("expected no args for %q, got %+v", tt.preference, args)
				}
			} else {
				if len(args) > 0 && args[0] != tt.wantPrefix {
					t.Errorf("expected arg prefix %q, got %+v", tt.wantPrefix, args)
				}
			}
		})
	}
}
