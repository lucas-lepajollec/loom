# Loom Interactive Public Demo

Isolated, 100% client-side web demonstration of [Loom](https://github.com/lucas-lepajollec/loom).

## Security & Isolation Contract

In accordance with Ecosystem Pass 09 (Public Demo Isolation):
- **100% Client-Side Simulation**: Runs entirely inside browser memory via `mock-engine.js`.
- **Zero Personal Data**: All models, paths, chats, and presets are synthetic fixtures.
- **No Backend Requirement**: Does not require `llama-server`, a local GPU, or network access.
- **Pass 9 Chrome**: Includes the standard `<dialog>` introduction on first visit (`lh-demo-intro-seen`) and the persistent bottom-right demo chip with instant reset.
- **Search Engine Isolation**: Configured with `X-Robots-Tag: noindex, nofollow, noarchive` in `vercel.json`.

## Local Preview

```bash
cd demo
npm run preview
```
Serves the demo on `http://127.0.0.1:2499`.
