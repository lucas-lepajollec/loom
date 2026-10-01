# Loom Interactive Public Demo

Isolated browser simulation of [Loom](https://loom.lucas-homelab.fr). The demo interface is in French, even when linked from another locale of the site.

## Security & Isolation Contract

In accordance with Ecosystem Pass 09 (Public Demo Isolation):
- **Client-Side Simulation**: Product actions are simulated in the browser via `mock-engine.js`; this is still a website served over the network, not a local product install.
- **Zero Personal Data**: All models, paths, chats, and presets are synthetic fixtures.
- **No Product Backend Requirement**: Does not require `llama-server`, a local GPU, or external model downloads. Network access is still required to load the hosted demo.
- **Pass 9 Chrome**: Includes the standard `<dialog>` introduction on first visit (`lh-demo-intro-seen`) and the persistent bottom-right demo chip with instant reset.
- **Search Engine Isolation**: Configured with `X-Robots-Tag: noindex, nofollow, noarchive` in `vercel.json`.

## Local Preview

```bash
cd demo
npm run preview
```
Serves the demo on `http://127.0.0.1:2499`.

## Hosted deployment

For Vercel, connect the `loom` GitHub mirror with `demo/` as the project Root Directory and the **Other** preset. Leave the Build Command empty to serve this static directory as-is. The checked-in `demo/vercel.json` applies noindex and baseline security headers. Attach `demo.loom.lucas-homelab.fr` and verify the intro, reset button and that no real engine or model download starts.
