# Agents module architecture

Loom's Agents module is an interface for an independent
[Codeman](https://github.com/Ark0N/Codeman) server. Codeman runs the native
coding CLIs, their tmux panes, transcripts and schedules; Loom displays and
drives them through Codeman's public API. Loom does not execute agents, add an
agent SDK, call model APIs for agents or re-implement sessions.

```
browser ── Loom auth + origin check ──▶ Loom gateway ── Basic auth ──▶ Codeman /api/v1
   │                                        │  SSE relay (flushed)          /api/v1/events
   └── same-origin WebSocket ─────────────▶ │  WS bridge (frames as-is)     /ws/sessions/:id/terminal
```

## Backend: `internal/loom/codeman.go`

| Route | Role |
| --- | --- |
| `GET/POST /api/codeman/config` | Codeman address, username and optional official-UI address. The password is sealed with Loom's installation key (`secrets/providers/codeman-gateway.sealed`, 0600) and never returned |
| `GET /api/codeman/status` | Reachability, authentication result, version, official UI link for this browser |
| `GET /api/codeman/discover` | Local Codeman installs (default port, systemd/launchd service, data-dir `.env`, environment) probed in parallel; reports whether a password was found, never the password |
| `POST /api/codeman/connect`, `POST /api/codeman/disconnect` | Test then save (address, user, sealed password, or the locally found one); forget the connection |
| `/api/codeman/v1/*` | Relay of any method to `/api/v1/*` with Basic auth added server-side. Browser cookies, `Authorization` and `Origin` are not forwarded. Codeman 401 becomes `502 CODEMAN_AUTH`; unreachable becomes `502 CODEMAN_UNREACHABLE`. `auth/*`, `logout` and `hook-event` are refused. SSE responses are flushed chunk by chunk. Bodies are limited to 64 MB |
| `/api/codeman/ws/{id}` | Same-origin WebSocket bridged to Codeman's terminal socket; close codes (4009 ended, 4004 missing) are forwarded |

All routes require Loom authentication and Go's cross-origin protection.
Streams and sockets are bound to the authorizing grant and close when access is
revoked. Launch routes (`sessions`, `quick-start`, `.../interactive`,
`.../shell`, `cron/jobs`, `cron/jobs/:id/run`, `ralph-loop/start`,
`reboot-restore/restore`) first pass Loom's policy subject `codeman.session`,
allowed by default and adjustable in Settings → Permissions.

## Frontend: `ui/next/js/features/agents/`

| File | Responsibility |
| --- | --- |
| `client.js` | Only network module: envelope unwrapping, errors, gateway config, SSE reader, prompt idempotence (`clientId` per page load, monotonic `seq`) |
| `store.js` | Domain state: connection, sessions, history, CLI registry, approvals, plan usage, Codeman settings; SSE reducers; per-CLI capability table; derived session state; app-wide presence (`useAgentsPresence`) used by the sidebar |
| `actions.js` | Intentions → official calls: create, resume, interrupt, close, rename, answer approvals |
| `labels.js` | Literal translation keys for variable values |
| `page.js`, `nav.js` | Route `#/agents[/s/<id>|/control|/cron|/settings]` (`/grid` opens the grid), single view or grid, layout picker, quota chip, session header |
| `side.js` | Section of Loom's sidebar: live sessions, history to resume, new-session dialog host |
| `layout.js`, `grid.js` | Per-device layout (1, 2, 3, 4 or 6 panes, cells, focus, zoom, divider fractions) and the tmux-like grid |
| `rail.js` | Full list (home screen, phone): sessions by folder, history, search, reboot restore |
| `attach.js` | Pasted images and dropped files: upload to Codeman, path inserted in the prompt or typed in the terminal |
| `settings.js` | Codeman server settings (`/settings`, `/clis`) and the connection |
| `quotas.js` | Activity → Quotas: live plan usage, Loom's sampled history, Codeman token stats |
| `conversation.js` | Transcript blocks, activity folds, approvals, composer, live screen before the first identifiable prompt |
| `terminal.js` | xterm.js terminal: screen read, WebSocket attach, resize, reconnect |
| `inspector.js` | Activity, Git, files, subagents, session options |
| `control.js`, `cron.js`, `connection.js`, `new-session.js` | Control center, scheduling, connection, new session |

The module uses Loom's tokens and components (`.composer`, `.thread`, `.seg`,
`Modal`, `Drawer`, `TabNav`), its i18n dictionaries and its CSS layering
(`css/agents.css`, class prefix `cm-`). It shares no state with Local/Cloud
discussions or the inference engine.

## Choices

- **Gateway, not iframe.** Codeman's CORS and CSP forbid cross-origin browser
  clients, and secrets stay server-side.
- **Generic relay plus typed client.** The backend does not mirror Codeman's
  200 routes; the browser client names the routes it uses. A Codeman minor
  release adding endpoints needs no Loom backend change; a `/api/v2` would.
- **Honest per-CLI fidelity.** Structured data is shown only where Codeman
  produces it; otherwise the native screen is the view.
- **No automatic answers.** Approvals and dialogs are answered only by a click
  (Claude) or in the terminal (others).
