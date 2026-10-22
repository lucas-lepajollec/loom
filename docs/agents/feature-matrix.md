# Codeman feature matrix

Status of each Codeman capability in Loom's Agents module (Codeman 1.43.0).
Categories: **API** = native through the official API, **Partial** = supported
with documented gaps, **Terminal** = needs the native screen, **External** =
kept in the official Codeman interface.

Per-CLI signals: Claude = hooks (stop, answerable approvals, JSONL transcript);
Codex = rollout transcript, screen-read dialogs (answer in terminal);
OpenCode/Antigravity/Pi/Gemini = screen-detected idle/working, screen-segmented
transcript, approvals in terminal; Shell = terminal only.

| Capability | Codeman route / event | Category | Loom status |
| --- | --- | --- | --- |
| Connection, auth, version | `GET /me`, `GET /status` | API | Done: server-side Basic auth, sealed password, status/version, official UI link |
| Live session list and state | `GET /sessions`, `session:*` SSE | API | Done: rail grouped by folder, live state dots, SSE with heartbeat and full resync on reconnect |
| History and resume | `GET /sessions/unified`, `POST /sessions` + resume fields | API | Done: Claude exact, Codex by thread id, OpenCode/Pi/Grok/Copilot/OMP/DeepSeek latest; Gemini/Antigravity open a fresh session (no resume exposed) |
| Create session for any CLI | `GET /clis`, `POST /sessions`, `/interactive`, `/shell` | API | Done for every enabled CLI in the registry; model field where the schema accepts it |
| Send prompt | `POST /sessions/:id/input` | API | Done: single line + `\r`, `clientId`/`seq`, no double send, gated on readiness |
| Interrupt | input `Esc` | API | Done |
| Conversation view | `GET /sessions/:id/last-response?context=full` | Partial | Done; structured for Claude/Codex, screen-segmented otherwise; live screen shown when no prompt can be identified |
| Native terminal | `WS /ws/sessions/:id/terminal`, `GET /terminal` | API | Done: xterm.js, reconnect, resize with confirmed geometry, conversation/terminal/side-by-side |
| Tile grid (tmux-like) | same WebSocket per pane | API | Done: 2, 3 (main + stack), 4 or 6 panes, draggable dividers, focus and zoom with keyboard chords, drag-and-drop from the sidebar, conversation or terminal per pane, inspector follows focus, per-device layout |
| Approvals inbox | `GET /approvals`, `approval:*` SSE, `answer`, `dismiss`, `viewed` | Partial | Done for Claude (options/approve/deny); Codex and screen-only CLIs routed to the terminal, never auto-answered |
| Close / detach / restart | `DELETE /sessions/:id?killMux=`, `/interactive` | API | Done with confirmation |
| Rename | `PUT /sessions/:id/name` | API | Done |
| Attach files | `POST /sessions/:id/paste-image` (+ `?kind=video`) | API | Done: paste an image or drop files in the composer (thumbnails) or the terminal (path typed); images, documents, videos ≤ 64 MB through Loom; the gateway sends Codeman's `X-Codeman-CSRF` |
| Activity timeline | `GET /sessions/:id/run-summary` | API | Done |
| Git status and diffs | `GET /git-status`, `GET /git-diff` | API | Done, read-only, multi-repository |
| Files | `GET /files`, `GET /file-content` | API | Done: tree and text preview; editing stays in Codeman |
| Subagents | `GET /sessions/:id/subagents`, `/subagents/:id/transcript`, `subagent:*` | API | Done (Claude) |
| Session info and options | session state, `/auto-compact`, `/auto-clear` | API | Done: model, CLI version, tokens, cost, prompt cache, auto-compact/clear |
| Scheduled jobs | `/cron/jobs`, `/cron/runs`, `cron:*` | API | Done: create/edit/enable/run now/delete, run history |
| Control center | aggregate of the above | API | Done: needs-attention queue, board by state, next runs, latest launches, update check |
| Plan usage limits | `planUsage` in `/status`, `session:statusTelemetry` | API | Done: Activity → Quotas (Claude 5 h / 7 d, Codex), pace and resets, 14-day history sampled by Loom, quota chip in the Agents bar |
| Token stats | `GET /token-stats` | API | Done: tokens and Codeman's estimated cost per day |
| Server settings and CLI registry | `GET/PUT /settings`, `PUT /clis/:id`, `POST /clis/:id/install` | API | Done: sessions, Claude Code, Codex, tracking, terminal/system, CLI enable/install, advanced switches; voice, notifications, tunnel and respawn presets stay in Codeman |
| Reboot restore | `/reboot-restore`, `/restore`, `/dismiss` | API | Done |
| Folder browser | `GET /filesystem/browse` | API | Done (Codeman host's view) |
| Respawn controller, Ralph loops, orchestrator, plans | `/respawn/*`, `/ralph-*`, `/orchestrator/*`, `/plan/*` | External | Reachable through the gateway; UI kept in Codeman (deep link) |
| Worktrees, cases, linked/remote/Docker cases | `/cases/*`, `/worktrees`, `/docker-*`, `/remote-hosts` | External | Reachable; UI in Codeman |
| Custom model endpoints, custom CLI entries, MCP sync runs | `/model-endpoints`, `POST /clis`, `/mcp-sync` | External | Switched on from Loom's Settings tab; their editors stay in Codeman |
| Web tabs, HTML view, voice dictation, Read My Mind | `/webviews`, `/html-view`, `/ws/voice`, `/readmymind` | External | HTML view and voice need Codeman's own origin; kept in Codeman |
| Push/webhook notifications, tunnel/QR, multi-user admin, self-update | `/push`, `/webhook`, `/tunnel`, `/admin`, `/system/update` | External | Kept in Codeman; Loom shows update availability |
| Codeman login/logout, hooks | `/auth/*`, `/logout`, `/hook-event` | Blocked | Never relayed by Loom |

Everything marked External is still callable by the gateway, so a later Loom
screen needs no backend change.
