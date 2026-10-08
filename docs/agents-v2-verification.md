# Agents v2 step 1 verification

This change implements backend canonical events and durable requests, native
Codex app-server and Pi RPC adapters, native session import/resume, compatibility
records, and automatic ACP selection when the builtin CLI lacks its native
protocol. No UI files or Go dependencies changed. No commit was created.

## UI handoff

The exact [JSON contract and examples](agents-compat.md#ui-contract-backend-only)
cover the additional `agent_event` envelope, assistant replacement deltas,
item lifecycle/payloads, usage/context scopes, generic raw rows, and these
request fields:

- `request.opened.request`: `id`, `kind`, `method`, optional `item_id`,
  `approval_kind`, `options`, `questions`, `message`, `schema`, `url`, `payload`.
- Questions: `id`, optional `header`, `question`, `options`, `free_text`,
  optional `secret`. Options: `id`, `label`, optional `description`.
- `request.resolved`: `request_id`, `outcome`, optional `decision` inside
  `agent_event`. The outer request row exposes `request_id` and `outcome` too.
- Session snapshots: `pending_requests`, `native_session_file` for Pi.
- Runtime descriptors: `compatibility`, including optional `warning`.

Submit decisions/answers/form content to the shared permission service:
`POST /api/workspace/sessions/{id}/requests/{request_id}`. Existing approval
aliases and permission submission remain available. Question/form rendering is
the design owner's next step; no new interactive UI is claimed here.

## Completed checks

| Check | Result |
| --- | --- |
| `gofmt` and `git diff --check` | Passed |
| `go vet ./...` | Passed |
| `go test ./internal/loom/runtime/...` | Passed |
| Focused canonical/request/ACP/Codex tests with `-race` | Passed |
| `go test -short ./internal/loom/...` with sandbox-incompatible test exclusions | Passed across all packages |
| `make check-ui` | Passed syntax checks and all 18 test suites |
| `make build` | Passed |
| Codex Go projection generation from the committed schema | Reproduced successfully |
| Local Markdown link checks | Passed |

The fixture corpus contains 23 synthetic JSONL exchanges derived from the
official Codex 0.159.2 schema and installed Pi 0.84.3 documentation. Fake stdio
processes exercise the actual leaf adapters, canonical event order and bounded
raw preservation, native ID retention, correlated request replies, final-only
assistant messages, approvals, forms, errors, retries and interruption. Extra
tests verify malformed JSON/process exit, verbatim failed command responses,
tool-argument identity, unknown context occupancy, and cancellation before
prompt acknowledgement. Session tests verify persistence before publication,
reload replay, HTTP answer validation/duplicate rejection, turn cancellation,
restart cancellation and incomplete tool closure.

The unrestricted `go test ./internal/loom/...` was attempted and failed in
existing environment-dependent tests:

- `TestHandleHubSearch` and `platform.TestFetchReleaseChannels`: loopback
  socket creation denied by the sandbox.
- `TestEngineKindFullVsServer`: its temporary-directory ancestor detection
  classified the server fixture as a full engine.

Subsequent sandbox runs also identified socket failures in
`TestNakedFitRouterINIAndRememberedOverrides`,
`TestObservedContextDoesNotAutoloadOrGuess` and
`TestEngineWrapperRouterLoadAndTransientInput`. The filtered passing run
excluded 144 tests from socket-dependent test files and these environment
checks; some ancillary tests in those files were excluded too. This is not an
unrestricted-suite pass. A non-short MCP integration attempt also could not
fetch its reference `npx` server in the network-restricted environment; the
existing `-short` guard excludes that external integration.

## Live versus fixture verification

- **Live Pi:** native `--mode rpc --no-session`, `get_state` and
  `get_available_models` passed. The observed model list was empty; this does
  not establish an authenticated account/model entitlement.
- **Live Codex:** executable version/help and official schema generation
  succeeded. `initialize`/`model/list` acceptance was attempted, but app-server
  startup failed under the sandbox's native-state write restrictions, including
  when using a temporary documented SQLite location. A successful installed
  app-server handshake is still required outside that restriction.
- **Fixtures only:** generation, tool execution, approvals, questions/forms,
  file changes, runtime error/interrupt behavior and native resume calls. No
  real account turn or paid generation was used.

## Remaining acceptance work

- Render the question/form/URL requests and replacement deltas using the UI
  contract. Legacy approval/tool/text consumers continue receiving aliases.
- Verify actual CLI/app-to-Loom and Loom-to-CLI continuation with an authorized
  test account or local test model, including process shutdown durability and
  simultaneous-window behavior. Reported Codex lock/busy errors are translated
  clearly, but a CLI that does not enforce locks cannot offer exclusive writers.
- Native Codex/Pi MCP execution uses CLI-owned configuration. Loom-scoped MCP
  provisioning is not implemented in this slice; explicit selections fail
  visibly and the adapters do not advertise `mcp`.
- Native historical import hydrates portable text only. Historical tool cards,
  private approvals and hidden reasoning are not reconstructed. Very large
  Codex full-history replies hit the 4 MiB transport frame bound.
- The versions tested by fixtures are fixed snapshots. A different installed
  version receives a visible non-blocking warning; refresh schema/docs/fixtures
  deliberately before treating it as tested.
