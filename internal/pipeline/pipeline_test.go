package pipeline

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/dj/fetch-track-cli/internal/cache"
	"github.com/dj/fetch-track-cli/internal/deps"
	"github.com/dj/fetch-track-cli/internal/downloader"
	"github.com/dj/fetch-track-cli/internal/metadata"
	"github.com/dj/fetch-track-cli/internal/progress"
	"github.com/dj/fetch-track-cli/internal/verifier"
)

func createTestAudio(t *testing.T, dir, filename string) string {
	t.Helper()
	filePath := filepath.Join(dir, filename)
	cmd := exec.Command("ffmpeg", "-v", "quiet", "-hide_banner", "-f", "lavfi", "-i", "anullsrc=r=48000:cl=stereo", "-t", "2", "-y", filePath)
	if err := cmd.Run(); err != nil {
		t.Skipf("ffmpeg not available: %v", err)
	}
	return filePath
}

func setupTestEnvironment(t *testing.T) (string, func()) {
	t.Helper()
	tempCacheDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tempCacheDir)
	t.Setenv("AGENT", "0")

	c := cache.NewInDir(filepath.Join(tempCacheDir, "fetch-track"), true)
	_ = c.Put("deps", "yt-dlp", "2026.08.01", time.Hour)
	_ = c.Put("deps", "ffmpeg", "ffmpeg version 8.1.2", time.Hour)
	_ = c.Put("deps", "ffprobe", "ffprobe version 8.1.2", time.Hour)

	cleanupDeps := deps.SetDefaultRunner(func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "yt-dlp" {
			return []byte("2026.08.01"), nil
		}
		return []byte(fmt.Sprintf("%s version 8.1.2", name)), nil
	})

	return tempCacheDir, func() {
		cleanupDeps()
	}
}

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
	cancel()

	_, cleanupEnv := setupTestEnvironment(t)
	defer cleanupEnv()
	t.Setenv("AGENT", "1")

	cleanupDl := downloader.SetDefaultRunner(func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return nil, errors.New("canceled")
	})
	defer cleanupDl()

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

	_, cleanupEnv := setupTestEnvironment(t)
	defer cleanupEnv()
	t.Setenv("AGENT", "1")

	cleanupDl := downloader.SetDefaultRunner(func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return nil, errors.New("canceled")
	})
	defer cleanupDl()

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
	unassignedPool := make([]downloader.Candidate, len(pool))
	copy(unassignedPool, pool)
	_ = downloader.RankAllCandidates(unassignedPool, "Boris Brejcha", "Space X")

	if unassignedPool[0].Score != 0 {
		t.Fatalf("expected unassigned pool element to have Score = 0, got %d", unassignedPool[0].Score)
	}

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
	cancel()

	_, cleanupEnv := setupTestEnvironment(t)
	defer cleanupEnv()
	t.Setenv("AGENT", "1")

	cleanupDl := downloader.SetDefaultRunner(func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return nil, errors.New("canceled")
	})
	defer cleanupDl()

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

func TestRun_EndToEndQuery_NonAgent(t *testing.T) {
	tempCacheDir, cleanupEnv := setupTestEnvironment(t)
	defer cleanupEnv()
	t.Setenv("AGENT", "0")

	outDir := t.TempDir()
	ctx := context.Background()

	c := cache.NewInDir(filepath.Join(tempCacheDir, "fetch-track"), true)
	metaRes := metadata.TrackMetadataResult{
		Title:       "Space X",
		Artist:      "Boris Brejcha",
		Album:       "Space X Single",
		ReleaseYear: "2024",
		Genre:       "Minimal Techno",
		Source:      "iTunes API",
	}
	_ = c.Put("metadata", "Space X", metaRes, time.Hour)
	_ = c.Put("metadata", "Boris Brejcha - Space X", metaRes, time.Hour)
	_ = c.Put("metadata", "Boris Brejcha - Space X (Extended Mix)", metaRes, time.Hour)
	_ = c.Put("metadata", "SpaceX", metaRes, time.Hour)

	jsonSearch := `{"id":"spx1","title":"Boris Brejcha - Space X (Extended Mix)","duration":2,"webpage_url":"https://soundcloud.com/boris-brejcha/space-x"}` + "\n"
	mockRunner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		for _, a := range args {
			if a == "-o" {
				createTestAudio(t, outDir, "Boris Brejcha - Space X (Extended Mix).m4a")
				return []byte("downloaded"), nil
			}
		}
		return []byte(jsonSearch), nil
	}

	cleanupDl := downloader.SetDefaultRunner(mockRunner)
	defer cleanupDl()

	opts := Options{
		OutDir:       outDir,
		Sources:      []string{"soundcloud"},
		SkipVerify:   false,
		SkipMetadata: false,
		SkipDepCheck: false,
		Verbose:      false,
		IsAgent:      false,
	}

	err := Run(ctx, "Boris Brejcha - Space X", opts)
	if err != nil {
		t.Fatalf("Run end-to-end query error = %v", err)
	}

	// Single word query
	outDir2 := t.TempDir()
	opts2 := opts
	opts2.OutDir = outDir2
	mockRunner2 := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		for _, a := range args {
			if a == "-o" {
				createTestAudio(t, outDir2, "SpaceX.m4a")
				return []byte("downloaded"), nil
			}
		}
		return []byte(`{"id":"spx2","title":"SpaceX","duration":2,"webpage_url":"https://soundcloud.com/spacex"}` + "\n"), nil
	}
	cleanupDl2 := downloader.SetDefaultRunner(mockRunner2)
	defer cleanupDl2()

	err = Run(ctx, "SpaceX", opts2)
	if err != nil {
		t.Fatalf("Run single word query error = %v", err)
	}
}

func TestRun_EndToEndQuery_AgentMode(t *testing.T) {
	tempCacheDir, cleanupEnv := setupTestEnvironment(t)
	defer cleanupEnv()
	t.Setenv("AGENT", "1")

	outDir := t.TempDir()
	ctx := context.Background()

	c := cache.NewInDir(filepath.Join(tempCacheDir, "fetch-track"), true)
	metaRes := metadata.TrackMetadataResult{
		Title:       "Space X",
		Artist:      "Boris Brejcha",
		Album:       "Space X Single",
		ReleaseYear: "2024",
		Genre:       "Minimal Techno",
		Source:      "iTunes API",
	}
	_ = c.Put("metadata", "Space X", metaRes, time.Hour)
	_ = c.Put("metadata", "Boris Brejcha - Space X", metaRes, time.Hour)
	_ = c.Put("metadata", "Boris Brejcha - Space X (Extended Mix)", metaRes, time.Hour)

	jsonSearch := `{"id":"spx1","title":"Boris Brejcha - Space X (Extended Mix)","duration":2,"webpage_url":"https://soundcloud.com/boris-brejcha/space-x"}` + "\n"
	mockRunner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		for _, a := range args {
			if a == "-o" {
				createTestAudio(t, outDir, "Boris Brejcha - Space X (Extended Mix).m4a")
				return []byte("downloaded"), nil
			}
		}
		return []byte(jsonSearch), nil
	}

	cleanupDl := downloader.SetDefaultRunner(mockRunner)
	defer cleanupDl()

	opts := Options{
		OutDir:       outDir,
		Sources:      []string{"soundcloud"},
		SkipVerify:   false,
		SkipMetadata: false,
		SkipDepCheck: false,
		Verbose:      true,
		IsAgent:      true,
	}

	err := Run(ctx, "Boris Brejcha - Space X", opts)
	if err != nil {
		t.Fatalf("Run agent mode error = %v", err)
	}
}

func TestRun_EndToEndURL_CachedAndUncached(t *testing.T) {
	tempCacheDir, cleanupEnv := setupTestEnvironment(t)
	defer cleanupEnv()
	t.Setenv("AGENT", "0")

	outDir := t.TempDir()
	ctx := context.Background()

	c := cache.NewInDir(filepath.Join(tempCacheDir, "fetch-track"), true)

	urlMeta := struct {
		Title           string  `json:"title"`
		Uploader        string  `json:"uploader"`
		DurationSeconds float64 `json:"duration_seconds"`
		Format          string  `json:"format"`
	}{
		Title:           "Boris Brejcha - Space X",
		Uploader:        "Boris Brejcha",
		DurationSeconds: 2,
		Format:          "m4a",
	}
	_ = c.Put("url_meta", "https://soundcloud.com/boris-brejcha/space-x", urlMeta, time.Hour)

	metaRes := metadata.TrackMetadataResult{
		Title:       "Space X",
		Artist:      "Boris Brejcha",
		Album:       "Space X Single",
		ReleaseYear: "2024",
		Genre:       "Minimal Techno",
		Source:      "iTunes API",
	}
	_ = c.Put("metadata", "Space X", metaRes, time.Hour)
	_ = c.Put("metadata", "Boris Brejcha - Space X", metaRes, time.Hour)
	_ = c.Put("metadata", "Boris Brejcha - Space X (Extended Mix)", metaRes, time.Hour)

	jsonSearch := `{"id":"spx1","title":"Boris Brejcha - Space X (Extended Mix)","duration":2,"webpage_url":"https://soundcloud.com/boris-brejcha/space-x"}` + "\n"
	mockRunner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		for _, a := range args {
			if a == "-o" {
				createTestAudio(t, outDir, "Boris Brejcha - Space X.m4a")
				return []byte("downloaded"), nil
			}
		}
		return []byte(jsonSearch), nil
	}

	cleanupDl := downloader.SetDefaultRunner(mockRunner)
	defer cleanupDl()

	opts := Options{
		OutDir:       outDir,
		Sources:      []string{"soundcloud"},
		SkipVerify:   false,
		SkipMetadata: false,
		SkipDepCheck: false,
		Verbose:      false,
		IsAgent:      false,
	}

	err := Run(ctx, "https://soundcloud.com/boris-brejcha/space-x", opts)
	if err != nil {
		t.Fatalf("Run end-to-end URL error = %v", err)
	}
}

func TestRun_AdditionalBranches(t *testing.T) {
	tempCacheDir, cleanupEnv := setupTestEnvironment(t)
	defer cleanupEnv()
	t.Setenv("AGENT", "0")
	ctx := context.Background()

	c := cache.NewInDir(filepath.Join(tempCacheDir, "fetch-track"), true)

	// Keep deps runner mocked for whole test
	cleanupDeps := deps.SetDefaultRunner(func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "yt-dlp" {
			return []byte("2026.08.01"), nil
		}
		return []byte(fmt.Sprintf("%s version 8.1.2", name)), nil
	})
	defer cleanupDeps()

	// 1. Dependency check failure when runner fails
	failDepsCleanup := deps.SetDefaultRunner(func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return nil, errors.New("missing tool")
	})
	_ = c.Delete("deps", "yt-dlp")
	_ = c.Delete("deps", "ffmpeg")
	_ = c.Delete("deps", "ffprobe")

	opts := Options{
		OutDir:       t.TempDir(),
		Sources:      []string{"soundcloud"},
		SkipDepCheck: false,
		AutoInstall:  false,
		IsAgent:      true,
	}
	err := Run(ctx, "Test Track", opts)
	if err == nil {
		t.Error("expected error on dependency check failure")
	}
	failDepsCleanup()

	// Restore deps in cache
	_ = c.Put("deps", "yt-dlp", "2026.08.01", time.Hour)
	_ = c.Put("deps", "ffmpeg", "ffmpeg version 8.1", time.Hour)
	_ = c.Put("deps", "ffprobe", "ffprobe version 8.1", time.Hour)

	// 2. Direct URL candidate with SkipVerify and SkipMetadata
	urlMeta := struct {
		Title           string  `json:"title"`
		Uploader        string  `json:"uploader"`
		DurationSeconds float64 `json:"duration_seconds"`
		Format          string  `json:"format"`
	}{
		Title:           "Direct Track",
		Uploader:        "Direct Artist",
		DurationSeconds: 2,
		Format:          "m4a",
	}
	_ = c.Put("url_meta", "https://soundcloud.com/direct/track", urlMeta, time.Hour)

	outDir := t.TempDir()
	mockRunner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		for _, a := range args {
			if a == "-o" {
				createTestAudio(t, outDir, "Direct Track.m4a")
				return []byte("downloaded"), nil
			}
		}
		return []byte(""), nil
	}

	cleanupDl := downloader.SetDefaultRunner(mockRunner)
	defer cleanupDl()

	cleanupVerifier := verifier.SetDefaultRunner(func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return nil, errors.New("probe failed")
	})
	defer cleanupVerifier()

	optsDirect := Options{
		OutDir:       outDir,
		Sources:      []string{"soundcloud"},
		SkipVerify:   true,
		SkipMetadata: true,
		SkipDepCheck: false,
		Verbose:      false,
		IsAgent:      false,
	}
	err = Run(ctx, "https://soundcloud.com/direct/track", optsDirect)
	if err != nil {
		t.Fatalf("Run direct URL error = %v", err)
	}

	// 3. Direct URL where probe fails
	outDir3 := t.TempDir()
	optsDirect3 := optsDirect
	optsDirect3.OutDir = outDir3
	_ = c.Put("metadata", "fail", metadata.TrackMetadataResult{Title: "Fail", Artist: "Artist"}, time.Hour)
	mockRunner3 := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		for _, a := range args {
			if a == "-o" {
				createTestAudio(t, outDir3, "fail.m4a")
				return []byte("downloaded"), nil
			}
		}
		return []byte(""), nil
	}
	cleanupDl3 := downloader.SetDefaultRunner(mockRunner3)
	defer cleanupDl3()

	_ = Run(ctx, "https://invalid-url.example.com/fail", optsDirect3)
}

func TestRun_InteractiveCancel(t *testing.T) {
	tempCacheDir, cleanupEnv := setupTestEnvironment(t)
	defer cleanupEnv()
	t.Setenv("AGENT", "0")

	outDir := t.TempDir()
	ctx := context.Background()

	c := cache.NewInDir(filepath.Join(tempCacheDir, "fetch-track"), true)
	metaRes := metadata.TrackMetadataResult{
		Title:  "Space X",
		Artist: "Boris Brejcha",
	}
	_ = c.Put("metadata", "Space X", metaRes, time.Hour)
	_ = c.Put("metadata", "Boris Brejcha - Space X", metaRes, time.Hour)

	jsonSearch := `{"id":"spx1","title":"Boris Brejcha - Space X (Extended Mix)","duration":2,"webpage_url":"https://soundcloud.com/boris-brejcha/space-x"}` + "\n"
	mockRunner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte(jsonSearch), nil
	}

	cleanupDl := downloader.SetDefaultRunner(mockRunner)
	defer cleanupDl()

	opts := Options{
		OutDir:       outDir,
		Sources:      []string{"soundcloud"},
		Interactive:  true,
		SkipDepCheck: false,
		IsAgent:      false,
	}

	// In non-TTY test runner, interactive candidate prompt will cancel and return error
	_ = Run(ctx, "Boris Brejcha - Space X", opts)
}

func TestRun_DownloadFailure(t *testing.T) {
	_, cleanupEnv := setupTestEnvironment(t)
	defer cleanupEnv()
	t.Setenv("AGENT", "1")

	outDir := t.TempDir()
	ctx := context.Background()

	jsonSearch := `{"id":"spx1","title":"Boris Brejcha - Space X (Extended Mix)","duration":2,"webpage_url":"https://soundcloud.com/boris-brejcha/space-x"}` + "\n"
	mockRunner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		for _, a := range args {
			if a == "-o" {
				return nil, errors.New("download failed")
			}
		}
		return []byte(jsonSearch), nil
	}

	cleanup := downloader.SetDefaultRunner(mockRunner)
	defer cleanup()

	opts := Options{
		OutDir:       outDir,
		Sources:      []string{"soundcloud"},
		SkipVerify:   true,
		SkipMetadata: true,
		SkipDepCheck: false,
		IsAgent:      true,
	}

	err := Run(ctx, "Boris Brejcha - Space X", opts)
	if err == nil {
		t.Fatal("expected error on download failure")
	}
}
