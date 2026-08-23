package spinner

import (
	"testing"
	"time"
)

func TestSpinnerLifecycle(t *testing.T) {
	sp := New("Testing spinner...")
	// PrintAbove while inactive
	sp.PrintAbove("Inactive message")

	// Stop while inactive
	sp.Stop()

	sp.Start()
	// Give goroutine a moment to start
	for i := 0; i < 50 && !sp.sp.Active(); i++ {
		time.Sleep(5 * time.Millisecond)
	}

	// Calling Start when already active
	sp.Start()

	sp.PrintAbove("Candidate found above spinner")
	sp.Update("Updating message...")

	sp.Stop()
	// Idempotent stop call
	sp.Stop()
}

func TestSpinnerNilSafe(t *testing.T) {
	var nilSpinner *Spinner
	nilSpinner.Start()
	nilSpinner.Update("test")
	nilSpinner.PrintAbove("test")
	nilSpinner.Stop()

	emptySpinner := &Spinner{}
	emptySpinner.Start()
	emptySpinner.Update("test")
	emptySpinner.PrintAbove("test")
	emptySpinner.Stop()
}
