package ui

import (
	"fmt"
	"sort"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/dj/fetch-track-cli/internal/downloader"
	"github.com/dj/fetch-track-cli/internal/verifier"
)

var (
	selectedStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("86"))
)

// FormRunner abstracts executing the interactive terminal form for testability.
type FormRunner func(form *huh.Form) error

func defaultFormRunner(form *huh.Form) error {
	return form.Run()
}

// PromptCandidateSelection displays an interactive Lip Gloss & Huh terminal prompt allowing the user to approve or change the selected candidate.
func PromptCandidateSelection(candidates []downloader.Candidate, current *downloader.Candidate) (*downloader.Candidate, error) {
	return PromptCandidateSelectionWithRunner(candidates, current, defaultFormRunner)
}

// PromptCandidateSelectionWithRunner displays the prompt using a provided FormRunner.
func PromptCandidateSelectionWithRunner(candidates []downloader.Candidate, current *downloader.Candidate, runner FormRunner) (*downloader.Candidate, error) {
	if len(candidates) == 0 {
		return nil, downloader.ErrNoCandidateFound
	}

	if runner == nil {
		runner = defaultFormRunner
	}

	// Sort candidates by score highest first
	sortedCands := make([]downloader.Candidate, len(candidates))
	copy(sortedCands, candidates)
	sort.SliceStable(sortedCands, func(i, j int) bool {
		return sortedCands[i].Score > sortedCands[j].Score
	})

	defaultIdx := 0
	if current != nil {
		for i, c := range sortedCands {
			if c.WebpageURL == current.WebpageURL || (c.ID != "" && c.ID == current.ID) {
				defaultIdx = i
				break
			}
		}
	}

	var selectedIdx int = defaultIdx

	options := make([]huh.Option[int], 0, len(sortedCands))
	for i, c := range sortedCands {
		durStr := verifier.FormatDuration(c.Duration)
		label := fmt.Sprintf("%s [%s %s score=%d]", c.Title, c.Source, durStr, c.Score)
		if i == defaultIdx {
			label += " (Auto-Selected)"
		}
		options = append(options, huh.NewOption(label, i))
	}

	// Line break above selector
	fmt.Println()

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[int]().
				Title("Choose Track Candidate:").
				Description("↑/↓ navigate • enter submit • ctrl+c cancel").
				Options(options...).
				Value(&selectedIdx),
		),
	).WithShowHelp(false)

	err := runner(form)
	if err != nil {
		return nil, fmt.Errorf("candidate selection canceled: %w", err)
	}

	chosen := &sortedCands[selectedIdx]
	fmt.Println(selectedStyle.Render(fmt.Sprintf("Approved: %q [%s]", chosen.Title, chosen.Source)))
	return chosen, nil
}
