# Harness brief — Loom as an ACP client

Historical design reference for the initial ACP integration. The lifecycle,
event vocabulary and permission rationale remain useful to contributors. For
current launchers, delivered skill sinks, model sources and remote limitations,
read [ACP integration](acp-implementation.md) and
[workspace contracts](../workspace-architecture.md). Examples below are design
examples, not an inventory of pending work.

Goal: every coding harness (Claude Code, Codex, Pi, then Hermes Agent,
Cursor…) runs inside a Loom discussion with its real tools, reasoning, diffs,
plans and permission requests visible, a chosen working folder, a permission
level, and Loom's skills and MCP servers distributed to it.

Loom implements the client side of the Agent Client Protocol (ACP,
<https://agentclientprotocol.com>). One generic adapter replaces per-harness
bridges. The JSON schema is vendored in `@agentclientprotocol/sdk`
(`schema/schema.json`, protocol version 1); treat it as the reference for every
field name. Antigravity has no native ACP endpoint; Loom now provides its
`agy-acp` bridge to the CLI, as described in the current integration notes.

## 1. Agent registry (data, not code)

`internal/loom/harness/acp_agents.json` (embedded), one entry per agent:

```json
{ "id": "claude-code", "name": "Claude Code", "logo": "claudecode",
  "command": "npx", "args": ["-y", "@agentclientprotocol/claude-agent-acp@0.88.0"],
  "detect": ["claude"], "docs": "https://…" }
```

Initial entries: `claude-code` (`@agentclientprotocol/claude-agent-acp`),
`codex` (`@agentclientprotocol/codex-acp`) and `pi` (`pi-acp`). Versions pinned. An entry is
"available" when its `detect` binary exists and `npx` (or the command) exists.
Adding a harness = one JSON entry (+ logo). Each registered ACP agent is a
`RuntimeAdapter` with honest capabilities (`chat stream cancel tools approvals
plan usage workdir mcp skills resume` only when really supported).

The existing Codex app-server code stays for **quota reading only**
(`QuotaReader`); turns go through ACP.

## 2. Process and session lifecycle

- One agent process per Loom discussion while it is active (idle timeout
  10 min, then killed; resumed with `session/load` when the agent advertises
  `loadSession`, otherwise a new native session with the portable transcript as
  first prompt context).
- `initialize` with `clientCapabilities: { fs: { readTextFile: true,
  writeTextFile: true }, terminal: false }`, `clientInfo: {name: "loom"}`.
- `session/new` with `cwd` = the discussion's working folder (absolute,
  must exist), `additionalDirectories` when set, `mcpServers` = Loom MCP
  definitions enabled for this discussion/project (stdio: command/args/env as
  `[{name,value}]`; http when the agent advertises `mcpCapabilities.http`).
- Store on the Loom session: `native_session_id`, `workdir`, `mode`,
  `config_options` values, `available_modes`, `available_config_options`.
- `session/cancel` on stop. Kill the process group on Loom shutdown.
- Never send a discussion to a harness without the existing consent flow.

## 3. Client methods Loom serves

- `fs/read_text_file`, `fs/write_text_file`: only inside the workdir and
  additional directories (resolve symlinks, reject `..` escapes). Every write
  is recorded in the session's **changed files** (path, op create/edit,
  timestamp, +/− line counts from a line diff against the previous content).
- `session/request_permission`: apply the session **permission policy**
  (below); when the user must decide, publish an `approval_request` event and
  block until `POST /api/runtime/sessions/approval` answers or the turn is
  cancelled (then answer `{outcome:{outcome:"cancelled"}}`). Timeout: none
  while the UI is subscribed; auto-reject after 30 min with no subscriber.

## 4. Permission levels (Loom side, per discussion)

`ask` (every request goes to the user), `edits` (auto-allow kinds `read`,
`search`, `edit`, `think`, `fetch`; ask for `execute`, `delete`, `move`,
other), `full` (auto-allow everything, explicit confirmation in the UI when
selected). Auto-answers pick the `allow_once` option; never `allow_always`
unless the user clicked it. When the agent exposes modes (`session/set_mode`),
show them separately as the agent's own modes (e.g. plan / default /
acceptEdits); Loom's level is an extra guard, never a bypass of the agent's.

## 5. Discussion events (SSE `/api/discussion/events`, same envelope as today)

Mapping from `session/update`:

| ACP | Loom event |
| --- | --- |
| `agent_message_chunk` (text) | `{type:"text_delta", text}` |
| `agent_thought_chunk` | `{type:"reasoning_delta", text}` |
| `tool_call` | `{type:"tool_start", tool}` |
| `tool_call_update` | `{type:"tool_delta", tool}` / `tool_end` when status completed/failed |
| `plan` / `plan_update` | `{type:"plan", entries:[{content,status,priority}]}` (full list each time) |
| `usage_update` | `{type:"usage", context:{used,size}, cost:{amount,currency}}` |
| `current_mode_update` | `{type:"mode", current}` |
| `config_option_update` | `{type:"config", options}` |
| `available_commands_update` | `{type:"commands", commands:[{name,description}]}` |
| permission request | `{type:"approval_request", approval:{id, tool, options:[{id,name,kind}]}}` |
| permission resolved | `{type:"approval_resolved", id, option_id, auto}` |
| fs write | `{type:"files", files:[{path, op, add, del, at}]}` (full list each time) |

`tool` object (stable shape for the UI):

```json
{ "id": "…", "title": "Read src/main.go", "kind": "read|edit|delete|move|search|execute|think|fetch|other",
  "status": "pending|in_progress|completed|failed",
  "locations": [{"path": "/abs/file", "line": 12}],
  "diffs": [{"path": "/abs/file", "old": "…", "new": "…"}],
  "output": "text, clipped to 64 KiB", "input": "rawInput as compact JSON, clipped to 4 KiB" }
```

All of it is persisted in the turn record so a replay re-renders the same
cards. Text stays Markdown (escaped by the UI); tool output is plain text.

## 6. HTTP

- `POST /api/runtime/sessions/approval` `{id, approval_id, option_id}` or
  `{id, approval_id, cancel:true}`.
- `POST /api/runtime/sessions/configure` gains `workdir`, `additional_dirs`,
  `permission` (`ask|edits|full`), `mode` (agent mode id), `config`
  (`{optionId: value}`); changing workdir restarts the native session.
- `GET /api/runtime/sessions/files?id=` changed files; `GET
  /api/runtime/sessions/diff?id=&path=` unified diff of a changed file
  (before first Loom-observed write → current content).
- `GET /api/fs/dirs?path=` lists sub-directories (name, path, is_git) for the
  folder picker; defaults to the user's home; refuses non-directories; never
  lists file contents.
- `GET /api/runtimes` includes ACP entries: id, name, logo, available,
  install hint, capabilities.

## 7. Skills and MCP distribution (step 2)

- MCP: per session through `mcpServers` (no harness config is modified).
- Skills: `SkillSink` per harness. Loom writes enabled skills into the
  harness's native skills folder under a `loom-` prefix only
  (`~/.claude/skills/loom-<slug>/SKILL.md`, `~/.codex/skills/loom-<slug>/…`,
  Pi via `--skill` paths), with an explicit per-harness toggle in Resources,
  a manifest of what Loom wrote, and removal when untoggled. Never touch files
  without the `loom-` prefix.

## 8. Tests

A fake ACP agent (Go test helper speaking NDJSON JSON-RPC on stdio) covering:
initialize/new/prompt, chunks, tool calls with diff, plan, usage, permission
request (ask/edits/full policies), fs write inside/outside workdir, cancel,
load. Also a dev-only agent `loom-fake-acp` registered when
`LOOM_DEV_FAKE_ACP=1`, scripted to exercise every event so the UI can be
reviewed without spending tokens.
