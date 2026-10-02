# Engine node (Linux)

Use **one main Loom** for discussions, Brain, projects, providers and harnesses.
On a GPU machine, `loom node` serves only engine management and inference. It
is a mode of the same release binary, sharing existing engine handlers and
native argument construction. It does not start the interface, Brain, MCP
servers, provider keyring reads, harness probes or harness update jobs.

The binary still contains the main application's code and embedded assets;
this first implementation reduces running services and state, not binary size.
A separate binary would currently duplicate release/install machinery while
still needing application lifecycle code. Extract one only when that boundary
is independently useful. This mode adds no inference scheduler or token loop:
llama-server and vLLM continue to execute inference. Actual throughput and
resource use depend on the native engine and hardware; no performance gain is
claimed without measurements.

## Install from a release

The node ships in the **same repository and release binary as Loom**. Starting with **v0.1.3**, run on Linux without sudo:

```sh
curl -fsSL https://raw.githubusercontent.com/lucas-lepajollec/loom/main/install.sh | sh -s -- --node
```

For a LAN/VPN address and existing engine/models:

```sh
curl -fsSL https://raw.githubusercontent.com/lucas-lepajollec/loom/main/install.sh | sh -s -- \
  --node --listen 192.168.1.20:2511 \
  --bin /opt/llama.cpp/build/bin/llama-server --models /data/models
```

The default binary is `~/.local/lib/loom-node/loom`, separate from a full Loom's
system binary. Override with `LOOM_INSTALL_DIR`. `LOOM_VERSION` can select a
published tag. SHA-256 validation and the `node capabilities` check happen
before replacing a binary or stopping a service: older releases without node
support fail without altering the existing installation.

The installer initializes isolated state, writes `loom-node.service` and enables/
starts that **user** service. It does not install `loom-ui`, `loom-engine` or
sudoers rules. On reinstall it stops the node before opening its database,
preserves credentials/model directories and a saved custom home/listener, then
restarts it. A failed binary/service installation restores the previous binary
and unit and restarts a previously active service. Newly initialized data is
retained for diagnosis; no data/model rollback or deletion is attempted. The
previous binary is retained as `loom.previous`.

`--home DIR` chooses a separate data root, `--no-start` leaves the service
stopped, and `--no-service` installs only a portable binary/data for foreground
use (stop any foreground node before reinstalling). A systemd user bus must be
available for service installation. User lingering may require administrator
setup for boot without a login. The installer cannot provision system build
dependencies or firewall rules as the node user.

For a source checkout, `make build`, place `bin/loom` at a stable user-owned
path, and follow the commands below. The release installer downloads published
binaries; it does not build unreleased source.

## Prepare an isolated node

In commands below, `loom` means the node's current binary. After the release
installer, use `~/.local/lib/loom-node/loom` (or its printed custom path) if a
system `loom` command still points to an older full installation.

Run without sudo. Keep the node binary at a stable, user-owned path, separate
from a system installation if you want independent updates.

```sh
loom node init --bin /opt/llama.cpp/build/bin/llama-server --models /data/models
loom node serve
```

`--bin` and `--models` are optional initialization options to link existing
files. Models are not copied or deleted. Without an existing engine, use the
main Loom's engine installation controls after connecting the node.

The default data root is the platform's normal Loom root with `-node` appended
(on Linux, typically `~/.local/share/loom-node`). `--home DIR` overrides it;
`LOOM_HOME`, `/etc/default/loom` and project dotenv files do not select a node's
root. `init` refuses an existing full Loom configuration. Repeating `init` keeps
credentials and configuration. It creates only engine directories and state;
there are no memory, conversation or workspace directories.

The node listens on **127.0.0.1:2511** by default. Both its protected control API
and `/v1` use this listener. The internal llama.cpp OpenAI front defaults to
loopback 2512 (native backend 12512). Existing explicit engine configuration is
retained. No engine/model starts just because the node listener starts.

To bind on a trusted LAN/VPN address:

```sh
loom node serve --listen 192.168.1.20:2511
```

Use a VPN, SSH tunnel or HTTPS reverse proxy outside a trusted private network.
Tokens over plain HTTP are visible to that network. The client refuses public
plain-HTTP links. The node does not change firewall rules.

## Connect the main Loom

In Settings → Machines, select the GPU machine, choose **Connect its Loom node**,
and enter its reachable node address and machine token. The token is in
`node.token` under the node's data root; retrieve it privately on that machine,
never put it in a URL, command argument, public configuration or shared log.
SSH registration and engine linking are independent: SSH offers harnesses and
terminals; the node offers only engines.

Management requires `Authorization: Bearer <machine token>` on **every** control
route, including ping/info. Browser session cookies and human passwords are not
accepted. `node.token` must be a regular private file (0600). The management
token and inference key are distinct random 256-bit credentials. Authenticated
`GET /api/node/info` lets the main Loom obtain the inference key and negotiate
`role: engine-node` and `v1_same_origin: true`. The link stores credentials with
existing Loom local secret state and never returns them to its browser.
Direct API clients need the inference key for `/v1`, not the management token.

The main Loom strips browser cookies/origin headers and substitutes the machine
credential when forwarding engine actions. Conversations and harness launches
stay on the main Loom. The node strips client credentials/cookies before
forwarding inference to its local native engine and uses that engine's key.

## Optional user service

```sh
loom node install --listen 192.168.1.20:2511
systemctl --user enable --now loom-node
systemctl --user status loom-node
journalctl --user -u loom-node
```

`install` writes/reloads `loom-node.service`; it does not enable/start it. Pass
the same `--home` to init/serve/install if using a custom root. It never installs
`loom-ui` or adds sudoers privileges. The node uses the separate engine identity
`loom-node-engine` and existing owned child supervision. Stopping the node stops
its owned engine processes. systemd `KillMode=control-group` also covers crashes.
For service startup without an interactive login, an administrator may need to
enable user lingering (`loginctl enable-linger <user>`).

Remove the service without deleting data/models:

```sh
systemctl --user disable --now loom-node
rm ~/.config/systemd/user/loom-node.service
systemctl --user daemon-reload
```

For updates, select a release that retains node support; the updater checks the
verified binary’s `node capabilities` before replacing the current executable.

Stop the service before replacing its binary manually or backing up its state.
Node application updates are available in the main Loom under Settings →
Machines → the connected node → Engine node updates. They use official release
assets, checksum/size verification and a retained rollback binary, just like
Loom's update flow. Only the node binary is replaced; the main Loom's version and
service stay independent. Confirmation warns that restarting `loom-node` stops
its owned engines and interrupts their active inference; models are not
automatically reloaded. The page verifies the node's new running version without
reloading the main interface. A foreground node needs a manual restart. Node
updates never use the full Loom's privileged updater. CLI equivalents:
`loom node update --check` and `loom node update` (using the installed node binary).

Engine update APIs remain available and
automatic engine updates remain opt-in. To rotate a lost/compromised management
token, stop the node, securely remove its `node.token`, run `node init` with the
same home, restart it and reconnect the main Loom. This does not rotate the
inference key or touch models.

## Supported boundary

The node registers the same handlers as the main Loom for engine status/logs,
GPU/VRAM/RAM, local model files/directories/downloads, Hugging Face search,
llama.cpp binary/source installation and updates, presets/parameters,
load/unload and vLLM lifecycle/library. vLLM still requires supported Linux
CUDA/ROCm hardware and uses its private Python environment. A node may use its
own loopback vLLM direct link, but cannot link another control plane. Unsupported
native capabilities remain unsupported. Source builds still need system build
dependencies; Loom never runs its interface as root to solve permissions.

Local one-shot benchmarks and GGUF/preset benchmark queues reuse the existing
native benchmark handlers. Node queues reject cloud choices before provider
lookup; stop vLLM before a GGUF sweep. Its active model can use the one-shot
benchmark. Servers without native phase timings leave prefill/decode rates
unknown rather than deriving an arbitrary split of elapsed time. No benchmark
starts automatically.

The authenticated inference listener also forwards read-only native `/health`,
`/props`, `/slots` and `/metrics` for engine observation. An unavailable native
endpoint remains unavailable. vLLM receives its actual model ID when a client
uses Loom's `loom` alias; explicit model IDs and other request fields are retained.
The management library remains the node's GGUF files even while vLLM serves
inference; its model library/parameters remain under the existing vLLM controls.
The main Loom reads sampling/reasoning settings from the selected node before
each native chat iteration. Failure to read them stops the turn visibly rather
than substituting a stale VM preset. A selected vLLM engine does not inherit the
previous GGUF's sampling/template settings.
Generic stop/unload/restart target the active owned vLLM engine when selected.
The displayed inference endpoint is the node listener, with a required key;
llama.cpp's internal front remains loopback. Its binding survives preset
application and unload; a model change cannot silently fall back to the main
Loom port or expose the native front. Change the node listener in its
service configuration, not through the native engine's network toggle.

No UI/auth-session, discussion, Brain/MCP, cloud/provider, harness, project,
terminal or environment routes are registered.
Unknown paths return 404. Native Windows/macOS node mode is explicitly
unsupported in this initial Linux implementation; direct inference-server links
remain available there. Migration from a previous full Loom is manual and
separate: back up data and link existing models/binaries before stopping or
removing old services.
