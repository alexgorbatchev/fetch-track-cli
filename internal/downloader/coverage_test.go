package downloader

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/alexgorbatchev/fetch-track-cli/internal/cache"
)

func TestSearchObserverAndCache(t *testing.T) {
	t.Setenv("AGENT", "0")
	c := cache.NewInDir(t.TempDir(), true)
	var mu sync.Mutex
	starts, completed, observed := 0, 0, 0
	observer := &SearchObserver{
		OnQueryStart:    func(string) { mu.Lock(); starts++; mu.Unlock() },
		OnQueryComplete: func(_ string, count int) { mu.Lock(); completed += count; mu.Unlock() },
		OnCandidate:     func(Candidate) { mu.Lock(); observed++; mu.Unlock() },
	}
	runner := func(context.Context, string, ...string) ([]byte, error) {
		return []byte("\ninvalid json\n" + `{"title":"Dream Skyline","duration":300,"url":"https://example.test/audio"}`), nil
	}
	got, err := SearchSourcesInParallelWithObserver(context.Background(), runner, []string{"youtube", " "}, "Artist", "Skyline Sunset", "query", c, observer)
	if err != nil || len(got) != 1 || got[0].WebpageURL != "https://example.test/audio" {
		t.Fatalf("search = %+v, %v", got, err)
	}
	if starts != 4 || completed != 1 || observed != 1 {
		t.Fatalf("observer starts=%d completed=%d candidates=%d", starts, completed, observed)
	}
	fail := func(context.Context, string, ...string) ([]byte, error) {
		return nil, errors.New("cache should avoid search")
	}
	got, err = SearchSourcesInParallelWithObserver(context.Background(), fail, []string{"youtube", " "}, "Artist", "Skyline Sunset", "query", c, observer)
	if err != nil || len(got) != 1 || observed != 2 || starts != 4 {
		t.Fatalf("cached search=%+v error=%v candidates=%d starts=%d", got, err, observed, starts)
	}
	got, err = SearchSourcesInParallelWithObserver(context.Background(), fail, []string{"youtube", " "}, "Artist", "Skyline Sunset", "query", c, observer, true)
	if err != nil || len(got) != 1 {
		t.Fatalf("verbose cache = %+v, %v", got, err)
	}
}

func TestSearchDefaultSourcesAndEmptyTitle(t *testing.T) {
	t.Setenv("AGENT", "1")
	runner := func(context.Context, string, ...string) ([]byte, error) {
		return []byte(`{"id":"one","title":"Any song","duration":300}`), nil
	}
	t.Cleanup(SetDefaultRunner(runner))
	got, err := SearchSourcesInParallelWithRunner(context.Background(), nil, nil, "", "", "query", nil)
	if err != nil || len(got) != 2 {
		t.Fatalf("default source search = %+v, %v", got, err)
	}
}

func TestRankingVersionAndDurationReasons(t *testing.T) {
	tests := []struct {
		name, artist, title, candidate, reason string
		duration                               float64
	}{
		{"long club", "Artist", "Song", "Artist - Song", "duration_long_club", 700},
		{"short radio", "Artist", "Song (Radio Edit)", "Artist - Song (Radio Edit)", "duration_short_radio", 120},
		{"short penalty", "Artist", "Song", "Artist - Song", "duration_short_penalty", 120},
		{"extended rejects radio", "Artist", "Song (Extended Mix)", "Artist - Song (Radio Edit)", "radio_penalty_on_extended_intent", 200},
		{"topic", "Artist", "Song", "Artist - Song", "official_topic_channel", 300},
		{"unrequested remix", "Artist", "Song", "Artist - Song (Someone Remix)", "unrequested_remix", 300},
		{"missing target remix", "Artist", "Song (Solomun Remix)", "Artist - Song", "missing_target_remix", 300},
		{"partial artist match", "Artist & Guest", "Song", "Artist - Song", "partial_artist_match", 300},
		{"fuzzy title", "Artist", "abcd", "Artist - a b c d", "title_fuzzy_match", 300},
		{"partial title", "Artist", "Skyline Sunset", "Artist - Skyline", "partial_title_tokens", 300},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidates := []Candidate{{Title: tt.candidate, Uploader: "Artist - Topic", Duration: tt.duration}}
			got := RankAllCandidates(candidates, tt.artist, tt.title)
			if len(got) != 1 || !strings.Contains(got[0].ScoreReason, tt.reason) {
				t.Fatalf("ranking = %+v, want reason %q", got, tt.reason)
			}
		})
	}
}
