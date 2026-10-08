# Agent protocol compatibility

Loom's local builtin Codex runtime prefers the installed `codex app-server`;
local builtin Pi prefers `pi --mode rpc`. A read-only CLI help probe selects
native transport when the protocol exists. A missing protocol selects the
existing pinned ACP launcher. Authentication, startup, resume and RPC failures
never silently retry a prompt through a different runtime. Remote/custom
launchers retain ACP. Native accounts and global permissions remain owned by
the CLI. No Go dependencies were added.

The tested contracts are **codex-cli 0.159.2** and **Pi 0.84.3**. Codex's selected
schema snapshot and generated Go message projections live in
`internal/loom/runtime/codexapp/`. Complex unions remain `json.RawMessage` so a
new variant survives translation. The official [app-server contract](https://developers.openai.com/codex/app-server)
and the installed Pi package's `docs/rpc.md` define the wire protocols.

## UI contract (backend only)

The existing authenticated session/discussion SSE envelope is unchanged. The
existing text, reasoning, tool, plan, approval and usage rows keep their fields.
Each agent row additionally carries `agent_event`. Render its new kinds, or use
its metadata alongside the old row; do not render both as duplicate rows.
Canonical events and requests are display/private state, never portable prompt
content. Their shape is:

```json
{
  "type": "tool_delta",
  "tool": {"id":"command-1","kind":"execute","title":"echo hello","status":"in_progress","output":"hello\n"},
  "agent_event": {
    "type":"content.delta","runtime":"codex","thread_id":"native-thread",
    "turn_id":"native-turn","item_id":"command-1","stream":"command_output",
    "delta":"hello\n","method":"item/commandExecution/outputDelta",
    "raw":{"method":"item/commandExecution/outputDelta","params":{"delta":"hello\n"}}
  }
}
```

| `agent_event.type` | Fields to render |
| --- | --- |
| `turn.started` | `thread_id`, `turn_id` (Codex native turn ID; Pi uses Loom's turn request ID) |
| `turn.completed` | `status`: `completed`, `failed`, `interrupted`, `cancelled`; optional `error` |
| `content.delta` | `item_id`, `stream`, `delta`, optional `replace`; streams include `assistant_text`, `reasoning_text`, `command_output`, `file_change_output`, `tool_arguments` |
| `item.started/updated/completed` | `item_id`, `item_type`, `status`, `payload`; command output/exit, file diffs, plan, web/MCP/tool arguments and results remain provider-shaped inside `payload` |
| `usage.spent`, `context.updated` | `usage:{scope,input?,output?,cached?,reasoning?,total?,context_window?}`; scope distinguishes `message` spend and `context` observations; `payload` retains provider usage |
| `request.opened` | `request` below |
| `request.resolved` | `request_id`, `outcome`: `accepted`, `declined`, `answered`, `cancelled`; optional `decision` |
| `raw` | `method`, `payload`; show a generic JSON row |
| `warning`, `error` | `message` or `error`; warnings do not block execution |

Every canonical event includes bounded `raw` provider JSON. Each `raw`/`payload`
field is at most 64 KiB. Larger values become
`{"truncated":true,"bytes":123456,"preview":"..."}`. Do not interpret a preview as
a complete diff/result. File changes retain reported diffs verbatim; Loom never
reconstructs a diff from a write target. Unknown notifications already have a
generic legacy tool row. Unknown server requests receive a protocol rejection
and a visible error instead of leaving the server waiting.

For questions/forms, the outer SSE type is `request.opened` or
`request.resolved`, with `request` / `request_id` / `outcome` directly available
as well as in `agent_event`. Approvals retain the old `approval_request` and
`approval_resolved` aliases; new clients read the canonical kind there.

```json
{
  "type":"request.opened",
  "request": {
    "id":"codex:18","kind":"user_input","method":"item/tool/requestUserInput",
    "item_id":"question-1",
    "questions":[
      {"id":"color","header":"Color","question":"Choose a color","free_text":true,
       "options":[{"id":"Blue","label":"Blue","description":"Cool"}]},
      {"id":"note","question":"Any note?","free_text":true}
    ]
  }
}
```

Question options use `id`, `label`, optional `description`. A question may also
set `secret:true`; render an appropriately private input. Codex supports
multi-question forms and free text where its protocol allows it. Pi extension
`select`, `confirm`, `input`, `editor` dialogs use one question with ID `value`;
confirm options are `yes` and `no`. Pi has no builtin command-approval RPC;
confirm is extension input, not an invented sandbox permission.

```json
{"id":"codex:17","kind":"approval","method":"item/commandExecution/requestApproval",
 "approval_kind":"command","item_id":"command-1","message":"Command needs approval",
 "options":[{"id":"allow_once","label":"Allow once"},
            {"id":"allow_always","label":"Allow for this session"},
            {"id":"deny","label":"Deny"}]}
```

`allow_always` maps to Codex's **session-scoped** acceptance, not a persistent
execpolicy/global-permission change. ACP keeps the provider's option IDs and
labels, so submit the actual offered ID. File approvals use `approval_kind:file`;
generic tool permissions use `approval_kind:tool` where supplied.

```json
{"id":"codex:21","kind":"elicitation","method":"mcpServer/elicitation/request",
 "message":"A form","schema":{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}}
```

URL elicitation replaces `schema` with `url`; opening the URL remains a UI/user
action. Form content is validated against the supplied JSON schema using Loom's
existing schema library. External schema-reference loading is disabled.
ACP `session/create_elicitation` supports form and URL requests too.

Resolve any pending request with the authenticated endpoint:

```text
POST /api/workspace/sessions/{id}/requests/{request_id}
Content-Type: application/json

{"decision":"allow_once"}
{"answers":{"color":["Blue"],"note":["A free-text answer"]}}
{"decision":"accept","content":{"name":"Fixture"}}
{"decision":"decline"}
{"decision":"cancel"}
```

Use `encodeURIComponent` for both path IDs. The endpoint returns `{ok:true}` or
HTTP 409 `{ok:false,error}` for a missing, invalid, expired or resolved request.
The old `/api/runtime/sessions/approval` endpoint uses the same native response
channels and still accepts its original body. Requests are saved in the private
session's `pending_requests` array and journal before publication, and replayed
on browser reload. Resolution removes them. Turn completion/stop/disconnect
cancels remaining requests; an abandoned turn after a Loom restart replays
cancelled resolutions. Pi's native dialog timeout also expires its request.

## Runtime records and native sessions

`GET /api/runtimes` descriptors carry `compatibility`; the same record is
available as `{ok:true,compatibility:...}` at
`GET /api/runtimes/{id}/compat`. Before connection the observed `version` is
empty, not an invented version. Connect records the executable version and
catalog; Codex/Pi version differences produce a visible, non-blocking warning.
ACP records its handshake agent version rather than the version of `npx`.

```json
{"runtime":"codex","executable":"codex","version":"codex-cli 0.159.2",
 "protocol":"app-server","adapter_version":"2.0.0",
 "tested_version":"codex-cli 0.159.2","capabilities":["chat","stream","cancel","resume","approvals","user-input","elicitation"]}
```

Loom's `native_session_id` is Codex's thread ID or Pi's native session ID.
Pi also saves `native_session_file`, and resumes that file through
`switch_session`; an imported ID is resolved within Pi's native session store.
Pi supplies no native turn ID, so its canonical `turn_id` is Loom's request ID;
this never replaces the native session ID or file.
Codex uses `thread/start`, `thread/resume` and `turn/interrupt`. Native history
uses Codex `thread/list`/`thread/read` and Pi bounded session headers plus RPC
`get_messages` (the native active branch). Model choices come from `model/list`
and `get_available_models`; Codex effort choices remain discovered native
values. The existing Codex read-only quota reader is unchanged.
Pi reads `get_session_stats` after settlement for a separate `context.updated`
observation; it may arrive after `turn.completed`. Unknown occupancy stays
absent, including immediately after native compaction. Message spend is not
treated as session context occupancy.

A genuine resume failure is surfaced, with no fresh-session retry. Reported
Codex busy/lock errors include `session open in another Codex window`. Native
CLIs that do not report exclusive locks cannot provide an exclusive-writer
guarantee; Loom does not kill their windows. Changing the route or incompatible
prepared context starts the usual explicit portable text handoff.

## Fixture maintenance and checks

`internal/loom/testdata/agents/{codex,pi}/*.jsonl` contains synthetic protocol
exchanges derived from the versioned official schema/installed docs. No real
account prompts, credentials or paid outputs are in this corpus. Each record
contains a provider `frame`, expected canonical `events`, and optionally a Loom
`answer` plus expected native `response`. An optional `command` marks a
correlated client-call response instead of a notification in the turn stream.
The fake process supplies the request's actual correlation ID.
`TestAgentFixtureCorpus` runs the
adapters against a fake stdio process, checking event order, correlated replies,
failures, native IDs and successful/failed/interrupted completion. Coverage
includes text/reasoning/usage, tool output, approval/extension confirmation,
questions, file changes, errors, interruptions, unknown notifications/requests,
Codex form/URL elicitation, plans and tool permissions, Pi retries, streamed
tool arguments, final-only messages, and separate context observations.
Additional fake-process tests cover verbatim command rejection, malformed JSON,
process exit, and stopping while the prompt acknowledgement is still pending.

Refresh the tested Codex release deliberately:

```sh
codex --version
codex app-server generate-json-schema --out /tmp/loom-codex-schema
python3 tools/generate-codex-types.py /tmp/loom-codex-schema
python3 tools/generate-codex-types.py
```

Update `schema/VERSION`, the generator's version, `TestedVersion`, this document
and fixtures together. The committed snapshot merges only used message schemas
and their definitions; rerunning the generator from it reproduces the Go file.
For Pi, inspect the installed package's `docs/rpc.md`, update `TestedVersion` and
fixtures together, and check every command's `success/error` contract.

For live recording, use an explicitly authorized test account or a local test
model, an isolated working folder, and a JSONL proxy that captures both pipe
directions. Convert captures to the fixture record shape. Remove credentials,
account identifiers, real prompts/results and private paths; replace native IDs
consistently and retain the actual method/field names. Review new event/reply
assertions before accepting a version upgrade. Ordinary tests never launch a
real agent. Read-only acceptance is opt-in:

```sh
go test ./internal/loom/runtime/... -run TestAgentFixtureCorpus
go test ./internal/loom -run 'TestAgent|TestACP|TestCodex'
LOOM_AGENT_HANDSHAKES=1 go test ./internal/loom/runtime -run TestNativeAgentHandshakes -v
go vet ./...
go test ./internal/loom/...
make check-ui
make build
```

The optional handshake test sends only initialize/state/model-list calls,
never a turn, login or quota reset. Its Codex SQLite directory is temporary;
other native state may still require write access in restricted environments.

## Acceptance limits for this slice

- Pi 0.84.3 state/model-list handshakes passed locally. Codex 0.159.2 schema
  generation passed; live app-server startup was blocked by read-only native
  state in the execution sandbox (also with a temporary SQLite directory).
  Codex turn behavior and both runtimes' generation are verified by fixtures,
  not paid live turns. CLI/app simultaneous-window locking remains a real
  platform acceptance check.
- Native adapters use the CLI's own MCP configuration. Provisioning Loom-scoped
  MCP definitions through these native protocols is not implemented; explicit
  Loom MCP selections fail visibly and descriptors do not claim `mcp`.
- Native import currently hydrates portable user/assistant text, not historical
  tool cards, hidden reasoning or runtime-private approval state. New live turns
  retain their rich canonical journal. Very large Codex full-history replies
  are bounded by the 4 MiB frame limit.
- New questions/forms require the UI rendering described above. UI files,
  tokens and layout were not changed.

See [the implementation verification report](agents-v2-verification.md) for the
checks completed and the sandbox failures encountered during this change.

## Agents v2 step 2

| Agent | Protocol / adapter | Tested version | Capabilities covered | Known gaps |
| --- | --- | --- | --- | --- |
| Claude Code | ACP v1, `@agentclientprotocol/claude-agent-acp@0.88.0` | Adapter 0.88.0 source/fixtures; installed CLI 2.1.294 version check | Text/thoughts, tools, terminal-output metadata, plans, plan approvals, AskUserQuestion single/multiple/multi-select/Other/Skip, MCP form/URL elicitation, failure metadata | No live 0.88 handshake/paid turn in this sandbox; Bash output timing is SDK-owned; no client-created terminals |
| OpenCode | Native `opencode serve`, HTTP + `/event` SSE | Installed 1.18.33 schema generation; HTTP fixtures | Native session create/resume/list/text import, provider models, text/reasoning reconciliation, tools, reported patches/plans/usage, approvals, questions, abort, verbatim errors | Live listener blocked by sandbox; CLI-owned MCP; V2 question/permission variants fail explicitly; concurrent TUI answers are not resolved into Loom's pending cards until turn settlement |
| OpenCode fallback | Native `opencode acp` | 1.18.33 schema/source and synthetic ACP v1 turn | Existing ACP tools, permissions, configuration, replay; launch-scoped Loom sources | No paid/live ACP turn; questions depend on what the upstream ACP bridge exposes |
| Hermes | Native `hermes acp` | No release accepted in this step | Existing shared ACP surface when supplied by the handshake | Versions unknown until handshake; no new release acceptance claimed |
| OpenClaw | Native `openclaw acp` | No release accepted in this step | Existing shared ACP surface when supplied by the handshake | Versions unknown until handshake; no new release acceptance claimed |
| Antigravity | Loom `agy-acp` bridge (adapter pin = Loom version) | No additional native release accepted in this step | Existing native CLI text/tools/plans/usage/modes/resume | No interactive permission RPC, provider keys or Loom MCP; version remains handshake-observed |

The local builtin OpenCode selects HTTP when its read-only help advertises
`serve`. Loom starts one owned server lazily, with `--hostname 127.0.0.1`,
`--port 0`, `--mdns=false` and a random per-process Basic-auth password. It
verifies both health and rejection of unauthenticated access before submitting
context. Session cancellation calls `/session/{id}/abort` and leaves the shared
server available to other discussions. Shutdown closes only Loom's owned child.
A startup/authentication/resume/prompt failure never retries a turn through ACP.
Missing server support, remote/custom launchers and an enabled **Loom model
source** use ACP. That launch supplies only the selected OpenCode provider key;
catalog probes get provider definitions with environment references, without
Loom keys. The shared HTTP server uses the CLI's own credentials/configuration.
Native session IDs are also usable by `opencode --session ID` in the TUI.

The versioned unedited OpenAPI snapshot is at
[`runtime/opencodehttp/schema/`](../internal/loom/runtime/opencodehttp/schema/README.md).
It was produced by installed `opencode generate`, which uses the same
`Server.openapi()` as `/doc`; fetching live `/doc` was blocked by the sandbox.
The 1.18.33 events are `permission.asked`, `question.asked`,
`message.part.updated` and `message.part.delta`. The older
`permission.updated`/session permission reply endpoint is accepted too.

### Additional backend fields for the UI owner

- `request.questions[].multi_select:true`: multiple offered labels may be
  selected. Submit all selections in that question's existing answer array.
  `false`/absent permits one offered option, plus free text when enabled.
- `request.questions[].optional:true`: Skip is supported. Submit an empty array
  for that question; include every question ID in `answers`, including skips.
- Claude question IDs are `question_0`, `question_1`, etc. `free_text:true`
  represents the native Other field. A selected option plus free text becomes
  the native selection plus its custom note; multi-select Other is additive.
- `request.approval_kind:"plan"`: EnterPlanMode/ExitPlanMode uses the existing
  approval card and the exact offered option IDs. Plan transitions stay explicit
  even when ordinary tool approvals are automated.
- Compatibility adds `adapter_package`, `agent_version` (exact handshake value),
  and `tested_versions` (known accepted fixture/source versions). Native CLIs
  are not pinned by Loom; their `adapter_version` is empty on ACP records.
  Missing observed/tested versions remain unknown. A new observed version
  outside the tested set produces a non-blocking `warning`, including agents
  with no accepted native release.

Example multi-select answer (same endpoint, no new UI endpoint):

```json
{"answers":{"question_0":["Blue","Green","Purple"],"question_1":[]}}
```

Claude initialization advertises `elicitation:{form:{},url:{}}` (capability
objects, not booleans), filesystem read/write where already permitted, and
`_meta.terminal_output:true`. It negotiates only the documented AIR
`sessionFailure` extension in `_meta.jetbrains.air`; reported failure titles
are canonical error messages verbatim, including successful `end_turn`
responses carrying failure metadata. Warning-severity notices remain warnings.
RPC rejection messages for Claude are surfaced verbatim too. Failure and raw
protocol metadata remain display/private state, outside portable context.

Step 2 fixtures are synthetic version-derived protocol records under
`testdata/agents/{claude,opencode,opencode-acp}`. They exercise replies,
error outcomes, cancellation and complete fake ACP turns. HTTP fixtures use a
real httptest server when sockets are permitted and the same streaming HTTP
handlers through pipes/httptest writers in restricted environments. See
[step 2 verification](agents-v2-step2-verification.md) for the actual checks and
live-versus-fixture boundary. No paid turns were run.

Sources: [Claude 0.88 changelog](https://github.com/agentclientprotocol/claude-agent-acp/blob/v0.88.0/CHANGELOG.md),
[Claude elicitation source](https://github.com/agentclientprotocol/claude-agent-acp/blob/v0.88.0/src/elicitation.ts),
[OpenCode server](https://opencode.ai/docs/server/),

## Agents v2 step 4: official ACP catalogue

The backend embeds the [official ACP registry](https://cdn.agentclientprotocol.com/registry/v1/latest/registry.json)
in `internal/loom/harness/acp_registry_snapshot.json`. Its initial snapshot has
41 agents. Startup and catalogue reads request a background refresh, with a
10-second deadline and at most one attempt per 24 hours, including failed
attempts. Loom state retains the last good document, ETag and attempt time;
conditional GET accepts 304 without replacing the document. Offline startup
uses the saved document or the embedded snapshot. Startup never waits on CDN
I/O. Unknown fields and malformed individual entries are ignored; an invalid
binary archive URL rejects the update. All binary archives must use HTTPS.
The registry is distribution metadata, not a claim of live Loom verification.

Authenticated API shapes for the Agents page (no UI changes in this step):

```json
[
  {
    "id": "qwen-code",
    "name": "Qwen Code",
    "version": "0.25.0",
    "description": "Registry description",
    "repository": "https://github.com/QwenLM/qwen-code",
    "icon": "https://cdn.agentclientprotocol.com/registry/v1/latest/qwen-code.svg",
    "distribution": {
      "kind": "npx",
      "command": "npx",
      "args": ["-y", "@qwen-code/qwen-code@0.25.0", "--acp", "--experimental-skills"],
      "package": "@qwen-code/qwen-code"
    },
    "installed": true,
    "added": false,
    "builtin": false
  }
]
```

- `GET /api/agents/catalog` returns the array directly. `icon` and
  `installed_version` are optional. `installed` means the required executable
  is on PATH: the native binary, or `npx`/`uvx` for an on-demand package launch.
  It does not assert that an npm/uv package has already been downloaded or that
  an account is signed in. `installed_version` is a last observed compatibility
  version, not a guess from the registry or PATH. `added` means a saved Loom
  runtime exists independently of installation/sign-in.
- `distribution.kind` is `npx`, `uvx`, `binary`, or `unsupported` for a platform
  without a supported distribution. `command` is the executable; `args` is the
  full argv excluding that executable and is always an array. `package` is the
  unversioned package name (or binary name). Loom prefers npx, then uvx, then
  the current platform's binary. Distribution environment/installer extensions
  are not imported in this step; launch arguments are taken from the registry.
- `codex-acp`, `claude-acp`, `pi-acp`, `opencode`, and `antigravity-acp` are
  visible as `builtin:true` and excluded from adding another runtime. The same
  rule applies if curated builtin IDs appear in a later registry. Gemini is
  also reserved (`builtin:true`) by Loom policy: it has no Loom builtin runtime;
  Antigravity is the Google harness. The UI must not offer Add on these rows.
- `POST /api/agents/catalog/add` with `{"id":"qwen-code"}` returns
  `{"ok":true,"agent":{"id":"registry-qwen-code","name":"Qwen Code","command":"npx","args":["-y","@qwen-code/qwen-code@0.25.0","--acp","--experimental-skills"],"custom":true,"registry_id":"qwen-code","registry_version":"0.25.0","registry_package":"@qwen-code/qwen-code","registry_kind":"npx"}}`
  (ordinary ACP metadata such as `docs`, `logo`, `detect` is also present).
  These runtimes use the existing ACP adapter, session configuration, consent
  and request-resolution endpoints. Adding does not launch a process, sign in,
  probe an account or share a transcript. Repeated Add returns the saved launch
  unchanged: refreshing the registry never silently upgrades an added agent.
- npx launches `npx -y <package>@<registry-version> <args>`; uvx launches
  `uvx <package>==<registry-version> <args>`. Existing package pins are replaced,
  including scoped npm names and uv entries using either `@` or `==`.
  Binary entries launch the command basename on PATH with the selected
  platform's argv. Loom does not download archives. Missing binaries return
  HTTP 400, `{"ok":false,"error":"binary must already be installed on PATH","install_hint":"upstream repository or website/archive URL"}`.
  Unsupported/reserved entries also return 400; unknown IDs return 404.
- `POST /api/agents/catalog/remove` with the registry ID returns
  `{"ok":true}` and removes the saved configuration/runtime registration;
  a missing saved entry returns 404. It does not uninstall anything. Remove
  and Add again explicitly select the current registry version.

Catalogue compatibility records include `adapter_package`, `adapter_version`,
`tested_version`, `tested_versions`, and `tested_version_source:"registry"`.
The pinned registry version is the distribution baseline; `agent_version`
and `version` remain the actual handshake value. A different handshake version
produces a non-blocking warning. `registry` provenance must not be presented as
an accepted live Loom test. `tested_version_source:"loom"` uses the accepted
versions described below.

## Curated ACP agents

| Runtime ID | Launch | Availability | Live accepted versions |
| --- | --- | --- | --- |
| `hermes` | `hermes acp` | `hermes` on PATH | None |
| `openclaw` | `openclaw acp` | `openclaw` on PATH | None |
| `deepseek-tui` | `deepseek-tui serve --acp` | `deepseek-tui` on PATH | None |

All three use the shared ACP initialization, permissions and form/URL
elicitation path. Questions supplied as elicitation schemas remain forms;
Loom does not invent a native question protocol for these agents. The accepted
version lists are empty, and their compatibility warning is exactly
`not yet verified with Loom`, including before any handshake. DeepSeek's launch
is **documented-but-unverified**: the study's CDesktop executor launches
`deepseek-tui serve --acp`, but no DeepSeek upstream repository clone/README was
available for this implementation. No successful live launch is claimed.

Synthetic shared ACP fixtures in `testdata/agents/{hermes,openclaw,deepseek-tui}`
cover a normal turn, permission reply and form elicitation. Complete subprocess
turn tests exercise the bidirectional reader and canonical request resolution.
These fixtures establish Loom's shared path, not an upstream version's behavior.

## Weekly compatibility watch and accepting versions

`internal/loom/harness/tested_versions.json` is the **single source of truth**
for accepted versions. It is embedded by the dependency-free `harness` package
and consumed by native and ACP compatibility records, including refreshed
observations saved before an upgrade. `tested_version` is the newest accepted
entry, and every entry remains accepted for drift comparisons. The native
schema `VERSION` files record snapshot provenance, not another accepted list.
The existing Claude launcher pin stays explicit; accepting an adapter release
in the watch does not silently change that launch pin.

`.github/workflows/agents-watch.yml` runs every Monday at 06:17 UTC and through
`workflow_dispatch` on Ubuntu. It uses the automatic `GITHUB_TOKEN` with
contents, PR and issue permissions; no account or provider secrets are needed.
The repository must allow GitHub Actions to create pull requests. The Go script
in `tools/agents-watch/` is runnable locally:

```sh
make agents-watch                         # check installed CLIs; write report only
make agents-watch AGENTS_WATCH_ARGS=--install  # latest npm releases, temporary prefix
make agents-watch AGENTS_WATCH_ARGS='--install --publish' # explicit GitHub review writes
```

Local reports default to ignored `.project-local/agents-watch/`. `--install`
uses temporary npm prefixes for `@openai/codex`, `opencode-ai`,
`@agentclientprotocol/claude-agent-acp`, and the current official
[`@earendil-works/pi-coding-agent`](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/package.json).
Probes use empty native profiles and an environment without provider/publishing
credentials. They initialize Codex/Claude or read Pi state, list models where
possible, and check the owned OpenCode loopback server. Authentication-required
steps are explicit skips. No prompts or paid turns run.

The script regenerates the Codex schemas consumed by Loom using
`codex app-server generate-json-schema --out …` and the OpenCode OpenAPI using
`opencode generate`. It compares parsed JSON with committed snapshots, ignoring
formatting/key ordering while retaining every field/union. Changed schemas
produce candidate JSON, a unified diff and a bounded changed-path summary.
It runs `go test ./internal/loom/runtime/... -run Fixture` and the application
ACP fixtures, refreshes/validates the registry, and writes a Markdown report
uploaded as a workflow artifact and included in the job summary.

With `--publish`, a completely passing check opens/updates
`agents-watch/<UTC-date>` from the checked HEAD in a temporary worktree. Only
`tested_versions.json` and the refreshed registry snapshot are included. Local
publishing requires a clean checkout; the script does not commit local work.
A schema change, installation/probe failure or failed fixture check opens or
updates one exact-title issue per affected candidate:
`Agents watch: <agent> <version> needs attention`. Registry-only failures use a
registry attention issue. No versions are accepted automatically when a check
fails. Automation returns a failing status after publishing attention issues.

To accept a new version, review the watch report and skips, inspect schema diffs
and adapt mappings/fixtures when needed, then rerun the checks. Merge the review
PR or append the verified version to the agent's array in `tested_versions.json`
in a reviewed change. For a changed native contract, refresh its schema and
`VERSION` provenance and regenerate Codex projections with
`tools/generate-codex-types.py` as appropriate. Curated agents remain unverified
until a trusted live check justifies a first entry; fixtures alone do not do so.

See [step 4 verification](agents-v2-step4-verification.md) for completed checks
and the sandbox limits of the local watch run.
