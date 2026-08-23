package progress_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/dj/fetch-track-cli/internal/progress"
)

func shortSocketPath(t *testing.T, prefix string) string {
	t.Helper()
	p := filepath.Join(os.TempDir(), fmt.Sprintf("%s_%d.sock", prefix, time.Now().UnixNano()))
	t.Cleanup(func() {
		_ = os.Remove(p)
	})
	return p
}

func TestNewReporter_UnixSocket(t *testing.T) {
	sockPath := shortSocketPath(t, "tp")

	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("failed to listen on unix socket: %v", err)
	}
	defer listener.Close()

	var receivedEvents []progress.Event
	var mu sync.Mutex
	serverDone := make(chan struct{})

	go func() {
		defer close(serverDone)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		scanner := bufio.NewScanner(conn)
		for scanner.Scan() {
			var ev progress.Event
			if err := json.Unmarshal(scanner.Bytes(), &ev); err == nil {
				mu.Lock()
				receivedEvents = append(receivedEvents, ev)
				mu.Unlock()
			}
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	reporter, err := progress.NewReporter(ctx, "unix://"+sockPath)
	if err != nil {
		t.Fatalf("NewReporter failed: %v", err)
	}

	testEvent := progress.Event{
		Type:       progress.EventPhaseStart,
		Phase:      "search",
		Step:       1,
		TotalSteps: 5,
		Message:    "searching sources",
	}

	if err := reporter.Emit(testEvent); err != nil {
		t.Fatalf("Emit failed: %v", err)
	}

	candEvent := progress.Event{
		Type:  progress.EventCandidateFound,
		Phase: "search",
		Candidate: &progress.CandidateInfo{
			ID:       "test_1",
			Title:    "Artist - Track (Extended Mix)",
			Source:   "youtube",
			Duration: 420.0,
			Score:    100,
		},
	}
	if err := reporter.Emit(candEvent); err != nil {
		t.Fatalf("Emit candidate failed: %v", err)
	}

	if err := reporter.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	select {
	case <-serverDone:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for server to read all events")
	}

	mu.Lock()
	defer mu.Unlock()

	if len(receivedEvents) != 2 {
		t.Fatalf("expected 2 events, got %d", len(receivedEvents))
	}

	if receivedEvents[0].Type != progress.EventPhaseStart || receivedEvents[0].Phase != "search" {
		t.Errorf("unexpected event 0: %+v", receivedEvents[0])
	}
	if receivedEvents[1].Type != progress.EventCandidateFound || receivedEvents[1].Candidate.Title != "Artist - Track (Extended Mix)" {
		t.Errorf("unexpected event 1: %+v", receivedEvents[1])
	}
}

func TestNewReporter_TCPSocket(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen on tcp: %v", err)
	}
	defer listener.Close()

	var receivedEvents []progress.Event
	var mu sync.Mutex
	serverDone := make(chan struct{})

	go func() {
		defer close(serverDone)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		scanner := bufio.NewScanner(conn)
		for scanner.Scan() {
			var ev progress.Event
			if err := json.Unmarshal(scanner.Bytes(), &ev); err == nil {
				mu.Lock()
				receivedEvents = append(receivedEvents, ev)
				mu.Unlock()
			}
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	addr := fmt.Sprintf("tcp://%s", listener.Addr().String())
	reporter, err := progress.NewReporter(ctx, addr)
	if err != nil {
		t.Fatalf("NewReporter failed: %v", err)
	}

	if err := reporter.Emit(progress.Event{
		Type:    progress.EventComplete,
		Message: "all done",
		Result: &progress.ResultInfo{
			Path:  "tracks/test.m4a",
			Title: "Test",
		},
	}); err != nil {
		t.Fatalf("Emit failed: %v", err)
	}

	if err := reporter.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	select {
	case <-serverDone:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for server")
	}

	mu.Lock()
	defer mu.Unlock()

	if len(receivedEvents) != 1 {
		t.Fatalf("expected 1 event, got %d", len(receivedEvents))
	}
	if receivedEvents[0].Type != progress.EventComplete || receivedEvents[0].Result.Path != "tracks/test.m4a" {
		t.Errorf("unexpected event: %+v", receivedEvents[0])
	}
}

func TestNewReporter_FD(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	defer r.Close()

	ctx := context.Background()
	target := fmt.Sprintf("fd://%d", w.Fd())
	reporter, err := progress.NewReporter(ctx, target)
	if err != nil {
		w.Close()
		t.Fatalf("NewReporter with fd failed: %v", err)
	}

	ev := progress.Event{
		Type:    progress.EventProgress,
		Message: "testing fd output",
	}
	if err := reporter.Emit(ev); err != nil {
		t.Fatalf("Emit failed: %v", err)
	}
	_ = reporter.Close()

	scanner := bufio.NewScanner(r)
	if !scanner.Scan() {
		t.Fatal("expected line in pipe reader")
	}
	var received progress.Event
	if err := json.Unmarshal(scanner.Bytes(), &received); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if received.Message != "testing fd output" {
		t.Errorf("expected message 'testing fd output', got %q", received.Message)
	}
}

func TestNewReporter_InvalidTarget(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name   string
		target string
	}{
		{"empty_target", ""},
		{"unsupported_scheme", "ftp://localhost:21"},
		{"invalid_fd", "fd://notanumber"},
		{"unreachable_unix", "unix:///nonexistent/path/here.sock"},
		{"unreachable_tcp", "tcp://127.0.0.1:1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := progress.NewReporter(ctx, tt.target)
			if err == nil && r != nil {
				_ = r.Close()
				t.Fatalf("expected error for target %q, got nil", tt.target)
			}
		})
	}
}

func TestReporter_ConcurrentEmits(t *testing.T) {
	sockPath := shortSocketPath(t, "tc")

	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer listener.Close()

	var count int
	var mu sync.Mutex
	done := make(chan struct{})

	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		scanner := bufio.NewScanner(conn)
		for scanner.Scan() {
			var ev progress.Event
			if err := json.Unmarshal(scanner.Bytes(), &ev); err == nil {
				mu.Lock()
				count++
				mu.Unlock()
			}
		}
	}()

	ctx := context.Background()
	reporter, err := progress.NewReporter(ctx, sockPath)
	if err != nil {
		t.Fatalf("NewReporter failed: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_ = reporter.Emit(progress.Event{
				Type:    progress.EventProgress,
				Message: fmt.Sprintf("progress item %d", idx),
				Percent: float64(idx * 5),
			})
		}(i)
	}
	wg.Wait()
	_ = reporter.Close()

	<-done

	mu.Lock()
	defer mu.Unlock()
	if count != 20 {
		t.Errorf("expected 20 events received concurrently, got %d", count)
	}
}

func TestReporter_NilSafe(t *testing.T) {
	var r *progress.Reporter
	if err := r.Emit(progress.Event{Type: progress.EventPhaseStart}); err != nil {
		t.Errorf("nil reporter Emit returned error: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Errorf("nil reporter Close returned error: %v", err)
	}
}

func TestNewReporter_StdoutStderr(t *testing.T) {
	ctx := context.Background()
	rStdout, err := progress.NewReporter(ctx, "stdout")
	if err != nil {
		t.Fatalf("stdout reporter failed: %v", err)
	}
	if err := rStdout.Emit(progress.Event{Type: progress.EventPhaseStart}); err != nil {
		t.Fatalf("stdout emit failed: %v", err)
	}
	_ = rStdout.Close()

	rStderr, err := progress.NewReporter(ctx, "stderr")
	if err != nil {
		t.Fatalf("stderr reporter failed: %v", err)
	}
	if err := rStderr.Emit(progress.Event{Type: progress.EventPhaseStart}); err != nil {
		t.Fatalf("stderr emit failed: %v", err)
	}
	_ = rStderr.Close()
}

func TestResolveTargetURI(t *testing.T) {
	tests := []struct {
		input      string
		wantScheme string
	}{
		{"unix:///tmp/test.sock", "unix"},
		{"/tmp/test.sock", "unix"},
		{"./test.sock", "unix"},
		{"tcp://127.0.0.1:9000", "tcp"},
		{"127.0.0.1:9000", "tcp"},
		{"localhost:9000", "tcp"},
		{"fd://3", "fd"},
		{"stdout", "stdout"},
		{"stderr", "stderr"},
		{"custom://localhost:9000", "custom"},
		{"relpath/test.sock", "unix"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			scheme, _, err := progress.ParseTarget(tt.input)
			if err != nil {
				t.Fatalf("ParseTarget(%q) error = %v", tt.input, err)
			}
			if scheme != tt.wantScheme {
				t.Errorf("ParseTarget(%q) scheme = %q, want %q", tt.input, scheme, tt.wantScheme)
			}
		})
	}
}
