package pipeline

import (
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

	os.Setenv("AGENT", "0")
	if IsAgentMode() {
		t.Errorf("expected IsAgentMode() to be false when AGENT=0")
	}

	os.Unsetenv("AGENT")
	if IsAgentMode() {
		t.Errorf("expected IsAgentMode() to be false when AGENT is unset")
	}
}
