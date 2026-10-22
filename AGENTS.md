# Loom agent guide

Public guidance: inspect branch, working tree, code and documentation before
editing; preserve unrelated changes. Local notes belong in ignored
`AGENTS.override.md` or `.project-local/`. Never commit credentials, private
machine paths, model files or local state.

## Product boundaries

Loom is a local-first workspace for Local and Cloud discussions, engines,
projects, Brain, skills, MCP, terminals, machines and voice. Coding harness
integration is retired. Do not restore native agent installation, account
login, model catalogs, sessions, quotas, ACP or other agent protocols. Existing
transcripts and display events remain readable for migration; they cannot
launch a retired executor. See [migration](docs/harness-retirement.md).

The Agents module is an interface for an independent Codeman server, not a
harness layer: Codeman runs the native CLIs and their accounts; Loom relays its
official `/api/v1`, SSE and terminal WebSocket through `/api/codeman/*` with
server-side credentials. Do not add agent SDKs, model calls for agents, CLI
adapters or Codeman internals. See [Agents architecture](docs/agents/architecture.md).

Loom controls external engines without reimplementing inference. Keep engine
and model state separate; llama.cpp router model changes use its API rather
than restart the server. Remote nodes expose only engine, terminal, observe and
optional voice modules. Cloud providers use explicit credentials and consent.
MCP clients are configured manually; Loom never writes native client settings.
Brain source files remain authoritative; indexes are derived data. Preserve
current voice/Jarvis, policy, notifications and memory behavior within this scope.

## Working method

- Inventory local/cloud, engine/node and UI callers before changing contracts.
- Reuse existing packages and official protocols before adding dependencies.
- Test regressions with deterministic fixtures and no paid model calls.
- Distinguish implemented, verified and actually observed behavior.
- Preserve the current Loom design and responsive components; no alternate chat.
- Keep secrets server-side, enforce auth/origin protection and preserve policy
  checks for tools, terminals, external sharing and sensitive operations.

## Development

Use Go 1.26.9 or newer compatible toolchain and Node for UI tests.
`make build` embeds `internal/loom/ui/next`; `make check-ui` checks modules and
runs free UI regressions. `go test -short ./...` covers Go behavior. Validate
changed docs with `python3 tools/check-doc-links.py`.

`make dev` uses isolated `.project-local/runtime`, port2594. LAN phone testing
requires an access password; see [contributing](CONTRIBUTING.md). Rebuild after
UI changes. Never overwrite production data to test a checkout.

PWA caches only the public offline fallback and listed brand icons, never
conversations, credentials, API/SSE traffic or queued sends. External context
sharing requires confirmation. Unknown usage stays unknown; refresh cannot
start generation. Model output escapes raw HTML.

Update focused docs and CHANGELOG for changed public contracts. Follow
CONTRIBUTING and SECURITY, plus the maintainer's public review/release process.
