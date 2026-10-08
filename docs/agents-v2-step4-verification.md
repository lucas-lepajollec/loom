# Agents v2 step 4 verification

Backend and workflow implementation; no UI files changed. No commit, provider
sign-in, paid turn, GitHub issue or PR was created during implementation.
Antigravity's registration and `runtime/antigravity` were left unchanged.
Gemini was not added as a builtin.

## Passing checks

- All modified/new Go files formatted with `gofmt`; `git diff --check` passed.
- `go vet ./...` passed.
- `go test ./tools/agents-watch ./internal/loom/harness` passed.
- Focused Loom tests passed for catalogue parsing, reserved IDs, HTTPS archive
  validation, package/version normalization, launch argv for npx/uvx/binary,
  persisted daily refresh throttling, ETag/304, offline snapshot fallback,
  API add/remove and startup restoration, curated availability/compatibility,
  Claude compatibility and complete shared ACP fixture turns.
- `go test ./internal/loom/runtime/... -run Fixture` passed.
- `go test ./internal/loom -run Fixture` passed in the local watch, including
  Hermes/OpenClaw/DeepSeek normal, permission and form subprocess turns.
- `python3 tools/check-doc-links.py` passed: 157 local targets, none missing.
- The embedded registry file is byte-identical to the provided 41-agent snapshot.
- Workflow YAML, weekly/manual triggers, action major versions and report
  artifact settings were checked. `actionlint` was unavailable; installing it
  was blocked by sandbox DNS/socket restrictions.

The default Go cache is read-only in this environment. Initial checks used a
writable temporary cache; later temporary-disk quota failures required moving
this task's cache and temporary test files into ignored `.project-local`
storage. Passing final checks used `GOCACHE` and `TMPDIR` there. An earlier
`TestEngineKindFullVsServer` failure depended on the shared `/tmp` layout and
passed with the isolated temporary directory; no engine code was changed.

## Full-suite sandbox limits

The requested `go test ./internal/loom/...` was run. The final run failed in
existing tests that require listeners:

- `loom.TestHandleHubSearch`: `httptest` TCP listener, `socket: operation not permitted`.
- `platform.TestFetchReleaseChannels`: same listener restriction.

Those package suites panic at the denied listener and cannot complete in this
sandbox. Other leaf package suites passed, including the full runtime tests.
The catalogue cache tests use `httptest` handlers/recorders through an in-memory
HTTP transport, so they exercise HTTP/ETag behavior without a listener.

## Local watch observations

`make agents-watch AGENTS_WATCH_ARGS='--out .project-local/agents-watch-step4'`
ran without `--install` or `--publish`. Its final report and candidate schemas
are retained in ignored local storage.

- Installed Codex `codex-cli 0.159.2`: initialization and model listing passed
  with eight models and an empty account/profile directory. Regenerated selected
  JSON schema matched the committed snapshot.
- Installed Pi `0.84.3`: native state/model listing passed; zero available models
  were observed in the empty profile. No generation ran.
- Installed OpenCode `1.18.33`: `opencode generate` matched the committed
  OpenAPI. The owned server exited during startup in the socket-restricted
  environment, so its live HTTP handshake was not verified.
- Claude ACP executable was absent. Latest installation and registry refresh
  could not be exercised through the sandbox network. The CDN refresh failed
  DNS resolution; the watch correctly wrote a failing report.
- Runtime and application fixtures passed. Schema comparison tests include
  retained unions and drift in integers beyond float64's exact range.

Publishing code is implemented but was not executed. The scheduled GitHub
runner must verify latest npm installs and the GITHUB_TOKEN PR/issue path. A
report with a passing handshake or an auth-required skip is not proof of paid
turn, resume or native permission behavior. Curated accepted version arrays
remain empty; DeepSeek's documented launch has no upstream README/live proof
in this implementation.
