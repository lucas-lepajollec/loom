# Contributing to Loom

Thank you for contributing. Search existing issues and pull requests first, open an issue before a large architectural change, and keep each pull request focused on one coherent outcome.

## Development

### Prerequisites

- Go 1.25 or later
- GNU Make
- Node for `make check-ui` (no UI build step)
- An engine or harness for real execution checks; Loom can install llama.cpp or link an existing server
- Git, CMake and a C/C++ toolchain if compiling llama.cpp; macOS native menu-bar builds also need a C toolchain

### Build from source

```bash
git clone https://github.com/lucas-lepajollec/loom.git
cd loom
make build
```

This embeds the web UI (`internal/loom/ui/next`, no build step) and outputs the binary to `bin/loom`.

### Running in development

```bash
./bin/loom web 2510
```

Access the UI at `http://127.0.0.1:2510`. Use the first-run guide to install or link an engine, or use cloud providers/harnesses. Settings › Engines manages engines; `./bin/loom edit` can configure `BIN` for an existing `llama-server`. Read the [architecture principles](docs/architecture-principles.md) before runtime changes.

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
Use `make help` to list all targets. For documentation changes, run
`python3 tools/check-doc-links.py` (Python 3, standard library only) to check
local Markdown/HTML file targets; remote URLs and heading fragments are not
checked.

## Pull requests

- Use a short-lived descriptive branch and clear conventional commits.
- Explain the problem, approach, user-visible effect, validation and remaining limitations.
- Update tests, documentation and `CHANGELOG.md` when applicable.
- Never include credentials, private data, generated runtime state or unrelated dependency churn.
- Report vulnerabilities through [SECURITY.md](SECURITY.md), not a public issue.

By participating, you agree to follow [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md). Contributions are made under the repository's `LICENSE`.

For the Linux node installer acceptance check, build the candidate and run as a
normal user: `LOOM_TEST_BINARY="$PWD/bin/loom" sh tools/tests/install-node.sh`.
It uses verified local artifacts and a simulated systemd user bus, never real
host services or upstream installation. Native GPUs, vLLM environments and actual
service boot/restart behavior still need separate platform acceptance.
