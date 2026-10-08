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
