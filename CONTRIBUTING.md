# Contributing to Loom

Thank you for contributing. Search existing issues and pull requests first, open an issue before a large architectural change, and keep each pull request focused on one coherent outcome.

## Development

### Prerequisites

- Go 1.25 or later
- GNU Make
- A local installation or build of `llama.cpp` (`llama-server`)

### Build from source

```bash
git clone git@github.com:lucas-lepajollec/loom.git
cd loom
make build
```

This assembles the web UI assets (`make assemble-ui`) and outputs the binary to `bin/loom`.

### Running in development

```bash
./bin/loom web 8091
```

Access the UI at `http://127.0.0.1:8091`. Configure `BIN=` to point to your `llama-server` binary via Settings → Engine or via `./bin/loom edit`.

## Validation

Before opening a pull request, run the test suite and verify UI assembly:

```bash
make test
make assemble-ui
make build
```

## Pull requests

- Use a short-lived descriptive branch and clear conventional commits.
- Explain the problem, approach, user-visible effect, validation and remaining limitations.
- Update tests, documentation and `CHANGELOG.md` when applicable.
- Never include credentials, private data, generated runtime state or unrelated dependency churn.
- Report vulnerabilities through [SECURITY.md](SECURITY.md), not a public issue.

By participating, you agree to follow [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md). Contributions are made under the repository's `LICENSE`.
