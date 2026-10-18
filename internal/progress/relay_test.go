package progress

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"testing"
)

func TestRelayMetadata(t *testing.T) {
	var buf bytes.Buffer
	r := &Reporter{encoder: json.NewEncoder(&buf), jobID: "parent"}
	if err := r.Emit(Event{Type: EventPhaseStart, Phase: "metadata", Step: 5, TotalSteps: 5}); err != nil {
		t.Fatal(err)
	}
	err := r.RelayMetadata(context.Background(), func(target, jobID string) error {
		if jobID != "parent" {
			t.Errorf("child job ID = %q", jobID)
		}
		conn, err := net.Dial("tcp", strings.TrimPrefix(target, "tcp://"))
		if err != nil {
			return err
		}
		defer func() { _ = conn.Close() }()
		_, err = conn.Write([]byte("{\"job_id\":\"child\",\"sequence\":1,\"type\":\"metadata_found\",\"phase\":\"identify\",\"percent\":50,\"metadata\":{\"title\":\"Track\"}}\n{\"type\":\"complete\",\"percent\":100,\"result\":{\"path\":\"track.m4a\",\"source\":\"Shazam\"}}\n"))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Emit(Event{Type: EventComplete}); err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(&buf)
	for i, want := range []float64{80, 89.5, 99, 100} {
		var ev Event
		if err := dec.Decode(&ev); err != nil {
			t.Fatal(err)
		}
		if ev.JobID != "parent" || ev.Sequence != uint64(i+1) || ev.Percent != want {
			t.Fatalf("event %d: %+v", i, ev)
		}
		if i == 1 && (string(ev.Metadata) != `{"title":"Track"}` || ev.Phase != "metadata" || ev.Subphase != "identify") {
			t.Fatalf("metadata lost: %+v", ev)
		}
		if i == 2 && (ev.Type != "tagged" || ev.Result.Source != "Shazam") {
			t.Fatalf("premature completion or lost result: %+v", ev)
		}
	}
}

func TestRelayMetadataFailure(t *testing.T) {
	want := errors.New("command failed")
	for _, r := range []*Reporter{nil, {encoder: json.NewEncoder(&bytes.Buffer{})}} {
		if err := r.RelayMetadata(context.Background(), func(target, jobID string) error { return want }); !errors.Is(err, want) {
			t.Fatalf("got %v, want command error", err)
		}
	}
}

func TestReadMetadataErrors(t *testing.T) {
	for _, input := range []string{"not json", `{}`} {
		r := &Reporter{encoder: json.NewEncoder(&bytes.Buffer{})}
		if err := r.readMetadata(strings.NewReader(input)); err == nil {
			t.Fatalf("expected error for %q", input)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := &Reporter{encoder: json.NewEncoder(&bytes.Buffer{})}
	if err := r.RelayMetadata(ctx, func(target, jobID string) error { t.Error("ran after cancellation"); return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}
