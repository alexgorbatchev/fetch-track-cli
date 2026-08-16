package spinner

import (
	"fmt"
	"os"
	"sync"
	"time"
)

func isTTY() bool {
	fileInfo, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fileInfo.Mode() & os.ModeCharDevice) != 0
}

var brailleFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Spinner provides a lightweight, thread-safe braille terminal spinner.
type Spinner struct {
	mu     sync.Mutex
	msg    string
	active bool
	stopCh chan struct{}
	done   chan struct{}
}

// New creates a new Spinner with the specified initial message.
func New(msg string) *Spinner {
	return &Spinner{
		msg: msg,
	}
}

// Start begins animating the braille spinner on stdout.
func (s *Spinner) Start() {
	s.mu.Lock()
	if s.active {
		s.mu.Unlock()
		return
	}
	s.active = true
	s.stopCh = make(chan struct{})
	s.done = make(chan struct{})
	s.mu.Unlock()

	go func() {
		defer close(s.done)
		if !isTTY() {
			return
		}
		ticker := time.NewTicker(80 * time.Millisecond)
		defer ticker.Stop()

		i := 0
		for {
			select {
			case <-s.stopCh:
				fmt.Print("\r\033[K")
				return
			case <-ticker.C:
				s.mu.Lock()
				m := s.msg
				s.mu.Unlock()
				frame := brailleFrames[i%len(brailleFrames)]
				fmt.Printf("\r%s %s", frame, m)
				i++
			}
		}
	}()
}

// Update changes the message displayed next to the spinner.
func (s *Spinner) Update(msg string) {
	s.mu.Lock()
	s.msg = msg
	s.mu.Unlock()
}

// Stop stops the spinner animation and clears the line.
func (s *Spinner) Stop() {
	s.mu.Lock()
	if !s.active {
		s.mu.Unlock()
		return
	}
	s.active = false
	close(s.stopCh)
	s.mu.Unlock()

	<-s.done
}
