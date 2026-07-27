<p align="center">
  <img src="assets/lfm.png" alt="lfm" width="160" />
</p>

# lfm — linkFM

**Monorepo** for the lfm product: a macOS Apple Music → Last.fm scrobbler with optional Discord Rich Presence, plus a product landing page.

```
lfm/
├── apps/
│   ├── cli/          # Go daemon (scrobbler + Discord RPC + launchd)
│   └── web/          # Product landing page (static)
├── assets/           # Shared brand assets
└── README.md
```

## Features (v2)

- Apple Music via AppleScript → official Last.fm API
- Correct scrobble rules (30s+, 50% or 4 minutes)
- Optional **Discord Rich Presence**
- LaunchAgent install (`lfm install`)
- Landing page ready for static hosting

## CLI (`apps/cli`)

### Build

```bash
cd apps/cli
go build -o lfm .
# optional stable path
mkdir -p ~/bin && cp lfm ~/bin/lfm
```

### Configure

`~/.config/lfm/config.yaml`:

```yaml
lastfm:
  api_key: "YOUR_KEY"
  api_secret: "YOUR_SECRET"
  # session_key from `lfm auth`

discord:
  enabled: true
  client_id: "YOUR_DISCORD_APP_ID"
  large_image: "lfm"   # upload this art key in Discord Developer Portal
  large_text: "lfm"

poll_interval: 3
log_level: info
```

| Env | Purpose |
|-----|---------|
| `LASTFM_API_KEY` / `LASTFM_API_SECRET` / `LASTFM_SESSION_KEY` | Last.fm |
| `DISCORD_ENABLED` | `true` / `false` |
| `DISCORD_CLIENT_ID` | Discord application ID |
| `DISCORD_LARGE_IMAGE` | Rich Presence large asset key |
| `POLL_INTERVAL` / `LOG_LEVEL` | Runtime |

### Usage

```bash
lfm auth       # browser auth → saves session_key
lfm run        # foreground daemon
lfm now        # debug current track
lfm install    # LaunchAgent (login)
lfm uninstall
lfm version    # 2.0.0
```

### Discord setup

1. Open [Discord Developer Portal](https://discord.com/developers/applications) → New Application.
2. Copy **Application ID** → `discord.client_id`.
3. (Optional) Rich Presence → Art Assets → upload icon as key `lfm`.
4. Enable in config, start Discord desktop, then `lfm run`.

Presence shows track name, artist/album, elapsed time while playing, and “Paused” when paused. If Discord isn’t running, lfm keeps scrobbling and retries quietly.

### Tests

```bash
cd apps/cli && go test ./...
```

## Web (`apps/web`)

Static landing page (no build step):

```bash
cd apps/web
# any static server, e.g.
python3 -m http.server 5173
# open http://localhost:5173
```

Deploy `apps/web` to GitHub Pages, Cloudflare Pages, Netlify, etc.

## License

MIT (or your choice).
