# Contributing to Loom

Thank you for contributing. Search existing issues and pull requests first, open an issue before a large architectural change, and keep each pull request focused on one coherent outcome.

## Development

### Prerequisites

- Go 1.26.8 or later
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

### Test uncommitted changes from a phone

`make dev` rebuilds and runs the current checkout on port **2594**, with separate
data in `.project-local/runtime` and development service names. It does not install
or update a production instance. Set an access password in that instance's
Settings → Security and data first, or use:

```sh
make build
LOOM_HOME="$PWD/.project-local/runtime" ./bin/loom password
make dev DEV_HOST=192.0.2.10
```

Replace `192.0.2.10` with the development computer's LAN address. On a phone on
the same network, open `http://<computer-LAN-address>:2594` and sign in with the
**development** password. If the host firewall blocks access, allow this port
only for the intended network. No public tunnel, HTTPS certificate, commit, push
or release is required for this LAN browser test.

After source changes, stop the foreground process with Ctrl-C, rerun the same
`make dev` command and reload the phone's page. Both Go and embedded UI changes
are rebuilt; this command does not provide automatic hot reload. Development
data persists across restarts. `DEV_HOME`, `DEV_PORT` and `DEV_HOST` can be
overridden. Keep development data and engine ports separate from production;
native harness accounts and processes still belong to the same OS user.

`LOOM_WEB_HOST` overrides the listener address for one foreground Loom process,
without writing its saved network setting. A non-loopback address still requires
an access password or existing control key. The default `make dev` listener is
loopback; specifying a LAN address is explicit.

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
