# Agents module validation

Validated on 22 October 2026 against a live **Codeman 1.43.0** (Linux, systemd
user service, password authentication) with Loom built from this branch on an
isolated data directory. Only checks listed here were run.

## Automated

| Check | Result |
| --- | --- |
| `go test -short ./...` (with a valid UTF-8 locale) | pass |
| `go test -race -short ./...` | pass |
| `go vet ./...`, Windows and macOS builds | pass |
| `make check-ui` (module syntax, 123 UI tests incl. i18n parity/scan) | pass |
| `python3 tools/check-doc-links.py` | pass |
| `codeman_test.go` against a fake Codeman | pass: password never returned or stored in plain config; Basic auth added server-side; browser cookie/Authorization/Origin not forwarded; Codeman 401 → 502 `CODEMAN_AUTH`; unreachable → 502; blocked routes and `..` never reach Codeman; cross-origin writes refused; empty POST bodies forwarded as empty; SSE first frame arrives unbuffered; WebSocket frames relayed both ways and close code 4009 forwarded; launch routes denied by a `codeman.session` deny rule while reads continue |

`TestShellOutputIsBoundedDuringExecution` fails on hosts whose `LC_ALL` names a
missing locale (shell warnings pollute output); it fails identically on `main`
and passes with `LC_ALL=C.UTF-8`.

## One-click connection

On a fresh Loom data directory, Settings → Agents found the VM's Codeman
(systemd service with a password in its override) and connected in one click;
a wrong password typed in the manual form showed "rejected" and saved nothing;
the Agents page then loaded its sessions. Desktop and phone, no console errors.
Unit tests cover systemd `Environment=` quoting, `.env` parsing, discovery
without password leakage, test-before-save and disconnect.

## Live instance

| Scenario | Result |
| --- | --- |
| Status with and without credentials | `rejected` without password, `ok` + version 1.43.0 after saving; sealed file mode 0600 |
| Shell session through the gateway | create → `/shell` → input with `clientId`/`seq` → unique marker matched in 57 ms |
| Terminal WebSocket bridge | input round trip, resize confirmed (`zc 100×30`); cross-origin upgrade refused |
| SSE relay | live `session:updated` after a rename; `?sessions=` filter removes the terminal stream (33 → 0 frames / 4 s) |
| Start each target CLI (no prompt) | Claude Code, Codex, OpenCode, Antigravity, Pi all got a PID. Codex showed its update dialog and Antigravity its folder-trust dialog: both require an on-screen answer, which the live-screen card provides |
| Full browser flow, Claude Code (Haiku, one tiny prompt) | New session dialog → live start screen → composer enabled → prompt → user bubble → structured answer "PONG" in 6 s → side-by-side terminal → close from the menu; session gone from Codeman; no console errors |
| Browser flow, Pi (local model) | Prompt delivered; the model host had no model loaded (`502 engine unavailable`), shown on Pi's live screen. Codeman's pane segmentation did not separate Pi's prompt from its output, so the live screen stays the view (documented limitation) |
| Visual review | Desktop 1440×900 dark and light, phone 390×844: sessions, conversation, terminal, inspector (activity/Git/info), control, mosaic, scheduling and its form, connection, new session. No horizontal overflow on phone pages |

Not exercised: answerable Claude permission prompts (the test instance runs
Claude with Codeman's default permission bypass), Codex approval dialogs during
a turn, cron executions, subagents with live data, multi-user Codeman, remote
or Docker cases, WebKit/iOS devices.

## Remaining limits

- Conversation fidelity depends on Codeman per CLI: structured for Claude and
  Codex, screen-segmented and sometimes unusable for others (terminal shown).
- Prompts are single-line (Codeman contract); newlines are sent as spaces.
- Uploads through Loom are capped at 64 MB (Codeman accepts larger videos).
- Respawn, Ralph loops, orchestrator, worktrees, case management, custom model
  endpoints, web tabs, HTML view, voice dictation and Codeman administration
  are reachable through the gateway but their screens remain in Codeman.
- The terminal theme follows the Loom theme at load; switching theme applies
  on the next attach.

## Sidebar, grid, settings, quotas (22 October 2026, later)

Run against the same Codeman 1.43.0 with two throwaway shell sessions (deleted
afterwards) and the live Claude session, on a rebuilt dev binary.

| Scenario | Result |
| --- | --- |
| Sidebar | Live sessions listed under Agents next to Projects and Recents; history to resume; current session and grid membership marked; phone drawer |
| Grid | 4 panes → 3 sessions + one free pane; 3-pane layout divider dragged (fractions 0.62/1.38 kept); Alt+Shift+→ moved focus; Alt+Shift+Z zoomed and restored; inspector follows the focused pane; Ctrl+Shift+G back to one session; terminals receive one resize per drag and repaint cleanly |
| Paste an image | Composer: thumbnail chip, upload done, prompt not sent. Terminal: the uploaded path `.codeman-uploads/paste-…png` typed in the shell. Before the gateway fix both were refused by Codeman (`CSRF check failed`) |
| Codeman settings | Loaded from `/settings`; selects, switches, CLI list with install buttons gated by CLI management |
| Quotas | Claude 5 h and 7 d and Codex 7 d shown live with resets and pace; history chart; tokens per day; phone layout |
| Console and overflow | No console errors; no horizontal overflow on phone pages (`#/agents`, settings, quotas, sessions) |

Follow-up, same evening: terminal xterm screen fits its box (no column under the
scrollbar at 1920 px); the wheel scrolled the live Claude session's own history
up and back down; the quick Shell button created a shell in the current
session's folder (deleted afterwards).

`codeman_quota_test.go` covers sampling (percentages and resets only, file
mode 0600), deduplication and expiry; `TestCodemanUploadPassesCodemanCSRFCheck`
covers the upload header.
