package pipeline

import (
	"context"
	"os"
	"testing"
)

func TestIsAgentMode(t *testing.T) {
	orig := os.Getenv("AGENT")
	defer os.Setenv("AGENT", orig)

	os.Setenv("AGENT", "1")
	if !IsAgentMode() {
		t.Errorf("expected IsAgentMode() to be true when AGENT=1")
	}

	os.Setenv("AGENT", "true")
	if !IsAgentMode() {
		t.Errorf("expected IsAgentMode() to be true when AGENT=true")
	}

	os.Setenv("AGENT", "0")
	if IsAgentMode() {
		t.Errorf("expected IsAgentMode() to be false when AGENT=0")
	}

	os.Unsetenv("AGENT")
	if IsAgentMode() {
		t.Errorf("expected IsAgentMode() to be false when AGENT is unset")
	}
}

func TestRun_CanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	opts := Options{
		OutDir:       t.TempDir(),
		Sources:      []string{"youtube"},
		SkipVerify:   true,
		SkipMetadata: true,
		Verbose:      true,
		IsAgent:      true,
	}

	err := Run(ctx, "Test Artist - Test Track", opts)
	if err == nil {
		t.Error("expected error when context is canceled, got nil")
	}
}
