package ui

import (
	"errors"
	"testing"

	"github.com/charmbracelet/huh"
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

func TestPromptCandidateSelectionWithRunner_Success(t *testing.T) {
	cands := []downloader.Candidate{
		{ID: "1", Title: "Boris Brejcha - Space X (Extended Mix)", Duration: 503, Source: "soundcloud", Score: 150},
		{ID: "2", Title: "Boris Brejcha - Space X (Radio Edit)", Duration: 195, Source: "youtube", Score: 80},
	}

	mockRunner := func(form *huh.Form) error {
		return nil
	}

	chosen, err := PromptCandidateSelectionWithRunner(cands, &cands[0], mockRunner)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chosen == nil || chosen.ID != "1" {
		t.Errorf("expected candidate 1, got %+v", chosen)
	}

	// Test with nil current
	chosen, err = PromptCandidateSelectionWithRunner(cands, nil, mockRunner)
	if err != nil {
		t.Fatalf("unexpected error with nil current: %v", err)
	}
	if chosen == nil {
		t.Error("expected non-nil chosen candidate")
	}
}

func TestPromptCandidateSelectionWithRunner_Cancel(t *testing.T) {
	cands := []downloader.Candidate{
		{ID: "1", Title: "Boris Brejcha - Space X (Extended Mix)", Duration: 503, Source: "soundcloud", Score: 150},
	}

	mockRunner := func(form *huh.Form) error {
		return errors.New("user aborted")
	}

	_, err := PromptCandidateSelectionWithRunner(cands, &cands[0], mockRunner)
	if err == nil {
		t.Fatal("expected error on user abort, got nil")
	}
}

func TestPromptCandidateSelection_NonTTY(t *testing.T) {
	cands := []downloader.Candidate{
		{ID: "1", Title: "Boris Brejcha - Space X (Extended Mix)", Duration: 503, Source: "soundcloud", Score: 150},
		{ID: "2", Title: "Boris Brejcha - Space X (Radio Edit)", Duration: 195, Source: "youtube", Score: 80},
	}

	// In non-interactive test runner, form.Run() will fail gracefully or return error
	_, _ = PromptCandidateSelection(cands, &cands[0])
}
