package progress

import (
	"bytes"
	"encoding/json"
	"math"
	"sync"
	"testing"
	"time"
)

func TestReporterCompletion(t *testing.T) {
	var buf bytes.Buffer
	r := &Reporter{encoder: json.NewEncoder(&buf)}
	tests := []struct {
		name  string
		event Event
		want  float64
	}{
		{"initial", Event{Type: EventPhaseStart, Step: 1, TotalSteps: 5}, 0},
		{"search", Event{Type: EventPhaseStart, Step: 2, TotalSteps: 5}, 20},
		{"found", Event{Type: EventCandidateFound}, 20},
		{"selected", Event{Type: EventCandidateSelected}, 20},
		{"download", Event{Type: EventPhaseStart, Step: 3, TotalSteps: 5}, 40},
		{"explicit", Event{Type: EventProgress, Percent: 45.5}, 45.5},
		{"unspecified", Event{Type: EventProgress}, 45.5},
		{"error", Event{Type: EventError}, 45.5},
		{"retry", Event{Type: EventPhaseStart, Step: 3, TotalSteps: 5}, 45.5},
		{"negative", Event{Type: EventProgress, Percent: -10}, 45.5},
		{"nan", Event{Type: EventProgress, Percent: math.NaN()}, 45.5},
		{"infinite", Event{Type: EventProgress, Percent: math.Inf(1)}, 45.5},
		{"invalid step", Event{Type: EventPhaseStart, Step: 6, TotalSteps: 5}, 45.5},
		{"missing total", Event{Type: EventPhaseStart, Step: 4}, 45.5},
		{"verify", Event{Type: EventPhaseStart, Step: 4, TotalSteps: 5}, 60},
		{"metadata", Event{Type: EventPhaseStart, Step: 5, TotalSteps: 5}, 80},
		{"complete", Event{Type: EventComplete}, 100},
		{"above maximum", Event{Type: EventProgress, Percent: 120}, 100},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf.Reset()
			if err := r.Emit(tt.event); err != nil {
				t.Fatalf("Emit: %v", err)
			}
			var got struct {
				Percent *float64 `json:"percent"`
			}
			if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if got.Percent == nil {
				t.Fatalf("percent missing: %s", buf.String())
			}
			if *got.Percent != tt.want {
				t.Errorf("percent = %v, want %v", *got.Percent, tt.want)
			}
		})
	}
}

func TestReporterEnvelope(t *testing.T) {
	var buf bytes.Buffer
	r := &Reporter{encoder: json.NewEncoder(&buf)}
	const count = 24
	var wg sync.WaitGroup
	for range count {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := r.Emit(Event{Type: EventProgress, Phase: "download"}); err != nil {
				t.Errorf("Emit: %v", err)
			}
		}()
	}
	wg.Wait()
	dec := json.NewDecoder(&buf)
	var jobID string
	for i := 1; i <= count; i++ {
		var got struct {
			JobID    string `json:"job_id"`
			Sequence int    `json:"sequence"`
		}
		if err := dec.Decode(&got); err != nil {
			t.Fatal(err)
		}
		if got.JobID == "" {
			t.Fatal("missing job_id")
		}
		if i == 1 {
			jobID = got.JobID
		}
		if got.JobID != jobID || got.Sequence != i {
			t.Fatalf("event %d: %+v, want job %q and sequence %d", i, got, jobID, i)
		}
	}
}

func TestReporterFailedEventDoesNotAdvance(t *testing.T) {
	var buf bytes.Buffer
	r := &Reporter{encoder: json.NewEncoder(&buf), jobID: "caller-job"}
	if err := r.Emit(Event{Type: EventProgress, Percent: 90, Candidate: &CandidateInfo{Duration: math.NaN()}}); err == nil {
		t.Fatal("expected JSON encoding error")
	}
	stamp := time.Date(2026, 10, 18, 12, 0, 0, 0, time.FixedZone("offset", -7*60*60))
	if err := r.Emit(Event{Type: EventPhaseStart, Step: 2, TotalSteps: 5, JobID: "wrong-job", Sequence: 99, Timestamp: stamp}); err != nil {
		t.Fatal(err)
	}
	var ev Event
	if err := json.Unmarshal(buf.Bytes(), &ev); err != nil {
		t.Fatal(err)
	}
	if ev.JobID != "caller-job" || ev.Sequence != 1 || ev.Percent != 20 || ev.Timestamp.Location() != time.UTC || !ev.Timestamp.Equal(stamp) {
		t.Fatalf("unexpected envelope after failed event: %+v", ev)
	}
}
