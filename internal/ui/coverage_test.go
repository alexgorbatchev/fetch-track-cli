package ui

import (
	"github.com/charmbracelet/huh"
	"github.com/dj/fetch-track-cli/internal/downloader"
	"testing"
)

func TestSelectionKeepsCurrentLowerRankedCandidate(t *testing.T) {
	candidates := []downloader.Candidate{
		{ID: "high", WebpageURL: "https://example.test/high", Title: "High", Score: 100, Uploader: "Label"},
		{ID: "low", WebpageURL: "https://example.test/low", Title: "Low", Score: 50},
	}
	chosen, err := PromptCandidateSelectionWithRunner(candidates, &candidates[1], func(*huh.Form) error { return nil })
	if err != nil || chosen == nil || chosen.ID != "low" {
		t.Fatalf("chosen = %+v, error = %v", chosen, err)
	}
}
