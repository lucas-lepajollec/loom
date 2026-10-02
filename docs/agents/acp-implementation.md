# ACP harness execution in Loom

The ACP protocol package and Loom session integration implement NDJSON
JSON-RPC v1 from the vendored
[`acp-schema/schema.json`](acp-schema/schema.json). The embedded registry is
[`internal/loom/harness/acp_agents.json`](../../internal/loom/harness/acp_agents.json).
Codex, Claude Code, Pi, Gemini and OpenCode use generic `acpAdapter` entries.
Custom and SSH launchers can register other ACP agents, including Hermes.
Hermes runs through `hermes acp`, locally or on an SSH machine. The npm bridges have pinned versions; the direct Gemini command uses the installed CLI.
Availability requires both the registry launcher and every `detect` executable.
Antigravity runs through Loom’s `agy-acp` bridge to the native CLI. Its
native access modes remain authoritative; Loom passes neither provider keys
nor MCP servers, and there is no interactive Loom approval RPC for that bridge.
Codex app-server is used only for quotas.

## Selecting and configuring a discussion

`GET /api/runtimes` returns descriptors including `logo`, `available`,
`install_hint`, `docs` and capabilities. ACP choices initially select the agent's
native default (`<runtime-id>:default`); actual model/effort choices come from its
reported session config options. Old app-server model catalogs do not establish
ACP model compatibility. `resume` is advertised after a handshake reporting
`loadSession`; `skills` is not advertised.

Use the existing session selection route with `consent:true` before an external
handoff. No process or generation starts when selecting a choice or a folder.
Configure an existing absolute directory explicitly; a project directory is
never an automatic grant. Harness-only configuration patches can omit the
portable discussion fields and `context_revision` (unchanged legacy fields are
also accepted; portable edits still require a revision):

```json
{"id":"<discussion>","workdir":"/absolute/existing/directory","additional_dirs":[],"permission":"ask"}
```

`POST /api/runtime/sessions/configure` also accepts `mode` and `config`:

```json
{"id":"<discussion>","mode":"<reported-mode-id>","config":{"<reported-option-id>":"<reported-value-id>"}}
```

Boolean options use JSON booleans. Modes and values are checked against the
agent's advertised options before the native RPC. Native settings require a
session handshake; the explicit no-prompt probe can discover supported
model/settings choices without generation. An inactive process applies settings
through `session/set_mode` / `session/set_config_option` without a prompt. If
there is no process, requested settings are applied during the next handshake.
Changing filesystem roots closes the process and clears the native session and
file baselines. Context edits retain the existing revision/consent contract.
Configuration is rejected during an active turn.

Selecting `permission:"full"` requires `consent:true` in that same configuration
request after an explicit UI confirmation. The frontend owner must provide that
confirmation and flag. Loom's policy answers only `allow_once` automatically:
`ask` asks for every request, `edits` allows read/search/edit/think/fetch, `full`
allows every requested kind. Native modes and native restrictions still apply.

## Approvals, files and directories

All endpoints use the existing control authentication, strict JSON for POST,
vault checks and `Cache-Control: no-store`.

- `POST /api/runtime/sessions/approval`:
  `{"id":"<discussion>","approval_id":"<approval>","option_id":"<reported-option-id>"}`
  or `{"id":"<discussion>","approval_id":"<approval>","cancel":true}`.
  Only offered options can be selected, once, on the owning discussion.
- `GET /api/runtime/sessions/files?id=<discussion>` returns the current changed
  file summaries (`path`, `op`, `add`, `del`, `at`). Counts describe the latest
  observed write against its previous content; each write event is in the turn.
- `GET /api/runtime/sessions/diff?id=<discussion>&path=<absolute-path>` returns
  JSON `{ok,diff}`: a unified line diff from before the first Loom-observed write
  to the current file. A removed tracked file is represented by empty content.
- `GET /api/fs/dirs?path=<absolute-directory>` returns JSON `{ok,path,dirs}`;
  directory records contain `name`, `path`, `is_git`. The default is the owner's
  home. It never reads file contents.

ACP fs paths must be absolute. Symlinks are canonicalized; `os.Root` enforces
confinement again during the operation, including symlink swaps. Writes replace regular files atomically; special files are rejected without
blocking. Files must be regular, at most 1 MiB. The baseline store is limited to 16 MiB per discussion.
Line diffs use a bounded LCS; very large unrelated middles use a valid wholesale
replacement hunk. These guards cover Loom's delegated fs methods. Native agent
commands and MCP server execution remain owned by those tools and their own
permissions.

## Lifecycle, shared resources and events

One process is retained per active discussion, then closed after ten idle
minutes. Shutdown (signals, normal exit, desktop quit/restart) and session delete
close its owned process group/tree. Stop sends `session/cancel`, cancels pending
permissions and closes an interrupted process. No turn has an automatic retry.
A resumed native session is used only when the runtime and portable
history/instructions fingerprint agree. `session/load` replay is suppressed;
Loom replays its own persisted event journal. An incompatible or non-resumable
binding starts a fresh native session whose first prompt contains the portable
role/content transcript, without tool results.

Selected enabled shared Loom MCP definitions for supported local agents are
passed to `session/new` and
`session/load`; a changed shared definition restarts the binding before the next
turn, with stdio env/header pairs using exact ACP `name` / `value`
fields. HTTP requires negotiated `mcpCapabilities.http`. Unsupported enabled
HTTP servers or per-tool masks fail explicitly rather than silently granting
extra tools. Definitions and env/header values are not logged or put in session
records. Optional `mcp_servers:["<name>",…]` selections on projects and session
configuration restrict these definitions. Session selection overrides project
selection; absent selections inherit global enabled definitions, and `[]` sends
none. Session `mcp_servers:null` restores inheritance. A globally disabled server
is always excluded. No definition/env/header values are copied into the scope
selection. Existing project edits preserve an omitted MCP selection.
Remote launchers receive no local MCP commands and advertise no delegated
local filesystem access. Shared skills have a separate opt-in, manifest-driven
sink workflow for owned `loom-*` resources; unrelated native folders and
linked read-only sources are preserved. See [workspace contracts](../workspace-architecture.md)
for protocol-specific Native / Loom model sources and credential handoff.

`session/update`, permissions and fs writes publish through the existing
`/api/discussion/events` SSE envelope. `RuntimeTurnRecord.acp_events` preserves
ordered text/reasoning/tool/plan/approval/file/usage/config/mode/command events.
Session state stores `native_session_id`, working roots, permission, mode,
config values and available options, file baselines, usage and commands. This
state uses the existing optional encrypted discussion store and never enters the
portable prompt. Events are saved before live publication. A turn stops explicitly
at 64 MiB or 16,384 journal events, preserving the events already received.

The common chat reducer in `features/chat/engine.js` retains full tool objects,
plans/approvals and `harness: {mode,modes,config,files,usage,commands,permission,workdir}`.
The UI renders native work events, permission requests and harness settings
using the existing components. Supported native sessions can also be resumed
in a [terminal](../terminals.md).

## Deterministic development agent and verification

Set `LOOM_DEV_FAKE_ACP=1` when starting Loom to register `loom-fake-acp` and its
`loom-fake-acp:default` choice. The private `loom-fake-acp` subprocess command
runs only with that flag. Configure a disposable workdir, select it with the
existing external consent flow, then send a synthetic message. The script emits
text, reported thought, tool start/update/end with a diff, plan, usage, modes,
config, commands, permission/resolution and confined fs events. A prompt exactly
`wait` waits for cancellation. It never contacts a model or spends tokens.

Go tests launch the same NDJSON helper as a subprocess; tests cover native
handshake/new/prompt/load/cancel, policy and approval routing, subscriber-aware
expiry, symlink/relative/outside paths, line counts, replay persistence,
auth/config routes and MCP wire definitions. UI tests cover reducer state and
parallel/reused tool IDs. Run the project checks:

```sh
make build
go vet ./internal/loom/
go test -short ./...
make check-ui
```

Use a writable Go/ccache directory in restricted environments. Full Go tests
need local TCP sockets for existing `httptest` servers. Real npm-backed agent
handshake, native account generation and macOS/Windows process teardown need
separate acceptance on those environments.
