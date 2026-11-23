# Loom Interactive Public Demo

Isolated browser-only preview of the real current [Loom](../README.md) `ui/next` Preact/HTM interface.

The demo defaults to **English (`en`)**, supports live switching to **French (`fr`)** in Settings, and honors `?lang=en` / `?lang=fr` query-parameter handoff from the website and documentation.

## Security & Isolation Contract

- **Client-Side Simulation**: All `/api/*` HTTP calls, SSE streams, and WebSocket terminal sessions are intercepted in the browser by [`mock-engine.js`](mock-engine.js). No Go backend, local `llama-server`, GPU, or external API key is used.
- **100% Fictional Sandbox State**: Hostnames (`workstation.local`, `gpu-node.local`, `nas-node.local`, `codeman.demo.invalid`), models, discussions, Codeman agent sessions, Second Brain notes, quotas, and policy rules are synthetic fixtures with zero personal data or private LAN IP addresses.
- **Session-Scoped Edits & Reset**: Edits (messages, Second Brain Markdown notes, folders, preferences) stay inside browser storage and can be wiped at any time via the bottom-right **Demo → Reset** control (`lh-demo-intro-seen`, `loom_demo_*`, `loom-*`, `loom.*`).
- **3-Layer Search Engine Isolation**:
  1. `<meta name="robots" content="noindex, nofollow, noarchive">` in [`index.html`](index.html)
  2. `X-Robots-Tag: noindex, nofollow, noarchive` header + restrictive CSP in [`vercel.json`](vercel.json)
  3. Disallow-all [`robots.txt`](robots.txt)

## Syncing Real `ui/next` Assets

The static `demo/next/`, `demo/fonts/`, `demo/marked.min.js`, and `demo/favicon.svg` assets are synced directly from `internal/loom/ui/` and `docs/brand/` so Vercel serves the static directory with zero build step:

```bash
npm --prefix demo run sync
npm --prefix demo run sync:check
```

## Local Preview & Verification

```bash
npm --prefix demo run preview   # serves on http://127.0.0.1:2499
npm --prefix demo run check     # runs unit, contract, SEO, and hygiene checks
```

## Reproducible Screenshots & Social Preview

With Playwright installed in `tools/tests/ui-routes`, regenerate all `docs/screenshots/*.png`, `docs/screenshots/manifest.json`, and `docs/brand/social-preview.png` deterministically from this demo:

```bash
npm --prefix demo run screenshots
```

## Hosted Deployment (Vercel)

Connect the GitHub repository with `demo/` as the project **Root Directory** and the **Other** framework preset. Leave the Build Command empty to serve this static directory directly. `demo/vercel.json` enforces `noindex`, restrictive CSP, baseline security headers, and disables Dependabot preview deployments.
