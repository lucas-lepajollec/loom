# Codeman audit

This audit records what Loom's Agents module relies on. It was written against
Codeman **1.43.0** (git install, commit `3907728`), read from its source,
documentation and a live instance. Codeman is an independent server; Loom does
not vendor, fork or patch it.

## What Codeman is

Codeman runs native coding CLIs (Claude Code, Codex, OpenCode, Antigravity,
Pi, Gemini, Grok, Copilot, DeepSeek, OMP, a plain shell) inside persistent tmux
panes. It owns sessions, their lifecycle, transcripts, approvals, subagent
discovery, scheduled jobs, respawn/Ralph loops, the orchestrator, worktrees,
file and Git inspection, push/webhook notifications and multi-user access.
Every agent keeps its own CLI login and subscription.

## Public surface used by Loom

Covered by Codeman's SemVer policy (`docs/versioning-policy.md`):

| Surface | Contract |
| --- | --- |
| HTTP API | `/api/v1/*`, envelope `{success,data}` / `{success:false,error,errorCode}`; additive changes only within v1 |
| SSE | `GET /api/v1/events`, 149 stable `domain:action` names, `sse:heartbeat` every 15 s, `?sessions=` filters only the terminal stream |
| Terminal | `GET /ws/sessions/:id/terminal` WebSocket, JSON frames `o`/`c`/`r`/`ia`/`zc` out, `i`/`z` in |
| Deep link | `/#session=<id>` opens one session in the official UI |
| Auth | HTTP Basic (`CODEMAN_USERNAME`/`CODEMAN_PASSWORD`) or `codeman_session` cookie |

Not used: `~/.codeman` state files, internal TypeScript modules, the hook
endpoint, experimental surfaces.

## Behaviours that shape the integration

- **CORS is loopback-only and browsers on other origins are refused.** Clients
  must integrate server-side. Loom relays through its own backend.
- **A 401 is a bare `Unauthorized` string**, not an envelope. Loom maps it to
  `502 CODEMAN_AUTH` so it never looks like an expired Loom session.
- **Prompts are one line ending in `\r`.** Newlines are stripped server-side;
  Enter is only sent when the input contains `\r`.
- **`clientId` + `seq` make input at-most-once.** Without `wait`, the reply is
  `{}`, so duplicates are invisible; Loom keeps its client id per page load.
- **There is no readiness signal.** A new session reports `exit` until its pane
  exists; Claude's trust dialog is answered by Codeman inside its startup
  window. Other CLIs (Codex update prompt, Antigravity folder trust) must be
  answered on screen.
- **Signals differ per CLI.** `stop` and answerable approvals exist only for
  Claude (hooks). Codex dialogs are read off the screen and are alert-only.
  OpenCode, Antigravity, Pi and Gemini expose `idle`/`working`/`exit` from
  screen detection only.
- **Transcripts differ per CLI.** `last-response?context=full` returns blocks
  `{kind: prompt|response|status|tool, role, text, turn}`: from Claude's JSONL,
  Codex's rollout, or pane segmentation for the others. Pane segmentation can
  fail to tell a prompt from an answer (observed with Pi), so the native
  screen remains the reference there.
- **A timeout on the wait endpoints is HTTP 200** with `wait.timedOut`.
- **The terminal WebSocket sends no history.** Clients read
  `GET .../terminal` first, then attach; resize confirmations (`zc`) carry the
  geometry the pane really took.
- **Defaults are Codeman's.** Claude is launched with permissions bypassed
  unless the Codeman instance is set to the `auto` mode; Codex approvals stay
  on unless Codeman is configured otherwise. Loom never changes these modes.

See the [feature matrix](feature-matrix.md) for each capability and its status.
