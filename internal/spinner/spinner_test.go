package spinner

import (
	"testing"
	"time"
)

func TestSpinnerLifecycle(t *testing.T) {
	sp := New("Testing spinner...")
	sp.Start()
	time.Sleep(200 * time.Millisecond)

	sp.Update("Updating message...")
	time.Sleep(100 * time.Millisecond)

	sp.Stop()
	// Idempotent stop call
	sp.Stop()
}
