package main

import (
	"log/slog"
	"sync"
	"testing"
	"time"
)

// fakeMusic returns a scripted sequence of tracks.
type fakeMusic struct {
	mu     sync.Mutex
	tracks []Track
	idx    int
}

func (f *fakeMusic) GetCurrentTrack() (Track, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.tracks) == 0 {
		return Track{State: "stopped"}, nil
	}
	if f.idx >= len(f.tracks) {
		return f.tracks[len(f.tracks)-1], nil
	}
	tr := f.tracks[f.idx]
	f.idx++
	return tr, nil
}

// fakeLastFM records API calls.
type fakeLastFM struct {
	mu          sync.Mutex
	nowPlaying  []string
	scrobbles   []string
	failNP      bool
	failScrobble bool
}

func (f *fakeLastFM) UpdateNowPlaying(artist, track, album string, durationSec int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failNP {
		return errFake("np failed")
	}
	f.nowPlaying = append(f.nowPlaying, artist+"|"+track)
	return nil
}

func (f *fakeLastFM) Scrobble(artist, track, album string, timestamp int64, durationSec int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failScrobble {
		return errFake("scrobble failed")
	}
	f.scrobbles = append(f.scrobbles, artist+"|"+track)
	return nil
}

type errFake string

func (e errFake) Error() string { return string(e) }

func testLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// clock allows advancing scrobbler time in tests.
type testClock struct {
	base   time.Time
	offset time.Duration
}

func (c *testClock) Now() time.Time { return c.base.Add(c.offset) }
func (c *testClock) Advance(d time.Duration) { c.offset += d }

func setupScrobbler(t *testing.T) (*Scrobbler, *fakeLastFM, *testClock) {
	t.Helper()
	lf := &fakeLastFM{}
	cfg := DefaultConfig()
	cfg.PollInterval = 3
	clk := &testClock{base: time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)}
	s := NewScrobbler(&fakeMusic{}, lf, cfg, testLogger())
	s.now = clk.Now
	return s, lf, clk
}

func playing(name, artist string, dur float64) Track {
	return Track{Name: name, Artist: artist, Album: "Alb", Duration: dur, State: "playing"}
}

func TestShouldScrobble(t *testing.T) {
	// duration must be > 30
	if shouldScrobble(100, 30, 30, 0.5, 240) {
		t.Fatal("duration == min should not scrobble")
	}
	if shouldScrobble(100, 29, 30, 0.5, 240) {
		t.Fatal("short track should not scrobble")
	}
	// 50% of 200 = 100
	if !shouldScrobble(100, 200, 30, 0.5, 240) {
		t.Fatal("50% should scrobble")
	}
	if shouldScrobble(99, 200, 30, 0.5, 240) {
		t.Fatal("under 50% should not")
	}
	// long track: 4 minutes wins before 50%
	if !shouldScrobble(240, 600, 30, 0.5, 240) {
		t.Fatal("240s should scrobble long track")
	}
	if shouldScrobble(239, 600, 30, 0.5, 240) {
		t.Fatal("239s of 600 should not yet")
	}
}

func TestNowPlayingOnNewTrack(t *testing.T) {
	s, lf, _ := setupScrobbler(t)
	s.tick(playing("Song", "Artist", 200))
	if len(lf.nowPlaying) != 1 || lf.nowPlaying[0] != "Artist|Song" {
		t.Fatalf("nowPlaying = %v", lf.nowPlaying)
	}
	if lf.scrobbles != nil {
		t.Fatalf("should not scrobble yet: %v", lf.scrobbles)
	}
}

func TestScrobbleAtFiftyPercent(t *testing.T) {
	s, lf, clk := setupScrobbler(t)
	tr := playing("Song", "Artist", 100) // 50% = 50s

	s.tick(tr) // t=0, now playing
	// Poll every 3s: accumulate until >= 50
	for i := 0; i < 20; i++ {
		clk.Advance(3 * time.Second)
		s.tick(tr)
	}
	// played ≈ 3*20 = 60 >= 50
	if len(lf.scrobbles) != 1 {
		t.Fatalf("scrobbles = %v, want 1", lf.scrobbles)
	}
	// Further ticks must not double-scrobble
	clk.Advance(3 * time.Second)
	s.tick(tr)
	if len(lf.scrobbles) != 1 {
		t.Fatalf("double scrobble: %v", lf.scrobbles)
	}
}

func TestScrobbleAtFourMinutes(t *testing.T) {
	s, lf, clk := setupScrobbler(t)
	tr := playing("Epic", "Band", 600) // 10 min track; 50% = 300, max = 240

	s.tick(tr)
	// 80 * 3s = 240s
	for i := 0; i < 80; i++ {
		clk.Advance(3 * time.Second)
		s.tick(tr)
	}
	if len(lf.scrobbles) != 1 {
		t.Fatalf("scrobbles = %v after 240s play", lf.scrobbles)
	}
}

func TestPauseDoesNotAccumulateOrDoubleScrobble(t *testing.T) {
	s, lf, clk := setupScrobbler(t)
	tr := playing("Song", "Artist", 100)

	s.tick(tr)
	// Play 30s
	for i := 0; i < 10; i++ {
		clk.Advance(3 * time.Second)
		s.tick(tr)
	}
	// Pause for a long wall-clock time
	paused := tr
	paused.State = "paused"
	for i := 0; i < 50; i++ {
		clk.Advance(3 * time.Second)
		s.tick(paused)
	}
	if len(lf.scrobbles) != 0 {
		t.Fatalf("should not scrobble while short+paused: %v", lf.scrobbles)
	}

	// Resume and finish to 50%
	for i := 0; i < 10; i++ {
		clk.Advance(3 * time.Second)
		s.tick(tr)
	}
	// First resume tick doesn't add delta; then 9*3=27, plus earlier ~30 → ~57
	if len(lf.scrobbles) != 1 {
		t.Fatalf("scrobbles after resume = %v (played=%.1f)", lf.scrobbles, s.state.playedSeconds)
	}

	// Pause again — no second scrobble
	for i := 0; i < 10; i++ {
		clk.Advance(3 * time.Second)
		s.tick(paused)
	}
	clk.Advance(3 * time.Second)
	s.tick(tr)
	if len(lf.scrobbles) != 1 {
		t.Fatalf("double scrobble after pause: %v", lf.scrobbles)
	}
}

func TestTrackChangeResetsState(t *testing.T) {
	s, lf, clk := setupScrobbler(t)
	a := playing("A", "Artist", 100)
	b := playing("B", "Artist", 100)

	s.tick(a)
	for i := 0; i < 20; i++ {
		clk.Advance(3 * time.Second)
		s.tick(a)
	}
	if len(lf.scrobbles) != 1 || lf.scrobbles[0] != "Artist|A" {
		t.Fatalf("scrobbles = %v", lf.scrobbles)
	}

	s.tick(b) // new track
	if len(lf.nowPlaying) < 2 || lf.nowPlaying[len(lf.nowPlaying)-1] != "Artist|B" {
		t.Fatalf("nowPlaying = %v", lf.nowPlaying)
	}
	if s.state.scrobbled || s.state.playedSeconds != 0 {
		t.Fatalf("state not reset: %+v", s.state)
	}

	// B must be scrobbled independently
	for i := 0; i < 20; i++ {
		clk.Advance(3 * time.Second)
		s.tick(b)
	}
	if len(lf.scrobbles) != 2 || lf.scrobbles[1] != "Artist|B" {
		t.Fatalf("scrobbles = %v", lf.scrobbles)
	}
}

func TestStoppedClearsState(t *testing.T) {
	s, lf, clk := setupScrobbler(t)
	tr := playing("Song", "Artist", 100)
	s.tick(tr)
	clk.Advance(3 * time.Second)
	s.tick(tr)
	s.tick(Track{State: "stopped"})
	if s.state.current.Name != "" || s.state.playedSeconds != 0 {
		t.Fatalf("state not cleared: %+v", s.state)
	}
	// Playing again is a fresh start
	s.tick(tr)
	if len(lf.nowPlaying) != 2 {
		t.Fatalf("nowPlaying count = %d", len(lf.nowPlaying))
	}
}

func TestShortTrackNeverScrobbles(t *testing.T) {
	s, lf, clk := setupScrobbler(t)
	tr := playing("Jingle", "Ad", 25) // < 30s
	s.tick(tr)
	for i := 0; i < 30; i++ {
		clk.Advance(3 * time.Second)
		s.tick(tr)
	}
	if len(lf.scrobbles) != 0 {
		t.Fatalf("short track scrobbled: %v", lf.scrobbles)
	}
	// Now playing is still sent
	if len(lf.nowPlaying) != 1 {
		t.Fatalf("nowPlaying = %v", lf.nowPlaying)
	}
}

func TestNowPlayingFailureDoesNotCrash(t *testing.T) {
	s, lf, clk := setupScrobbler(t)
	lf.failNP = true
	tr := playing("Song", "Artist", 100)
	s.tick(tr) // should not panic
	lf.failNP = false
	// Accumulate and scrobble still works
	for i := 0; i < 20; i++ {
		clk.Advance(3 * time.Second)
		s.tick(tr)
	}
	if len(lf.scrobbles) != 1 {
		t.Fatalf("scrobbles = %v", lf.scrobbles)
	}
}

func TestScrobbleFailureAllowsRetry(t *testing.T) {
	s, lf, clk := setupScrobbler(t)
	tr := playing("Song", "Artist", 100)
	s.tick(tr)
	lf.failScrobble = true
	for i := 0; i < 20; i++ {
		clk.Advance(3 * time.Second)
		s.tick(tr)
	}
	if len(lf.scrobbles) != 0 {
		t.Fatal("expected no successful scrobble")
	}
	if s.state.scrobbled {
		t.Fatal("should not mark scrobbled on failure")
	}
	lf.failScrobble = false
	clk.Advance(3 * time.Second)
	s.tick(tr)
	if len(lf.scrobbles) != 1 {
		t.Fatalf("retry scrobble = %v", lf.scrobbles)
	}
}

func TestDeltaCapPreventsSleepCountingAsPlay(t *testing.T) {
	s, lf, clk := setupScrobbler(t)
	tr := playing("Song", "Artist", 100)
	s.tick(tr)
	// Simulate laptop sleep: 1 hour jump, one poll
	clk.Advance(1 * time.Hour)
	s.tick(tr)
	// Cap is max(10, poll*3) = 9 → wait PollInterval=3 so maxDelta=9
	// Actually maxDelta = 3*3 = 9
	if s.state.playedSeconds > 10 {
		t.Fatalf("playedSeconds too high after sleep: %v", s.state.playedSeconds)
	}
	if len(lf.scrobbles) != 0 {
		t.Fatalf("should not scrobble from sleep jump: %v", lf.scrobbles)
	}
}
