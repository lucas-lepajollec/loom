# Loom workspace: discussions, models and context

Current workspace contracts as of 2026-10-03. Read with the
[architecture principles](architecture-principles.md),
[package architecture](architecture.md) and [roadmap](ROADMAP.md).
[ACP integration](agents/acp-implementation.md) documents harness execution.

## One discussion, several executors

A discussion belongs to Loom. Its portable transcript and explicitly selected
project context survive changes between local models, cloud models and harnesses.
A harness is an executor that can itself use a local or cloud model when its
protocol supports that source.

- Local owns the GGUF library, Hub, presets, engine and API controls.
- Cloud owns provider connections, model catalogs and selector visibility.
- Harnesses owns native/ACP configurations, machine connections, resources and
  supported model-source associations.
- Projects link working folders, instructions, selected context files, Brain
  sources, skills, MCP selections and default execution choices.
- Brain owns shared sources/context, memory pages, skills and MCP definitions; the historical Resources route remains an alias.
- Terminals, Environment, Bench, Usage and Settings expose their own controls.

The original discussion/composer and full local parameter panel remain the
common interface. The side panel adapts to the executor. Reuse existing UI
components and styles; the visual design belongs to the design owner.
`internal/loom/ui/next` contains native ES modules with vendored Preact + htm,
embedded directly in the Go binary without an asset build step.

### Narrow screens

The existing shell becomes an opaque navigation drawer at 720px and below.
Settings starts on a section index, then opens one section with a back link to
`#/settings`; desktop keeps its side navigation. Hidden mobile sections are not
mounted. Anchored model menus follow the visual viewport, resize and content
changes. Bench and Usage use labeled metric blocks below 900px, preserving the
native usage values rather than hiding columns. Responsive overrides load after
the desktop styles in `ui/next/css/mobile.css` and reuse the same theme tokens.

## Discussion and execution state

`RuntimeSession` stores the Loom transcript, project reference, selected route,
per-turn provenance, reported usage, request IDs and execution state. Selecting
another target updates that same record without clearing or forking messages.
Route changes are rejected during an active reply.

The chat picker identifies an execution by runtime, provider and exact model,
not by filename. Local choices come from the active engine's library, including
remote nodes and directly linked servers; engine aliases are resolved without
guessing between equal filenames. Returning from a harness to direct execution
must select and activate the local route before loading the engine model. A
missing, hidden or ambiguous choice reports an error. The picker keeps the
execution label (Local, Cloud or the harness name) visible on narrow screens.
Loading a model from the engine library alone remains engine management and
does not change the execution of an existing discussion.

Titles, project associations and discussion-specific instructions can be edited
without rewriting exchanged messages. Detaching a project removes its context
from future prompts, not text already shared. Missing or locked projects block
sending until resolved or detached. Context edits and sends check a revision
hash; stale revisions fail before generation or history persistence. An already
accepted request ID remains idempotent.

`workspace_native.go` binds common local discussions to the existing rich
Conversation pipeline and native archives: attachments, tools, reasoning,
model/global prompts, presets and compaction retain their native processing.
Returning from cloud or a harness appends portable text without replaying tools
and preserves source or diverged archives. Imported text stays inert. The display
journal supplies visible user/assistant text; private reasoning and tool protocol
messages are not promoted into portable context.

The shared llama.cpp engine stays running in router mode while models load and
unload through its API. Older engines or explicit single mode retain the legacy
process-replacement path. vLLM and linked servers have separate lifecycle and
capability boundaries; see [engines](engines.md).

## Cloud destinations and credentials

The common cloud adapter implements Chat Completions and bounded SSE streaming.
It uses the configured base URL/model, rejects redirects and sanitizes upstream
errors. HTTPS is required except explicitly local-network HTTP. It does not
silently convert another provider protocol, retry a paid turn or infer native
model compatibility.

The Cloud catalog probe is explicit: a Bearer key is sent to the selected
GET `/models` destination, with no discussion or generation. Up to 32 selected
IDs can be saved, or entered manually. A returned model catalog does not prove
account access, quota or chat compatibility. `usage_mode:"none"` omits streamed
usage options for providers that reject them.

Provider records contain configuration, not credentials. Keys live in the
workspace's server memory and can be explicitly remembered in the OS keychain.
Startup restores available remembered keys; otherwise reconnect after a restart.
Keys are not saved in provider records or browser storage. The optional Loom
vault protects supported Loom stores; it is separate from OS keychain storage.
Linked-engine credentials use the optionally encrypted node record. MCP files,
external project files and native harness data retain their own storage rules.

`ready` means an in-memory key is present, not that a model/account has passed
real-provider acceptance. Changing a destination requires a new connection.
Selecting an external executor requires confirmation before sharing the portable
text history and selected context; that can include earlier replies.

## Harnesses and native sessions

Codex, Claude Code, Pi and OpenCode have built-in ACP launchers.
Antigravity uses Loom's own ACP bridge to `agy`; authentication, native models
and access modes remain with that CLI. It does not receive Loom provider keys or
MCP servers and does not provide an interactive Loom approval RPC. Hermes can
run through a custom or discovered SSH ACP launcher; its built-in local entry
provides quotas and usage, not chat.

Availability requires the native CLI and adapter launcher. Capabilities come
from descriptors and handshake results, not a model association. ACP exposes
native tools, diffs, plans, permissions, settings and reported usage where the
agent supplies them. Hidden reasoning is never reconstructed. An agent-reported
write target is not a verified file diff; Loom's delegated filesystem writes
have their own bounded baseline/diff tracking.

Bindings stay under the Loom discussion. Native resumption requires compatible
runtime, working roots and context. Unsupported or incompatible resumption
starts a fresh native session with portable role/content text, without tool
results. Import/replay does not fabricate private memory or duplicate tools.
Loom owns the process it launched and closes it on cancellation, idle timeout
or shutdown; it never kills an unrelated process.

**Native / Loom** model sources are protocol-specific. Codex and Claude Code
receive compatible endpoints, selected models and keys in their launch
environment. Opt-in Pi configuration contains provider entries with environment
references; OpenCode receives launch configuration. Antigravity uses its native catalog. Hermes source configuration remains on its
machine and is not a universal binding. Native global permissions are never
changed to make a source work.

Shared MCP definitions can be scoped globally, per project and per discussion;
disabled servers are always excluded. Local ACP agents receive supported
selected definitions. Remote launchers do not receive local MCP commands or
Loom-delegated local filesystem access. Opt-in skill sinks distribute owned
`loom-*` links/resources with manifests; unrelated native skills remain intact.
See [MCP files](mcp-files.md) and the [ACP notes](agents/acp-implementation.md).

A supported harness discussion with a native session ID can be resumed in a
real terminal on its machine. That terminal uses the native CLI rather than
portable text replay; see [terminals](terminals.md).

## Context ownership and portability

| Owner | State | Shared across execution choices? |
| --- | --- | --- |
| Loom discussion | Visible user/assistant text and provenance | Text yes; provenance stays in the display journal |
| Loom project | Instructions, selected files/skills, budgeted Brain passages | Yes, assembled for the next turn |
| Engine/runtime | KV cache, hidden reasoning, compaction internals | No |
| Native harness | Session ID, tool journal, approvals, native memory | Only through its own supported protocol |
| Project folder | Files on disk | Explicit context files or native tool access only |

A folder link alone does not grant Loom's delegated filesystem permissions.
Explicit local context files are reread for messages, confined to the project
folder and bounded to eight files / 48 KiB. Remote project folders are used by
harnesses and terminals on that machine; their context files are not read by
Loom's local context-file pipeline.

Projects can select Brain sources and a token budget. Retrieval adds cited
passages for the latest user message; personal sources must be explicitly
selected. BM25 is local. Cloud semantic indexing sends selected source text and
search queries only after stored consent. Distillation generates durable items
only on request and requires consent when the active engine is remote.
See [Brain](brain.md) for source access, indexing, storage and MCP boundaries.

The prepared-text preview lists initial instructions, portable history and an
optional draft without generating a reply or persisting that draft. It is not
a native wire dump: the original local pipeline and native harness retain their
own prompt templates, tools and compaction. Brain semantic retrieval can require
its configured embedding backend when project context is assembled.

Shared context is reread at send time and revision-checked. Context consumes
model tokens; byte limits are not token-window guarantees. No identical memory,
latency or billing across executors is promised.

## Events and usage

Authenticated `/api/discussion/events` SSE supplies one event vocabulary and
replay for local, cloud and harness discussions. `features/chat/engine.js` uses
one reducer/subscription. Reconnection subscribes to state and never resubmits
generation. Native runtime metadata remains display-only; unknown historical
metrics stay unknown.

Usage distinguishes retained Loom turns, native harness activity including
sessions outside Loom, subscription windows, provider balances and manual-price
estimates. Deleting a discussion changes retained totals. Costs are not invoices;
missing values are not zero. Antigravity's reported end-to-end output-token rate
is not llama.cpp decode throughput. Account refreshes do not generate replies
or redeem resets. See [harness usage](harness-usage.md) and
[provider balances](usage.md).

## API and persistence boundaries

Workspace management APIs use the control-key boundary, strict JSON and bounded
requests. Important existing routes include:

- `GET /api/workspace`, `/api/providers`, `/api/runtime/sessions`.
- `POST /api/providers/save`, `/api/providers/disconnect`, `/api/providers/models`.
- `POST /api/runtime/sessions/create|select|import|send|stop|delete|configure`.
- `POST /api/runtime/sessions/local` binds/restores the rich native discussion;
  it does not load a model or generate a reply.
- `GET/POST /api/runtime/sessions/preview` prepares portable text.
- `POST /api/runtimes/{id}/connect|quota` dispatches optional registry actions.
- ACP approval, configuration and file routes are described in the
  [ACP notes](agents/acp-implementation.md).

Common portable requests retain bounded message counts and text sizes; oversized
inputs fail rather than being silently truncated. A restart marks unfinished
turns interrupted and never resubmits them. Cancellation retains received text.
Storage failures can leave an unsaved result in memory; a crash can lose
non-checkpointed streaming text.

## UI and PWA boundary

English is the initial UI language; French can be selected and remembered.
Both use the same components and styles; see [languages](i18n.md).

PWA installation depends on browser support and a secure context: HTTPS or
recognized loopback development. Plain LAN HTTP can show the UI but does not
guarantee installation. The service worker caches only the public offline page
and listed icons, never conversations, credentials, app HTML, API/SSE traffic
or queued sends. Offline guards preserve the live draft without automatic retry
or durable offline storage. Notifications require explicit permission.

## Contributor verification

Run `make check-ui` for syntax and UI regression checks, and
`go test -short ./...` for Go validation. Protocol fixtures establish deterministic
behavior; real accounts, GPU fitting, OS keychains and native Windows ConPTY need
acceptance on their actual platforms. Use the [roadmap](ROADMAP.md) for remaining
work rather than earlier migration briefs.
