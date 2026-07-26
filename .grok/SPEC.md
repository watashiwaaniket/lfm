# Apple Music → Last.fm Scrobbler (Go)

**Lightweight, single-binary macOS daemon** that polls Apple Music via AppleScript and scrobbles to Last.fm.

## Goals

- Zero third-party "sniffing" apps.
- Pure local observation of the Music app + official Last.fm API.
- Single static binary, minimal dependencies.
- Correct Last.fm scrobbling rules.
- Simple CLI for auth + running as a background process.
- Easy to install via launchd later.

## Non-Goals

- Windows / Linux support (macOS + AppleScript only).
- iOS / MusicKit integration.
- Fancy GUI or menu-bar app.
- Offline queue persistence beyond a simple in-memory + optional file retry (keep it lightweight).
- Discord presence, notifications, or other extras (can be added later).

## Tech Stack

- Go 1.22+
- Standard library only for HTTP, crypto, os/exec, etc.
- Optional: `gopkg.in/yaml.v3` for config file (acceptable).
- No CGO.
- Target: macOS (Apple Silicon + Intel).

## Project Structure

```
lfm/
├── main.go              # CLI entrypoint (auth / run)
├── config.go            # Config loading (env + YAML)
├── music.go             # AppleScript client
├── lastfm.go            # Last.fm API client (auth, nowplaying, scrobble)
├── scrobbler.go         # Core polling + scrobble decision logic
├── go.mod
├── go.sum
├── SPEC.md              # This file
└── README.md            # User-facing docs (generated later)
```

Keep everything in the root package (`package main`) for simplicity. No internal/ packages needed for this lightweight version.

## Configuration

Support both environment variables and a YAML config file (`~/.config/lfm/config.yaml`).

### Required fields

```yaml
lastfm:
  api_key: "..."
  api_secret: "..."
  session_key: "..."   # obtained via `auth` command
```

### Optional

```yaml
poll_interval: 3          # seconds (default 3)
min_track_duration: 30    # seconds – ignore shorter tracks
scrobble_threshold: 0.5   # 50%
scrobble_max_seconds: 240 # 4 minutes
log_level: info           # debug / info / warn / error
```

Environment variables take precedence:

- `LASTFM_API_KEY`
- `LASTFM_API_SECRET`
- `LASTFM_SESSION_KEY`
- `POLL_INTERVAL`

## CLI Commands

```bash
lfm auth     # Interactive auth flow
lfm run      # Start the polling daemon (foreground)
lfm now      # Print currently playing track (for debugging / tmux)
lfm version
```

### Auth flow (`auth`)

1. Read `api_key` + `api_secret` from config/env.
2. Call `auth.getToken`.
3. Print URL: `https://www.last.fm/api/auth/?api_key=...&token=...`
4. Wait for user to press Enter after authorizing in browser.
5. Call `auth.getSession` with the token.
6. Save `session_key` (and username) back into the YAML config file.

### Run flow (`run`)

- Load config.
- Create Music client + Last.fm client.
- Enter polling loop.
- Handle SIGINT / SIGTERM gracefully.

## AppleScript Client (`music.go`)

### Track info needed

```go
type Track struct {
    Name     string
    Artist   string
    Album    string
    Duration float64  // seconds
    Position float64  // seconds
    State    string   // "playing" | "paused" | "stopped"
}
```

### Recommended AppleScript (single call)

```applescript
tell application "Music"
    if not (exists current track) then
        return "stopped||||0|0"
    end if
    set t to current track
    set st to player state as string
    set nm to name of t
    set ar to artist of t
    set al to album of t
    set dur to duration of t
    set pos to player position
    return st & "|" & nm & "|" & ar & "|" & al & "|" & dur & "|" & pos
end tell
```

Notes:
- Use `osascript -e '...'` via `os/exec`.
- Parse the pipe-separated result.
- Handle cases where Music is not running or `current track` fails (common on newer macOS for non-library tracks). Return a zero Track with State="stopped" instead of erroring hard.
- Timeout the osascript call (e.g. 2 seconds).

## Last.fm Client (`lastfm.go`)

### Required methods

- `GetToken() (token string, err error)`
- `AuthURL(token string) string`
- `GetSession(token string) (username, sessionKey string, err error)`
- `UpdateNowPlaying(artist, track, album string, durationSec int) error`
- `Scrobble(artist, track, album string, timestamp int64, durationSec int) error`

### Implementation notes

- Endpoint: `https://ws.audioscrobbler.com/2.0/`
- All write methods are POST + form-urlencoded.
- Signature (`api_sig`): sort parameters alphabetically, concatenate `key+value`, append secret, MD5 hex.
- Always send `api_key`, `sk` (when available), `api_sig`, `method`, and `format=json`.
- Use `artist[0]`, `track[0]`, `timestamp[0]` etc. for scrobble (even for single track).
- Reasonable HTTP timeout (10–15 s).
- Surface Last.fm error codes/messages cleanly.

## Core Scrobbler Logic (`scrobbler.go`)

### State to track

```go
type scrobbleState struct {
    current       Track
    startedAt     time.Time   // when we first saw this track as playing
    playedSeconds float64     // accumulated playing time (handles pauses)
    nowPlayingSent bool
    scrobbled     bool
}
```

### Rules (official Last.fm)

1. Track duration must be > 30 seconds.
2. Scrobble when **either**:
   - playedSeconds ≥ 50% of duration, **or**
   - playedSeconds ≥ 240 seconds
3. Only scrobble once per play (reset on track change or full stop).
4. Send `updateNowPlaying` as soon as a new track starts playing (and optionally on resume).
5. Ignore very short seeks / track changes under ~3–5 seconds to avoid noise.

### Polling loop (every `poll_interval` seconds)

```
track = music.GetCurrentTrack()

if track.State != "playing" {
    // accumulate nothing, but keep state if paused
    continue
}

if track is different from previous (name+artist) {
    // reset state
    // send UpdateNowPlaying
}

// accumulate played time since last poll (using delta or Position)
// if threshold reached and !scrobbled → Scrobble() and mark scrobbled
```

Use wall-clock accumulation while `State == "playing"` rather than trusting `player position` exclusively (handles seeking better).

## Error Handling & Logging

- Log at configurable level (default `info`).
- Never crash the daemon on a single failed scrobble or AppleScript error.
- On Last.fm network/API errors: log and continue (optional simple retry queue can be added later).
- On AppleScript failure: treat as "stopped" for that cycle.

## Build & Run

```bash
go build -o lfm .
./lfm auth
./lfm run
```

Later: provide a sample `launchd` plist so users can `launchctl load` it.

## Acceptance Criteria

- [ ] `auth` successfully obtains and saves a session key.
- [ ] While Apple Music is playing a track > 30 s, `updateNowPlaying` is sent within one poll cycle.
- [ ] After 50% or 4 minutes of actual play time, exactly one scrobble is submitted.
- [ ] Pausing / resuming does not cause double scrobbles.
- [ ] Changing track resets state correctly.
- [ ] Binary has no external runtime dependencies beyond macOS + Music.app.
- [ ] Config can be driven purely by environment variables.

## Future Extensions (out of scope for v1)

- Persistent offline scrobble queue (SQLite or JSON file).
- `launchd` install/uninstall commands.
- Discord Rich Presence.
- Support for other players via MediaRemote (private framework).
- Windows SMTC backend.

---

**Implementation priority order for the coding agent:**

1. `lastfm.go` (auth + scrobble methods) – pure and testable.
2. `music.go` (AppleScript).
3. `config.go`.
4. `scrobbler.go` (state machine).
5. `main.go` (CLI wiring).
6. Basic README.

Keep the code clean, well-commented on the non-obvious parts (signature generation, scrobble threshold logic, AppleScript edge cases), and prefer readability over cleverness.
