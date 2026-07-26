# lfm - lastFM

Lightweight **macOS** daemon that polls Apple Music via AppleScript and scrobbles to [Last.fm](https://www.last.fm).

- Single static binary, no CGO
- Official Last.fm API only (no audio sniffing)
- Correct scrobble rules: >30s duration, and 50% played **or** 4 minutes

## Requirements

- macOS with the **Music** app
- Go 1.22+ (to build)
- Last.fm [API key + secret](https://www.last.fm/api/account/create)

## Install

```bash
go build -o lfm .
```

## Configuration

Create `~/.config/lfm/config.yaml`:

```yaml
lastfm:
  api_key: "YOUR_KEY"
  api_secret: "YOUR_SECRET"
  # session_key filled by `auth`
poll_interval: 3
min_track_duration: 30
scrobble_threshold: 0.5
scrobble_max_seconds: 240
log_level: info
```

Environment variables override the file:

| Variable | Purpose |
|----------|---------|
| `LASTFM_API_KEY` | API key |
| `LASTFM_API_SECRET` | API secret |
| `LASTFM_SESSION_KEY` | Session after auth |
| `POLL_INTERVAL` | Seconds between polls |
| `LOG_LEVEL` | debug / info / warn / error |

## Usage

```bash
# 1. Authorize (opens a Last.fm URL; saves session_key to config)
./lfm auth

# 2. Run in the foreground
./lfm run

# Debug: print what Music reports
./lfm now

./lfm version
```

Optional config path: `-c /path/to/config.yaml`

### Signals

`run` handles **SIGINT** / **SIGTERM** and exits cleanly.

## How scrobbling works

1. Poll Music every `poll_interval` seconds (default 3).
2. On a new playing track → `track.updateNowPlaying`.
3. Accumulate **wall-clock** time while `player state` is playing (pauses freeze the counter).
4. When played time ≥ 50% of duration **or** ≥ 240s (and duration > 30s) → one `track.scrobble`.
5. Track change or full stop resets state.

## Tests

```bash
go test ./...
```

## launchd (optional)

A sample plist can load the binary as a user agent; not installed by this project yet. Run under `launchctl` pointing at `lfm run` once auth is done.

## License

MIT (or your choice).
