package progress

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// EventType represents the category of a progress event.
type EventType string

const (
	EventPhaseStart        EventType = "phase_start"
	EventCandidateFound    EventType = "candidate_found"
	EventCandidateSelected EventType = "candidate_selected"
	EventProgress          EventType = "progress"
	EventComplete          EventType = "complete"
	EventError             EventType = "error"
)

// CandidateInfo describes a candidate track found during search.
type CandidateInfo struct {
	ID         string  `json:"id,omitempty"`
	Title      string  `json:"title"`
	Artist     string  `json:"artist,omitempty"`
	Source     string  `json:"source"`
	Duration   float64 `json:"duration_seconds,omitempty"`
	Score      int     `json:"score,omitempty"`
	WebpageURL string  `json:"url,omitempty"`
}

// ResultInfo describes the final artifact and quality verification metrics.
type ResultInfo struct {
	Source          string  `json:"source,omitempty"`
	Path            string  `json:"path"`
	Title           string  `json:"title,omitempty"`
	Artist          string  `json:"artist,omitempty"`
	Album           string  `json:"album,omitempty"`
	ReleaseYear     string  `json:"release_year,omitempty"`
	Duration        float64 `json:"duration_seconds,omitempty"`
	BandwidthHz     int     `json:"bandwidth_hz,omitempty"`
	BandwidthRating string  `json:"bandwidth_rating,omitempty"`
	SuggestedGainDb float64 `json:"suggested_gain_db,omitempty"`
	Status          string  `json:"status,omitempty"`
}

// Event represents an atomic structured telemetry/progress update sent over the socket.
type Event struct {
	JobID      string          `json:"job_id"`
	Sequence   uint64          `json:"sequence"`
	Timestamp  time.Time       `json:"timestamp"`
	Type       EventType       `json:"type"`
	Phase      string          `json:"phase,omitempty"`
	Subphase   string          `json:"subphase,omitempty"`
	Step       int             `json:"step,omitempty"`
	TotalSteps int             `json:"total_steps,omitempty"`
	Message    string          `json:"message,omitempty"`
	Percent    float64         `json:"percent"`
	Candidate  *CandidateInfo  `json:"candidate,omitempty"`
	Metadata   json.RawMessage `json:"metadata,omitempty"`
	Result     *ResultInfo     `json:"result,omitempty"`
	Error      string          `json:"error,omitempty"`
}

// Reporter handles streaming progress events to a socket, file descriptor, or stream.
type Reporter struct {
	mu         sync.Mutex
	closer     io.Closer
	encoder    *json.Encoder
	shouldSkip bool
	percent    float64
	jobID      string
	sequence   uint64
}

// ParseTarget parses a target string into a normalized scheme and address/path.
func ParseTarget(raw string) (scheme, address string, err error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", "", errors.New("empty progress target")
	}

	switch trimmed {
	case "stdout", "stdout://":
		return "stdout", "", nil
	case "stderr", "stderr://":
		return "stderr", "", nil
	}

	if strings.Contains(trimmed, "://") {
		u, err := url.Parse(trimmed)
		if err == nil && u.Scheme != "" {
			addr := u.Host + u.Path
			return u.Scheme, addr, nil
		}
	}

	// Auto-detect unix domain socket by path prefix or .sock extension
	if strings.HasPrefix(trimmed, "/") || strings.HasPrefix(trimmed, "./") || strings.HasPrefix(trimmed, "../") || strings.HasSuffix(trimmed, ".sock") {
		return "unix", trimmed, nil
	}

	// Check if it's host:port
	if strings.Contains(trimmed, ":") {
		return "tcp", trimmed, nil
	}

	return "", "", fmt.Errorf("unrecognized progress target format %q", raw)
}

// NewReporter connects to the specified progress target and returns a Reporter.
// An empty jobID generates a random ID on the first event. Sequence starts at 1
// for each reporter; callers should use a different job ID for each run.
// Supported targets:
//   - "unix:///path/to/socket.sock" or "/path/to/socket.sock"
//   - "tcp://127.0.0.1:9099" or "127.0.0.1:9099"
//   - "fd://3"
//   - "stdout" or "stderr"
func NewReporter(ctx context.Context, target, jobID string) (*Reporter, error) {
	scheme, address, err := ParseTarget(target)
	if err != nil {
		return nil, err
	}

	var writer io.Writer
	var closer io.Closer

	switch scheme {
	case "unix":
		var d net.Dialer
		conn, dialErr := d.DialContext(ctx, "unix", address)
		if dialErr != nil {
			return nil, fmt.Errorf("connecting to unix socket %s: %w", address, dialErr)
		}
		writer = conn
		closer = conn

	case "tcp":
		var d net.Dialer
		conn, dialErr := d.DialContext(ctx, "tcp", address)
		if dialErr != nil {
			return nil, fmt.Errorf("connecting to tcp address %s: %w", address, dialErr)
		}
		writer = conn
		closer = conn

	case "fd":
		fdNum, parseErr := strconv.Atoi(address)
		if parseErr != nil {
			return nil, fmt.Errorf("invalid file descriptor %q: %w", address, parseErr)
		}
		file := os.NewFile(uintptr(fdNum), "progress_fd")
		if file == nil {
			return nil, fmt.Errorf("failed to open file descriptor %d", fdNum)
		}
		writer = file
		closer = file

	case "stdout":
		writer = os.Stdout
		closer = nil

	case "stderr":
		writer = os.Stderr
		closer = nil

	default:
		return nil, fmt.Errorf("unsupported progress target scheme %q", scheme)
	}

	return &Reporter{
		closer:  closer,
		encoder: json.NewEncoder(writer),
		jobID:   jobID,
	}, nil
}

// Emit sends a single progress event as an NDJSON line. Thread-safe and nil-safe.
func (r *Reporter) Emit(event Event) error {
	if r == nil || r.encoder == nil {
		return nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.jobID == "" {
		r.jobID = rand.Text()
	}
	event.JobID = r.jobID
	event.Sequence = r.sequence + 1
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	} else {
		event.Timestamp = event.Timestamp.UTC()
	}
	event.Percent = r.completion(event)
	if err := r.encoder.Encode(event); err != nil {
		return err
	}
	r.percent = event.Percent
	r.sequence = event.Sequence
	return nil
}

// completion measures pipeline milestones, not elapsed time. Retaining the
// highest value keeps retries and events without step information from regressing.
func (r *Reporter) completion(event Event) float64 {
	const complete = 100.0
	if event.Type == EventComplete {
		return complete
	}
	percent := event.Percent
	if math.IsNaN(percent) || math.IsInf(percent, 0) {
		percent = r.percent
	}
	if event.Type == EventPhaseStart && event.Step > 0 && event.Step <= event.TotalSteps {
		percent = max(percent, complete*float64(event.Step-1)/float64(event.TotalSteps))
	}
	return min(complete, max(r.percent, percent))
}

// Close gracefully closes the socket or file descriptor connection if applicable.
func (r *Reporter) Close() error {
	if r == nil {
		return nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closer != nil {
		err := r.closer.Close()
		r.closer = nil
		return err
	}
	return nil
}
