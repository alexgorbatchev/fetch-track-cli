package progress

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

// RelayMetadata runs a tagging subprocess against a private loopback target.
// The callback must wait for the subprocess to exit and close its connection.
// Child events are drained before returning so parent completion comes last.
func (r *Reporter) RelayMetadata(ctx context.Context, run func(target, jobID string) error) error {
	if r == nil {
		return run("", "")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var lc net.ListenConfig
	l, err := lc.Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("starting metadata progress relay: %w", err)
	}
	defer func() { _ = l.Close() }()
	stop := context.AfterFunc(ctx, func() { _ = l.Close() }) // unblock Accept on cancellation
	defer stop()
	r.mu.Lock()
	if r.jobID == "" {
		r.jobID = rand.Text()
	}
	jobID := r.jobID
	r.mu.Unlock()
	done := make(chan error, 1)
	go func() {
		conn, err := l.Accept()
		if err != nil {
			done <- fmt.Errorf("accepting metadata progress: %w", err)
			return
		}
		defer func() { _ = conn.Close() }()
		stop := context.AfterFunc(ctx, func() { _ = conn.Close() }) // unblock Decode on cancellation
		defer stop()
		done <- r.readMetadata(conn)
	}()
	runErr := run("tcp://"+l.Addr().String(), jobID)
	// A child that never connects or leaks its socket must not stall acquisition.
	const drainTimeout = 2 * time.Second
	timer := time.AfterFunc(drainTimeout, cancel)
	defer timer.Stop()
	relayErr := <-done
	return errors.Join(runErr, relayErr)
}

func (r *Reporter) readMetadata(reader io.Reader) error {
	dec := json.NewDecoder(reader)
	for {
		var ev Event
		if err := dec.Decode(&ev); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("reading metadata progress: %w", err)
		}
		if ev.Type == "" {
			return errors.New("metadata progress event has no type")
		}
		ev.Subphase = ev.Phase
		ev.Phase = "metadata"
		ev.Step, ev.TotalSteps = 5, 5
		// Reserve final completion for fetch-track after it handles tagging output.
		const metadataStart, metadataEnd = 80.0, 99.0
		ev.Percent = metadataStart + min(100, max(0, ev.Percent))*(metadataEnd-metadataStart)/100
		if ev.Type == EventComplete {
			ev.Type = "tagged"
		}
		if err := r.Emit(ev); err != nil {
			return fmt.Errorf("forwarding metadata progress: %w", err)
		}
	}
}
