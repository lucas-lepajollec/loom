# Contributing to Loom

Thank you for contributing. Search existing issues and pull requests first, open an issue before a large architectural change, and keep each pull request focused on one coherent outcome.

## Development

### Prerequisites

- Go 1.25 or later
- GNU Make
- A local installation or build of `llama.cpp` (`llama-server`)

### Build from source

```bash
git clone https://github.com/lucas-lepajollec/loom.git
cd loom
make build
```

This embeds the web UI (`internal/loom/ui/next`, no build step) and outputs the binary to `bin/loom`.

### Running in development

```bash
./bin/loom web 8091
```

Access the UI at `http://127.0.0.1:8091`. Configure `BIN=` to point to your `llama-server` binary via Settings → Engine or via `./bin/loom edit`.

## Validation

Before opening a pull request, validate Go packages and the embedded UI:

```bash
go test -short ./...
go vet ./...
make check-ui
make build
```

`make test` runs the full Go suite (`go test ./...`); it is not the short-mode
command above. Node is required for `make check-ui`, not for `make build`.
Use `make help` to list all targets.

## Pull requests

- Use a short-lived descriptive branch and clear conventional commits.
- Explain the problem, approach, user-visible effect, validation and remaining limitations.
- Update tests, documentation and `CHANGELOG.md` when applicable.
- Never include credentials, private data, generated runtime state or unrelated dependency churn.
- Report vulnerabilities through [SECURITY.md](SECURITY.md), not a public issue.

By participating, you agree to follow [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md). Contributions are made under the repository's `LICENSE`.
