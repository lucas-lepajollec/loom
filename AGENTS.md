# Loom agent guide

This file is public repository guidance for maintainers and AI agents. Inspect the current branch, working tree, code, configuration and documentation before changing anything. Preserve unrelated work.

## Product boundaries

Loom is a workstation control plane, OpenAI-compatible proxy, and test bench for llama.cpp. It operates a single owned `llama-server` process without vendoring or reimplementing llama.cpp. Conversations, settings, presets, and model files reside strictly on the local machine. Telemetry, external cloud dependencies, and unmanaged process killing are strictly out of bounds.

## Development

- Setup requires Go 1.24+ and a functional `llama-server` or llama.cpp build.
- Build the binary with `make build` (assembles embedded UI into `internal/ajean/ui/index.html` and compiles `bin/loom`).
- Run the web dashboard in development with `make web` or `./bin/loom web 8091`.
- Run validation tests with `make test` or `go test -short ./...`.
- Rebuild UI assets with `make assemble-ui` after modifying files in `internal/ajean/ui/src/`.

## Repository expectations

- Update tests, `README.md`, focused docs and `CHANGELOG.md` when public workflows, API endpoints or CLI flags change.
- Never commit model files (`*.gguf`), local database state, `.env` values, private machine paths or logs.
- Follow `CONTRIBUTING.md` for pull requests and `SECURITY.md` for vulnerabilities.
- GitHub is the public review surface; maintainers integrate the exact accepted result into authoritative Forgejo history.

Local machine notes belong in ignored `AGENTS.override.md` and `.project-local/`, never in this public file.
