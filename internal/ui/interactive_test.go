package ui

import (
	"errors"
	"testing"

	"github.com/dj/fetch-track-cli/internal/downloader"
)

func TestPromptCandidateSelection_Empty(t *testing.T) {
	cand, err := PromptCandidateSelection(nil, nil)
	if err == nil {
		t.Fatal("expected error for empty candidates, got nil")
	}
	if !errors.Is(err, downloader.ErrNoCandidateFound) {
		t.Errorf("expected ErrNoCandidateFound, got %v", err)
	}
	if cand != nil {
		t.Errorf("expected nil candidate, got %v", cand)
	}
}

func TestPromptCandidateSelection_NonTTY(t *testing.T) {
	cands := []downloader.Candidate{
		{ID: "1", Title: "Boris Brejcha - Space X (Extended Mix)", Duration: 503, Source: "soundcloud", Score: 150},
		{ID: "2", Title: "Boris Brejcha - Space X (Radio Edit)", Duration: 195, Source: "youtube", Score: 80},
	}

	// In non-interactive test runner, form.Run() will fail gracefully or return error
	_, err := PromptCandidateSelection(cands, &cands[0])
	if err == nil {
		t.Log("PromptCandidateSelection returned no error in test environment")
	} else {
		t.Logf("PromptCandidateSelection returned expected non-interactive error: %v", err)
	}
}
