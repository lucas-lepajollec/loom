# Loom agent guide

This file is public repository guidance for maintainers and AI agents. Inspect the current branch, working tree, code, configuration and documentation before changing anything. Preserve unrelated work.

## Product boundaries

Loom is a conversation-first, local-first AI workspace. A discussion owns its portable transcript and project context independently of the model or harness chosen for a turn. Do not split discussions into local/cloud products. Models owns the local library/Hub, providers, cloud models and selector visibility; Harnesses owns adapter configurations and model associations. The common path supports llama.cpp, configured Chat Completions providers and experimental Antigravity and Codex text bridges. Other harness execution and terminals remain unimplemented. Antigravity uses its own CLI/account catalog; do not export provider keys, change native global permissions or claim arbitrary-model compatibility. Read `docs/architecture-principles.md` and `docs/architecture.md` before changing runtime or product structure; agents continuing implementation follow `docs/agents/implementation-brief.md`. The visual design of `ui/next` (CSS, tokens, layout of existing screens) is owned by the design owner: reuse existing components, do not restyle, and see `docs/workspace-architecture.md`. Loom owns one external `llama-server` without reimplementing it; a router-capable engine is started once and models load through its API (`backend_router.go`), never by restarting the engine. External selection requires explicit confirmation before sharing transcript/context. Runtime-private memory, approvals and tool state are not portable. Catalog/connection/quota actions dispatch through the registry in `workspace_runtime.go` and optional `Connectable`/`QuotaReader` interfaces. Native account reads remain explicit; planned descriptors have empty capabilities. Do not build a parallel memory engine. Telemetry and unmanaged process killing remain out of bounds.

## Development

Preserve the original discussion/composer and complete local parameters. The one original sidebar adapts to local, cloud or harness state; unsupported harness profiles remain non-executable previews. Do not reintroduce a second simplified chat or a redundant header. Common local discussions bind to the existing Conversation pipeline through workspace_native.go. External portability remains text-only. Usage snapshots distinguish account windows, retained Loom token counts and manual-price estimates; missing data is unknown, not zero. Refresh never starts generation or redeems resets.

- Setup requires Go 1.25+ and a functional `llama-server` or llama.cpp build.
- Build the binary with `make build` (embeds `internal/loom/ui/next` and compiles `bin/loom`). Check the UI with `make check-ui`.
- Run the web dashboard in development with `make web` or `./bin/loom web 8091`.
- Run validation tests with `make test` or `go test -short ./...`.
- UI regression tests: `node --test internal/loom/ui/tests/*.test.mjs` (Node required only for this test command).
- Reasoning families are a UI projection of discovered native IDs, not invented runtime models. Keep per-response provenance in the display journal and out of model-visible context; unknown historical metrics stay unknown. AGY average output-token throughput is not llama.cpp decode throughput. Never reconstruct hidden reasoning or claim a file diff from a reported write target.
- The default interface is `internal/loom/ui/next/`: native ES modules with vendored Preact + htm, no build step, served at `/` (and `/next/`). Edit its files directly and rebuild the binary; styles live in `ui/next/css/`, the conversation engine in `ui/next/js/features/chat/engine.js`. Model output is rendered through `features/chat/md.js`, which escapes raw HTML.

## Repository expectations

- PWA caching is restricted to the public offline fallback and listed brand icons. Never cache conversation HTML/JSON, credentials, API/SSE traffic or queued sends. Installing on a non-loopback host requires a secure browser context; LAN HTTP is a preview, not an installability guarantee.
- Update tests, `README.md`, focused docs and `CHANGELOG.md` when public workflows, API endpoints or CLI flags change.
- Never commit model files (`*.gguf`), local database state, `.env` values, private machine paths or logs.
- Follow `CONTRIBUTING.md` for pull requests and `SECURITY.md` for vulnerabilities.
- GitHub is the public review surface; maintainers integrate the exact accepted result into authoritative Forgejo history.

Local machine notes belong in ignored `AGENTS.override.md` and `.project-local/`, never in this public file.
