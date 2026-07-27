package main

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Track is the currently observed Apple Music state.
type Track struct {
	Name     string
	Artist   string
	Album    string
	Duration float64 // seconds
	Position float64 // seconds
	State    string  // "playing" | "paused" | "stopped"
}

// IsEmpty reports whether no meaningful track is present.
func (t Track) IsEmpty() bool {
	return t.Name == "" && t.Artist == "" || t.State == "stopped"
}

// SameTrack reports identity by name + artist (case-sensitive as returned).
func (t Track) SameTrack(other Track) bool {
	return t.Name == other.Name && t.Artist == other.Artist
}

// MusicClient fetches the current track from Apple Music.
type MusicClient interface {
	GetCurrentTrack() (Track, error)
}

// AppleScriptMusic runs osascript against the Music app.
type AppleScriptMusic struct {
	// Exec runs a command; defaults to exec.CommandContext when nil.
	// Used in tests to inject a fake osascript.
	Exec    func(ctx context.Context, name string, args ...string) ([]byte, error)
	Timeout time.Duration
}

// defaultAppleScript is a single-call script that returns pipe-separated fields:
// state|name|artist|album|duration|position
//
// Note: do not use the identifier `st` — on current macOS AppleScript it is a
// syntax error ("Expected expression but found st"), which made every poll
// soft-fail as stopped.
const defaultAppleScript = `
tell application "Music"
    if not (exists current track) then
        return "stopped||||0|0"
    end if
    set t to current track
    set playerStateStr to (player state as string)
    set trackName to name of t
    set trackArtist to artist of t
    set trackAlbum to album of t
    set trackDur to duration of t
    set trackPos to player position
    return playerStateStr & "|" & trackName & "|" & trackArtist & "|" & trackAlbum & "|" & trackDur & "|" & trackPos
end tell
`

// NewAppleScriptMusic creates a client with a 2s osascript timeout.
func NewAppleScriptMusic() *AppleScriptMusic {
	return &AppleScriptMusic{Timeout: 2 * time.Second}
}

// GetCurrentTrack queries Music.app. On failure (Music not running, non-library
// tracks, timeout), returns a zero Track with State="stopped" and no error so
// the daemon keeps running.
func (m *AppleScriptMusic) GetCurrentTrack() (Track, error) {
	timeout := m.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	out, err := m.run(ctx, "osascript", "-e", defaultAppleScript)
	if err != nil {
		// Soft failure: treat as stopped for this poll cycle.
		return Track{State: "stopped"}, nil
	}
	return parseTrackOutput(string(out))
}

func (m *AppleScriptMusic) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if m.Exec != nil {
		return m.Exec(ctx, name, args...)
	}
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.Output()
}

// parseTrackOutput parses "state|name|artist|album|duration|position".
// Fields may be empty; duration/position default to 0 on parse error.
func parseTrackOutput(s string) (Track, error) {
	s = strings.TrimSpace(s)
	// AppleScript may include trailing newlines; split with enough parts.
	parts := strings.Split(s, "|")
	if len(parts) < 6 {
		// Incomplete / unexpected → treat as stopped.
		return Track{State: "stopped"}, nil
	}

	// Re-join if name/artist/album somehow contained pipes? Spec uses pipes;
	// Music metadata rarely has them. If more than 6 parts, rejoin middle carefully.
	if len(parts) > 6 {
		// state | name... | artist | album | dur | pos  is ambiguous;
		// take first as state, last two as dur/pos, third-last album, rest split.
		// Simpler: first, then join middle until we have exactly 6 logical fields
		// by taking last 2 as numbers, last-3 as album is fragile. Keep first 4
		// and last 2 if extra pipes appear only in album (common enough).
		// For robustness: state=parts[0], pos=last, dur=last-1, album=last-2,
		// artist=last-3, name=join(parts[1:len-3]).
		state := parts[0]
		pos := parts[len(parts)-1]
		dur := parts[len(parts)-2]
		album := parts[len(parts)-3]
		artist := parts[len(parts)-4]
		name := strings.Join(parts[1:len(parts)-4], "|")
		parts = []string{state, name, artist, album, dur, pos}
	}

	dur, _ := strconv.ParseFloat(strings.TrimSpace(parts[4]), 64)
	pos, _ := strconv.ParseFloat(strings.TrimSpace(parts[5]), 64)

	state := strings.TrimSpace(parts[0])
	// Normalize AppleScript "player state as string" variants.
	switch strings.ToLower(state) {
	case "playing", "paused", "stopped":
		state = strings.ToLower(state)
	default:
		// e.g. empty or unexpected
		if state == "" {
			state = "stopped"
		}
	}

	return Track{
		State:    state,
		Name:     parts[1],
		Artist:   parts[2],
		Album:    parts[3],
		Duration: dur,
		Position: pos,
	}, nil
}

// String implements fmt.Stringer for CLI `now` output.
func (t Track) String() string {
	if t.State == "stopped" || (t.Name == "" && t.Artist == "") {
		return "stopped"
	}
	return fmt.Sprintf("%s: %s — %s [%s] (%.0f/%.0fs)",
		t.State, t.Artist, t.Name, t.Album, t.Position, t.Duration)
}
