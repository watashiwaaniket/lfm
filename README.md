<p align="center">
  <img src="assets/lfm.png" alt="lfm" width="160" />
</p>

# lfm — linkFM

**Monorepo** for the lfm product: a macOS Apple Music → Last.fm scrobbler with optional Discord Rich Presence, plus a product landing page.

**Site:** [lfm.aniketdhakane.xyz](https://lfm.aniketdhakane.xyz)

```
lfm/
├── apps/
│   ├── cli/          # Go daemon (scrobbler + Discord RPC + launchd)
│   └── web/          # Landing page → Cloudflare Workers
├── assets/           # Shared brand assets
└── README.md
```

## Features (v2)

- Apple Music via AppleScript → official Last.fm API
- Correct scrobble rules (30s+, 50% or 4 minutes)
- Optional **Discord Rich Presence** (album art, Listening activity)
- LaunchAgent install (`lfm install`)
- Landing page on Cloudflare Workers (`lfm.aniketdhakane.xyz`)

---

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
  large_image: "lfm"   # optional fallback asset key in Developer Portal
  large_text: "lfm"

poll_interval: 3
log_level: info
```

| Env | Purpose |
|-----|---------|
| `LASTFM_API_KEY` / `LASTFM_API_SECRET` / `LASTFM_SESSION_KEY` | Last.fm |
| `DISCORD_ENABLED` | `true` / `false` |
| `DISCORD_CLIENT_ID` | Discord application ID |
| `DISCORD_LARGE_IMAGE` | Fallback Rich Presence asset key |
| `POLL_INTERVAL` / `LOG_LEVEL` | Runtime |

### Usage

```bash
lfm auth       # browser auth → saves session_key
lfm run        # foreground daemon (scrobble + Discord)
lfm now        # debug current track
lfm install    # LaunchAgent (login) — includes Discord if configured
lfm uninstall
lfm version    # 2.0.0
```

One process does **both** Last.fm scrobbling and Discord presence. Prefer either LaunchAgent **or** `lfm run`, not both.

### Discord setup (step by step)

Discord presence is optional but recommended. It runs inside the same `lfm run` / LaunchAgent process.

1. **Create an app**  
   Open the [Discord Developer Portal](https://discord.com/developers/applications) → **New Application** → name it (e.g. `lfm` or `Apple Music`).  
   The name appears as “Listening to **&lt;name&gt;**” in Discord.

2. **Copy the Application ID**  
   General Information → **Application ID** → paste into config:

   ```yaml
   discord:
     enabled: true
     client_id: "1234567890123456789"
   ```

   Or: `export DISCORD_ENABLED=true DISCORD_CLIENT_ID=...`

3. **(Optional) Fallback icon**  
   Rich Presence → **Art Assets** → upload `assets/lfm.png` with key **`lfm`**.  
   Used only when album art cannot be resolved. Live covers come from Last.fm / iTunes automatically.

4. **Desktop Discord must be open**  
   Presence uses local IPC. Browser-only Discord is not enough.

5. **Activity privacy**  
   Discord → Settings → **Activity Privacy** → allow others to see your activity (as you prefer).

6. **Run**

   ```bash
   # Foreground test
   lfm run

   # Or background (reload agent after config changes)
   lfm install
   tail -f ~/Library/Logs/lfm/stderr.log
   ```

   You should see `discord rich presence enabled` and `scrobbler started … discord=true`.

7. **What shows on your profile**

   | Field | Content |
   |-------|---------|
   | Image | Album art (auto) |
   | Title line | Discord app name (“Listening to …”) |
   | Details | Song name |
   | State | Artist (or `Paused · Artist`) |
   | Button | Apple Music powered by lfm |
   | Timer | Progress when duration is known |

8. **Troubleshooting**

   | Symptom | Fix |
   |---------|-----|
   | No presence | Discord desktop open? `enabled: true` + correct `client_id`? |
   | Only scrobbling works | Restart agent after editing config: `lfm install` |
   | LaunchAgent exits (`CODESIGNING`) | `lfm install` ad-hoc signs the binary; rebuild then reinstall |
   | Two scrobblers | Don’t run terminal `lfm run` while the agent is up |
   | No cover art | Needs network; falls back to `large_image` portal asset |

### Tests

```bash
cd apps/cli && go test ./...
```

---

## Web (`apps/web`)

Static landing page on **Cloudflare Workers** (Static Assets), custom domain **`lfm.aniketdhakane.xyz`**.

### Local

```bash
cd apps/web
npm install
npm run dev
# → http://localhost:8787
```

### Deploy

Prerequisites:

- Cloudflare account with zone **aniketdhakane.xyz** (you already have this)
- Logged in once: `npx wrangler login` (or `CLOUDFLARE_API_TOKEN`)

```bash
cd apps/web
npm install
npm run deploy
```

`wrangler.jsonc` sets:

- Worker name: `lfm-web`
- Assets directory: `./public`
- Custom domain route: `lfm.aniketdhakane.xyz` (`custom_domain: true`)

On first deploy, Cloudflare creates the DNS record for the subdomain. No manual CNAME is required when using Workers Custom Domains (do **not** keep a conflicting CNAME for `lfm` if one already exists — remove it first).

### Project layout

```
apps/web/
├── public/                 # served as the site root
│   ├── index.html
│   ├── styles.css
│   ├── lfm.png
│   └── discord-showcase.png
├── wrangler.jsonc
├── package.json
└── README.md
```

---

## License

MIT.
