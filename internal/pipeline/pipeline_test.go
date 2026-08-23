package pipeline

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

	"github.com/dj/fetch-track-cli/internal/downloader"
	"github.com/dj/fetch-track-cli/internal/progress"
)

func TestIsAgentMode(t *testing.T) {
	orig := os.Getenv("AGENT")
	defer os.Setenv("AGENT", orig)

	os.Setenv("AGENT", "1")
	if !IsAgentMode() {
		t.Errorf("expected IsAgentMode() to be true when AGENT=1")
	}

	os.Setenv("AGENT", "true")
	if !IsAgentMode() {
		t.Errorf("expected IsAgentMode() to be true when AGENT=true")
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

func TestRun_CanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	opts := Options{
		OutDir:       t.TempDir(),
		Sources:      []string{"youtube"},
		SkipVerify:   true,
		SkipMetadata: true,
		SkipDepCheck: true,
		Verbose:      true,
		IsAgent:      true,
	}

	err := Run(ctx, "Test Artist - Test Track", opts)
	if err == nil {
		t.Error("expected error when context is canceled, got nil")
	}
}

func TestRun_SkipDepCheck(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	opts := Options{
		OutDir:       t.TempDir(),
		Sources:      []string{"youtube"},
		SkipVerify:   true,
		SkipMetadata: true,
		SkipDepCheck: true,
		IsAgent:      true,
	}

	err := Run(ctx, "Test Artist - Test Track", opts)
	if err == nil {
		t.Error("expected error when context is canceled with SkipDepCheck")
	}
}

func TestPipeline_CandidatePoolScoring(t *testing.T) {
	cands := []downloader.Candidate{
		{ID: "1", Title: "Boris Brejcha - Space X (Extended Mix)", Duration: 503, Source: "soundcloud"},
		{ID: "2", Title: "Boris Brejcha - Space X (Radio Edit)", Duration: 210, Source: "youtube"},
	}

	candidatePool := downloader.DeduplicateCandidates(cands)

	// In pipeline.Run, RankAllCandidates must reassign candidatePool so candidate.Score > 0 is preserved
	candidatePool = downloader.RankAllCandidates(candidatePool, "Boris Brejcha", "Space X")

	for _, c := range candidatePool {
		if c.Score == 0 {
			t.Fatalf("candidate %q has Score = 0; expected non-zero score after ranking", c.Title)
		}
	}
}

func TestPipeline_RunCandidatePoolRanking(t *testing.T) {
	cands := []downloader.Candidate{
		{ID: "1", Title: "Boris Brejcha - Space X (Extended Mix)", Duration: 503, Source: "soundcloud"},
		{ID: "2", Title: "Boris Brejcha - Space X (Radio Edit)", Duration: 210, Source: "youtube"},
	}

	pool := downloader.DeduplicateCandidates(cands)

	// Without reassignment (the bug before fix): candidate.Score remains 0 on the slice header
	unassignedPool := make([]downloader.Candidate, len(pool))
	copy(unassignedPool, pool)
	_ = downloader.RankAllCandidates(unassignedPool, "Boris Brejcha", "Space X")

	if unassignedPool[0].Score != 0 {
		t.Fatalf("expected unassigned pool element to have Score = 0, got %d", unassignedPool[0].Score)
	}

	// With reassignment (the fix): candidate.Score is correctly updated in candidatePool
	assignedPool := downloader.RankAllCandidates(pool, "Boris Brejcha", "Space X")

	if assignedPool[0].Score == 0 {
		t.Fatalf("expected assigned pool element to have non-zero Score, got 0")
	}
}

func TestRun_WithProgressReporter(t *testing.T) {
	sockPath := filepath.Join(os.TempDir(), fmt.Sprintf("pipe_prog_%d.sock", time.Now().UnixNano()))
	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("failed to listen on socket: %v", err)
	}
	defer func() {
		listener.Close()
		_ = os.Remove(sockPath)
	}()

	var receivedEvents []progress.Event
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
				receivedEvents = append(receivedEvents, ev)
				mu.Unlock()
			}
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // canceled context

	reporter, err := progress.NewReporter(context.Background(), sockPath)
	if err != nil {
		t.Fatalf("failed to create progress reporter: %v", err)
	}
	defer reporter.Close()

	opts := Options{
		OutDir:           t.TempDir(),
		Sources:          []string{"youtube"},
		SkipVerify:       true,
		SkipMetadata:     true,
		SkipDepCheck:     true,
		IsAgent:          true,
		ProgressReporter: reporter,
	}

	_ = Run(ctx, "Test Artist - Test Track", opts)
	_ = reporter.Close()

	<-done

	mu.Lock()
	defer mu.Unlock()

	if len(receivedEvents) == 0 {
		t.Fatal("expected progress events over socket, got 0")
	}

	hasErrorEvent := false
	for _, ev := range receivedEvents {
		if ev.Type == progress.EventError {
			hasErrorEvent = true
			break
		}
	}
	if !hasErrorEvent {
		t.Errorf("expected at least one error event when context is canceled, got: %+v", receivedEvents)
	}
}
