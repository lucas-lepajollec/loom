# Agent protocol compatibility

Loom's local builtin Codex runtime prefers the installed `codex app-server`;
local builtin Pi prefers `pi --mode rpc`; local builtin Antigravity prefers
`agy --input-format stream-json --output-format stream-json`. A read-only CLI help probe selects
native transport when the protocol exists. A missing protocol selects the
existing pinned ACP launcher. Authentication, startup, resume and RPC failures
never silently retry a prompt through a different runtime. Remote/custom
launchers retain ACP. Native accounts and global permissions remain owned by
the CLI. No Go dependencies were added.

The accepted contracts include **codex-cli 0.159.2 and 0.162.0** and
**Pi 0.84.3 and 1.1.0**. Codex's selected
schema snapshot and generated Go message projections live in
`internal/loom/runtime/codexapp/`. Complex unions remain `json.RawMessage` so a
new variant survives translation. The official [app-server contract](https://developers.openai.com/codex/app-server)
and the installed Pi package's `docs/rpc.md` define the wire protocols.

### October 2026 acceptance

The October 8 watch verified Codex 0.162.0 initialization and eight catalog
models, OpenCode 1.18.35 health/catalog with unchanged OpenAPI, Claude ACP
0.88.0 initialization/catalog, and Pi 1.1.0 state/catalog (no configured models).
These are read-only probes plus synthetic fixtures, not live paid-turn tests.
Older accepted releases remain in `tested_versions.json`; the Claude launch pin
is unchanged. The refreshed ACP registry artifact is byte-identical to the
already committed snapshot.

Codex's 0.162.0 snapshot adds `partial_answer` alongside `commentary` and
`final_answer`. All phases retain native payloads and normal text reconciliation;
only `turn/completed` settles a turn. Missing phases keep legacy behavior.
`ThreadItem.subAgentActivity` adds optional resolved model/effort metadata and
stays an informational raw row. `ResponseItem.additional_tools` is catalog
metadata: raw response notifications retain it without inventing execution.
`CodexErrorInfo` now admits arbitrary strings/objects; errors and retry warnings
keep the native message verbatim and the complete bounded error details, including
`misalignment.reviewTarget`. An opaque review target never starts continuation.
Optional `Turn.rootTurnId` stays in the native payload; generated `TurnStartParams`
accepts `parentTurnId`/`rootTurnId`, but ordinary user turns send neither.
The existing 0.159.2 exchanges and new metadata/phase/error fixtures replay together.

Pi package inspection with `npm pack --ignore-scripts` could not download 1.1.0
in this sandbox (`EAI_AGAIN` resolving the npm registry). Review instead used the
version-tagged [changelog](https://github.com/earendil-works/pi/blob/v1.1.0/packages/coding-agent/CHANGELOG.md),
[RPC types](https://github.com/earendil-works/pi/blob/v1.1.0/packages/coding-agent/src/modes/rpc/rpc-types.ts),
[RPC mode](https://github.com/earendil-works/pi/blob/v1.1.0/packages/coding-agent/docs/rpc.md),
and [event reference](https://github.com/earendil-works/pi/blob/v1.1.0/packages/coding-agent/docs/json.md).
The `--mode rpc --no-session` launch, command correlation and `success/error`
envelopes remain supported, as do Loom's state/model/resume/configuration/prompt/
abort/history/stats commands and message/tool/retry/extension UI events.
The 1.0 changes concern native UI/tools/providers; the RPC lane remains available.
Pi 1.1.0's `agent_settled.aborted` maps to interruption even without an aborted
message. A prompt acknowledgement with `disposition:handled` completes the
submitted input without waiting for a run that never started; older acknowledgements
still wait for native settlement. Tool `durationMs` stays in its payload.

Acceptance checks passed: native/ACP fixture replay, runtime/harness/watch tests,
`go vet ./...`, `make build`, `make check-ui`, documentation links, workflow YAML
parsing and exact projection regeneration. The full
`go test ./internal/loom/... ./tools/...` run is blocked by sandbox sockets in
`TestHandleHubSearch` and `TestFetchReleaseChannels` (`socket: operation not permitted`).
An initial `TestEngineKindFullVsServer` failure from unrelated temporary-directory
contents passed with an isolated temporary directory. GitHub publication/CI
dispatch remains a workflow integration check; no review writes ran locally.

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

Loom's `native_session_id` is Codex's thread ID, Pi's native session ID, or
Antigravity's conversation ID.
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
python3 tools/generate-codex-types.py /tmp/loom-codex-schema --version "$(codex --version)"
python3 tools/generate-codex-types.py
```

The import requires `--version` and records it in `schema/VERSION`; append the
accepted release to `tested_versions.json`, and update this document and fixtures
together. `TestedVersion` derives from that accepted list. The committed snapshot
merges only used message schemas and their definitions; rerunning the generator
from it reproduces the Go file.
For Pi, inspect the package changelog and `docs/rpc.md`, update its accepted list
and fixtures together, and check every command's `success/error` contract.

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
| Antigravity | Native `agy` NDJSON; Loom `agy-acp` fallback | CLI 1.3.1 help/version/binary inspection; synthetic process fixtures | Text, tools/commands/reported file targets, per-step spend, native conversation IDs, effort/modes/sandbox, errors, outcomes, raw rows | No headless permission/question replies; no accepted reasoning/plan/context-occupancy schema; no history-list/read API |

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

## Agents v2 step 3: Antigravity

The builtin selects `agy-stream-json` when read-only help advertises input and
output stream JSON plus explicit conversation resume. Otherwise it retains
`loom agy-acp`; older structured-output CLIs without NDJSON input use the
bridge's one-shot `--print` invocation. Remote/custom ACP launchers remain explicit. Selection happens
before any transcript is sent. Startup, account, busy/lock, resume and process
failures never retry a turn through the bridge. Antigravity remains separate
from Gemini CLI, with its native account/catalog and no Loom provider keys.

### Structured-path investigation

| Option | Evidence and decision |
| --- | --- |
| Native `agy` print stream | Installed 1.3.1 help/changelog/binary types (`steps.StreamEvent`, `steps.StepUpdatePayload`, `steps.ToolInfo`, `steps.JSONUsage`); [Google headless contract](https://antigravity.google/docs/cli/headless/). Selected: a documented protocol on the installed executable, without an additional runtime or private DB decoding. |
| Google ACP runtime | The [ACP registry entry](https://github.com/agentclientprotocol/registry/blob/main/antigravity-acp/agent.json) distributes proprietary Google `agy_acp_server.par`/`.exe` 1.2.1 separately (Linux argv includes `--uid=`). It is not an `agy --acp` mode advertised by this CLI. Not installed, downloaded, authenticated or accepted here. |
| Loom bridge | Existing `loom agy-acp` translates the CLI into ACP and remains the compatibility fallback. Its synthetic approval/retry and file display behavior is a legacy bridge feature, not native stream permission RPC or a native reported diff. |

The supplied Poracode study and Apache-2.0 Antigravity implementation were read
as references, without wholesale copying. Its registry ACP integration normalizes
model/effort IDs, suppresses output after interruption, and parses a stderr
reply boundary while background tasks keep `session/prompt` open. These runtime
quirks require separate exact-server acceptance before replacing the installed
CLI lane; the older study recommendation predates this CLI investigation.

### Accepted mapping and gaps

Each turn writes one `user`/`message.content` NDJSON frame and closes stdin.
`init`, `step_update`, and `result` retain bounded raw provider JSON. Step indices
identify items within Loom's turn request ID, never replace the conversation ID.
Text deltas reconcile with final-only/corrected responses. Tool errors and final
errors remain verbatim; stderr diagnostics use the existing bounded credential
redaction. Only owned child processes are stopped. Completion follows stdout
drain and exit verification, so a success result followed by a nonzero exit fails.

Command items retain command/output; file items retain native target/output.
There is no filesystem diff reconstruction. Unknown messages, checkpoint,
subagent metadata and future reasoning/plan/context variants retain raw rows.
The accepted stream contract exposes no separate reasoning delta, structured
plan or context occupancy. `--mode plan` remains available; its reported answer
is text. These missing capabilities are not advertised or simulated.

Per-step usage snapshots are deduplicated by step index and summed once at
settlement as `usage.spent` with scope `turn`. Missing/invalid token fields stay
unknown. The final raw result retains cumulative session usage/duration; those
counters never become current-turn spend or context occupancy. Spent usage rows
also no longer project a legacy `context.used` value in the shared mapper.

Headless input rejects `control_request`/`control_response`; it has no permission
or question reply channel. Native policy denials remain failed tool items and
raw denied-action metadata and a visible denial warning. Native `DONE` is step
completion; an uncorrelated denial list never becomes an invented per-command
execution result. Loom sends no invented replies, approval cards or
automatic continuation prompts. A visible warning explains this gap. Native
permission settings remain active. Only an explicit full permission/mode
selection adds `--dangerously-skip-permissions`; plan mode suppresses the full
permission setting. Edits maps to `--mode accept-edits`. Selected
`config_options.sandbox:true` passes `--sandbox`, independently of mode; this is
the CLI terminal sandbox, not a new Loom filesystem-confinement guarantee.

### Native continuity, catalog and compatibility

`native_session_id` is the exact `conversation_id` received from init, steps or
result. Compatible subsequent turns launch with `--conversation ID` and just the
new prompt; native CLI/IDE turns remain in that conversation. Loom never uses
`--continue`. A mismatched returned ID is a resume failure. A changed route or
prepared context uses the existing explicit portable text handoff. Native errors
report busy/locked conversations without starting a new session or killing a
window; simultaneous IDE/CLI behavior still needs real-platform acceptance.

Models use the existing read-only `agy models` parser. Effort configuration uses
the installed help's `low`, `medium`, `high`, `xhigh`, `max` values and passes
`--effort`; these are CLI choices, not a promise that every model accepts all
values. Unsupported selection fails natively. No accepted CLI session-list or
history-read API exists here; history actions fail clearly rather than invoking
Codex or privately decoding native databases. No usable Antigravity discovery
API existed in the bridge. Native ID resume remains available, and other agents'
discovery/import paths are unchanged.

Compatibility records include `protocol:agy-stream-json`, `adapter_package:agy`,
`tested_version:1.3.1`, `tested_versions:[1.3.1]`, observed `version`/
`agent_version` from a read-only version check, and the actual capabilities.
Unknown versions remain empty. A differing observed version emits a visible,
non-blocking drift warning; it never substitutes another runtime.

Backend only: existing configuration shapes add discovered `reasoning_effort`
(select) and `sandbox` (boolean) options, and the existing mode list is populated.
No new top-level UI fields, endpoint or UI files. Native `payload.tool_info` and
file `payload.path` are provider-shaped metadata within the existing event field.
Explicit Loom MCP selections fail visibly; native CLI MCP remains CLI-owned.

### Verification boundary

`testdata/agents/antigravity/*.jsonl` is synthetic, derived from Google docs and
installed type/help inspection, not recordings of paid outputs. Fake owned
processes exercise normal/deduplicated usage, final-only/reconciled text,
command/file/error items, unavailable permission/question interaction, busy
errors, interrupt, unknown rows, missing results and nonzero exits. Additional
checks cover cancellation, malformed JSON, stderr redaction, native ID mismatch,
unknown counters, launch policy, read-only probes, fallback selection, native
resume versus text handoff, and the canonical display projection.

Live checks: `agy --help`, `agy help models`, `agy --version` (1.3.1),
`agy changelog`, binary/type inspection. The read-only `agy models` attempt
was blocked by `listen tcp 127.0.0.1:0: socket: operation not permitted` and
read-only native log/crash paths. No generation, login, permission mutation,
quota reset, paid turn or ACP server installation was performed.

See [step 3 verification](agents-v2-step3-verification.md) for commands,
sandbox failure names and the live-versus-fixture acceptance boundary.

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
contents, PR, issue and Actions permissions; the latter dispatches CI on the
review branch. No account or provider secrets are needed.
The repository must allow GitHub Actions to create pull requests. The Go script
in `tools/agents-watch/` is runnable locally:

```sh
make agents-watch                         # check installed CLIs; write report only
make agents-watch AGENTS_WATCH_ARGS=--install  # latest npm releases, temporary prefix
make agents-watch AGENTS_WATCH_ARGS=--update-capabilities # refresh installed-agent baselines
make agents-watch AGENTS_WATCH_ARGS='--install --update-capabilities' # refresh latest baselines
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

The watch captures raw capability announcements before Loom's typed projections:
ACP `initialize` (agent capabilities, auth methods, agent info and `_meta` keys),
plus no-account `session/new` modes/models/config options and immediate command
notifications; Pi `get_state`, `get_commands` and `get_available_models`; Codex
`initialize` and all `model/list` pages; OpenCode `/config/providers`, `/agent`
and `/command`. Unsupported optional endpoints and account-required discovery
remain explicit in snapshots. The watched ACP package is Claude ACP; registry
refresh does not execute every catalog agent.

Stable JSON snapshots live in `internal/loom/harness/capabilities/<agent>.json`.
Keys and unordered announcement lists are sorted, duplicates removed, stable
command/mode/config IDs become diffable object keys, and volatile session IDs, paths, timestamps, versions, current selections and counts
are dropped. Model catalogs record the union of field shapes, efforts and flags,
not account-dependent model identities, inventory size or prices. `_meta` records
only extension keys, not private values. Unknown announcement fields are
retained. Model config-option inventories use the same projection as model
catalogs.
The report lists added, removed and changed JSON-pointer paths (first 80); the
full path diff and candidate JSON are artifacts. Auth-gated fields remain unknown.

Missing snapshots automatically produce **baseline created**, not an attention
item. The workflow proposes these baselines in its review PR on the first run;
normal local runs only write candidate artifacts. `--update-capabilities` writes
the observed snapshots into the checkout for review, including changed ones.
It still reports detected changes and other failures; it does not accept versions.
Use it separately from `--publish`, which writes only its isolated review tree.

For a version absent from the tested list, the watch fetches GitHub releases
between the latest tested version and the candidate, excluding the tested release.
Sources are `openai/codex`, `earendil-works/pi`,
`agentclientprotocol/claude-agent-acp`, and OpenCode's installed npm
`package.json` repository (falling back to `sst/opencode` if unavailable).
The installed npm tarball's `CHANGELOG.md` is a fallback. Reports, issues and
PR bodies include source links and headings/bullets bounded to 80 lines per
agent; long lines are also bounded. Network errors, missing notes or an
incomplete bounded release range produce **release notes unavailable**, without
failing checks or creating attention by themselves. Fetches send no credentials.

Schema and capability diffs detect advertised protocol changes, including
features for agents without a schema. Fixtures check Loom's mapped behavior;
handshakes do not exercise turns, tools, approvals or authenticated catalogs.
Features exposed only in an agent's own UI are surfaced through release notes,
not tests. An upstream feature omitted from both announcements and release notes
can still go undetected.

The script regenerates the Codex schemas consumed by Loom using
`codex app-server generate-json-schema --out …` and the OpenCode OpenAPI using
`opencode generate`. It compares parsed JSON with committed snapshots, ignoring
formatting/key ordering while retaining every field/union. Changed schemas
produce candidate JSON, a unified diff and a bounded changed-path summary.
It runs `go test ./internal/loom/runtime/... -run Fixture` and the application
ACP fixtures, refreshes/validates the registry, and writes a Markdown report
uploaded as a workflow artifact and included in the job summary.

With `--publish`, passing candidates open/update
`agents-watch/<UTC-date>` from the checked HEAD in a temporary worktree. Only
`tested_versions.json`, passing candidates' capability snapshots, and a
successfully refreshed registry snapshot are included. Local publishing requires
a clean checkout; the script does not commit local work.
A capability change, schema change, installation/probe failure or failed fixture
check opens or updates one exact-title issue per affected candidate:
`Agents watch: <agent> <version> needs attention`. A registry failure also opens
its own attention issue, even when candidate issues exist. Failed candidates
are excluded from the version PR. A capability change alone proposes the new
snapshot and version in the PR while also opening an attention issue with its
path diff; the maintainer reviews the announcements before acceptance. Passing
candidates still publish when another agent or the registry needs attention. A failed registry refresh keeps the old
snapshot. Both review channels are attempted, including after a publication
failure. Automation returns a failing status after publishing whenever attention
is needed; publication errors also fail the job.

PRs created/pushed with `GITHUB_TOKEN` do not automatically run CI without
maintainer intervention (some GitHub configurations offer an approval banner).
The watch explicitly dispatches `ci.yml` on its dated branch after PR creation
or update; CI supports `workflow_dispatch` with only `contents:read` permission
and no provider secrets. A maintainer must verify CI passed for the current PR
head before merging. For local publishing or a failed dispatch, run
`gh workflow run ci.yml --ref agents-watch/<UTC-date>`, or close/reopen the PR or
push a change using maintainer credentials to trigger PR CI. See GitHub's
[workflow trigger rules](https://docs.github.com/en/actions/how-tos/write-workflows/choose-when-workflows-run/trigger-a-workflow).

To accept a new version, review release notes and the watch report/skips, inspect
capability and schema diffs and adapt mappings/fixtures when needed, then rerun the checks. Merge the review
PR or append the verified version to the agent's array in `tested_versions.json`
in a reviewed change along with its capability snapshot (refresh locally with
`make agents-watch AGENTS_WATCH_ARGS=--update-capabilities`). For a changed native
contract, refresh its schema and `VERSION` provenance and regenerate Codex projections with
`tools/generate-codex-types.py` as appropriate. Curated agents remain unverified
until a trusted live check justifies a first entry; fixtures alone do not do so.

See [step 4 verification](agents-v2-step4-verification.md) for completed checks
and the sandbox limits of the local watch run.
