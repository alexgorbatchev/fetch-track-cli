package spinner

import (
	"fmt"
	"time"

	bspinner "github.com/briandowns/spinner"
)

// Spinner wraps github.com/briandowns/spinner for consistent CLI terminal progress.
type Spinner struct {
	sp     *bspinner.Spinner
	active bool
}

var brailleFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// New creates a new Spinner using exact braille frames.
func New(msg string) *Spinner {
	s := bspinner.New(brailleFrames, 80*time.Millisecond)
	s.Suffix = " " + msg
	return &Spinner{
		sp: s,
	}
}

// Start begins animating the braille spinner.
func (s *Spinner) Start() {
	if s != nil && s.sp != nil && !s.active {
		s.active = true
		s.sp.Start()
	}
}

// Update changes the suffix message displayed next to the spinner.
func (s *Spinner) Update(msg string) {
	if s != nil && s.sp != nil {
		s.sp.Suffix = " " + msg
	}
}

// PrintAbove clears the current spinner line, prints the provided message above,
// and lets the spinner redraw on the bottom line.
func (s *Spinner) PrintAbove(msg string) {
	if s != nil && s.sp != nil && s.active {
		fmt.Printf("\r\033[K%s\n", msg)
	} else {
		fmt.Println(msg)
	}
}

// Stop stops the spinner animation and clears the line.
func (s *Spinner) Stop() {
	if s != nil && s.sp != nil && s.active {
		s.active = false
		s.sp.Stop()
	}
}
