# lfm web

Product landing page for **https://lfm.aniketdhakane.xyz**, hosted on **Cloudflare Workers** (Static Assets).

## Local

```bash
cd apps/web
npm install
npm run dev
# → http://localhost:8787
```

Or without Wrangler:

```bash
cd public && python3 -m http.server 5173
```

## Deploy

Requires Cloudflare auth (`npx wrangler login`) on an account that owns **aniketdhakane.xyz**.

```bash
cd apps/web
npm install
npm run deploy
```

`wrangler.jsonc` attaches the Worker custom domain `lfm.aniketdhakane.xyz` (Cloudflare manages the DNS record on deploy).

## Layout

```
public/                 # asset root (served as-is)
├── index.html
├── styles.css
├── lfm.png             # brand mark
└── discord-showcase.png
wrangler.jsonc
package.json
```
