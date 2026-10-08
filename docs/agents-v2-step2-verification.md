# Agents v2 step 2 verification

Backend-only implementation on `agents/v2-claude-opencode`. No Go dependencies,
UI edits, paid turns, commits, login, quota resets or global installs.

## What was verified live

- Installed CLI versions: Claude Code **2.1.294**, OpenCode **1.18.33**.
- Installed OpenCode help advertises `serve` and `acp`. Its built-in
  `opencode generate` succeeded in isolated temporary XDG directories. The
  committed unedited 1.18.33 snapshot comes from the binary's `Server.openapi()`;
  it is not claimed as a live `/doc` capture.
- Cached Claude ACP **0.84.0** accepted `initialize` with object-valued form/URL
  capabilities, `_meta.terminal_output` and negotiated AIR `sessionFailure`.
  The handshake reported protocol 1 and adapter 0.84.0. No `session/new` or
  prompt was sent. This does not establish live 0.88 compatibility.
- `npm pack @agentclientprotocol/claude-agent-acp@0.88.0 --ignore-scripts` was
  attempted with a temporary cache; DNS failed with `EAI_AGAIN`. The supplied
  study checkout's 0.88.0 changelog and source were inspected directly, and
  `npm pack . --ignore-scripts` there produced a temporary 0.88.0 package
  manifest/README/license archive. That checkout has no compiled `dist`, so
  no 0.88 subprocess was executed and no install scripts ran.

## Fixture verification

Claude synthetic fixtures cover single/multiple questions, multi-select, Other
and single-choice notes, Skip, cancellation, form/URL elicitation, ExitPlanMode,
and rate-limit/auth/overloaded metadata on updates and prompt results. Complete
fake stdio turns verify question/plan replies and failed completion even when
upstream returns `stopReason:end_turn`. Terminal metadata tests retain reported
output and exit codes; canonical validation rejects multiple single-choice picks
and duplicate answers. OpenCode fallback completes fake ACP turns
verify initialization, native IDs, text and completion.

OpenCode fixtures exercise the HTTP session/create/resume path, prompt body,
stream subscription before send, text delta/snapshot reconciliation, tools,
plans, scoped/deduplicated usage, permission/question replies and reject, legacy
permission endpoint, abort and verbatim errors. Server subprocess fixtures
check lazy singleton ownership, random auth, forced loopback/mDNS-disabled
launch, health, refusal of an unsecured server, close and owned restart.
The tests use a real httptest listener where possible. Here socket creation was
denied, so the same handlers ran via streaming pipes and httptest writers;
tests explicitly log that restriction. This verifies adapter behavior, not
live OpenCode server/TUI interoperability.

Compatibility tests cover every requested ACP family, actual launcher pin
retention, handshake versions and untested-version warnings. Hermes, OpenClaw
and Antigravity have no newly accepted native release in this step;
their tested-version set remains empty. Missing versions are never invented.
OpenCode launch tests check that probes/native choices receive no Loom keys
and a selected Loom provider receives only its own key. Existing Codex/Pi
corpora remain routed to their own stdio drivers; new ACP/HTTP corpora use their
corresponding replay tests.

## Checks

- `gofmt` and `git diff --check`: passed.
- `go vet ./...`: passed (temporary Go/ccache directories were necessary because
  the ordinary caches are read-only).
- Focused agent/ACP/HTTP/provider tests with `-race`: passed.
- `go test ./internal/loom/...`: attempted; all runtime packages and other leaf
  packages passed. Existing failures prevent a full-suite pass:
  `TestHandleHubSearch` and `platform.TestFetchReleaseChannels` panic because
  loopback sockets are denied; `TestEngineKindFullVsServer` classifies the test's
  `/tmp` ancestor as a full engine. These tests were not changed or hidden.
- `make build`: passed.
- `make check-ui`: passed syntax checks and all 18 regression tests.
- Local documentation link checks: passed.

## Acceptance still needed

Run the live 0.88 handshake and authenticated OpenCode `/doc`, session/provider
listing, TUI resume and concurrency checks in an environment that permits local
sockets and adapter retrieval. Generation still requires an explicitly
authorized test account/local model; none was used here. Native OpenCode V2
permission/question variants fail visibly, and concurrent native TUI answers
close Loom cards only at turn settlement. CLI-owned MCP configuration remains
the native HTTP path; an explicit Loom MCP selection fails visibly. Enabling
Loom model sources selects the launch-scoped ACP path.

The exact additional UI fields are documented in
[agent compatibility](agents-compat.md#additional-backend-fields-for-the-ui-owner):
`multi_select`, `optional`, `approval_kind:plan`, and additive compatibility
metadata. UI files were not modified. `COMMIT_MSG.txt` contains the proposed
commit text; no commit was created.
