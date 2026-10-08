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

The node ships in the **same repository and release binary as Loom**. Starting with **v0.1.4**, run on Linux without sudo:

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

On its first unpaired start the node logs one pairing code, for example
`K7QM-4XPA`. To replace that code (including after expiry), run locally on the
node machine, while its service continues running:

```sh
loom node pair
# With a custom node data root:
loom node pair --home /data/loom-node
```

Codes contain eight Crockford base32 characters, omit ambiguous letters, expire
after **10 minutes** and work **once**. The node stores only the code's SHA-256
hash and expiry, plus attempt counters; five wrong attempts invalidate it.
Failures are also limited by the request's remote IP (five failures per ten-minute
window), across source ports and code replacements. Service restarts do not
print the first-start code again. Keep that short-lived log/code private.

The main Loom backend now supports pairing without SSH or copying a machine
token. UI integration is separate. Its authenticated, origin-protected
`POST /api/machines/pair` accepts:

```json
{"address":"http://192.168.1.20:2511","code":"K7QM-4XPA"}
```

The main Loom sends `POST /api/node/pair` to that address:

```json
{"code":"K7QM-4XPA","main":{"id":"persistent-main-id","name":"Main Loom","version":"loom-version"}}
```

This is the only unauthenticated node control route; possession of the code
authorizes the exchange. A successful response goes **only to the main Loom
server**, consumes the code and records the main's ID, name and `paired_at`
(Unix milliseconds) on the node:

```json
{"machine_token":"private","inference_key":"private","node":{"id":"persistent-node-id","name":"GPU machine","version":"loom-version","role":"engine-node","modules":["engine"],"handshake":1}}
```

**Plain LAN HTTP exposes both credentials once in this exchange response to
anyone able to observe that request.** Use a trusted network or an HTTPS endpoint
when TLS is already configured (for example a reverse proxy); an explicit
`https://` address is retained, certificates are verified, and redirects are
refused. This feature does not configure TLS or open firewall ports.

The main stores the credentials in the existing machine maintenance secret
record, encrypted when the vault is enabled/unlocked, and returns only
`{"ok":true,"machine":{…}}`. The machine includes `node_id`, `modules` and
`handshake`, with its usual ID/name/host fields. A new entry uses the node's name;
an existing matching machine retains its SSH configuration, harnesses and
folders. Pairing does not change the active inference engine.

Errors use `{"ok":false,"error_code":"…","error":"…"}`: `502 unreachable`,
`401 invalid_code` (invalid/expired/consumed), `429 rate_limited`,
`409 handshake_mismatch`, or `409 already_paired`. For a valid code presented
by another main, the node's `409` also includes `main:{id,name,paired_at}` and
names that main. To explicitly replace it, generate a fresh code and add
`"force":true` to the main's pairing request; it forwards this flag. Force
still requires a valid, unconsumed code. Re-pairing updates the association;
it does not rotate credentials or revoke a previous main's retained token.

### Find nodes on the LAN

`GET /api/machines/discover` broadcasts `LOOM?` to UDP port **2512** on every
active, non-loopback IPv4 broadcast interface, waits about **1.2 seconds**, and
returns nodes not already linked here:

```json
{"nodes":[{"id":"persistent-node-id","name":"GPU machine","version":"loom-version","address":"http://192.168.1.20:2511","port":2511,"paired":false}]}
```

This does not pair, generate a code or start inference. Nodes listen for probes
only when their HTTP listener extends beyond loopback. Responses are rate-limited
and contain no secrets. `paired:true` means a recorded main already exists;
that node can still be explicitly re-paired. Discovery uses IPv4 broadcast on
the local subnet; it does not cross routers/VPNs that block broadcast. A firewall
must permit UDP 2512 separately from the node's TCP listener. UDP 2512 can coexist
with llama.cpp's private TCP 2512. If UDP is unavailable, discovery returns
`{"nodes":[]}` and a node logs discovery startup failure while its API stays up.
Discovery advertises the native HTTP endpoint; supply the HTTPS reverse-proxy
address explicitly when using TLS.

### Token fallback and handshake

The existing **Connect its Loom node** address + token flow remains available.
The token is in `node.token` under the node's data root; retrieve it privately
on that machine, never put it in a URL, public configuration or shared log.
SSH registration and node linking are independent: SSH offers harnesses and
terminals; the node currently offers only engines.

All other management routes, including ping/info, require
`Authorization: Bearer <machine token>`. Browser cookies and human passwords
are not accepted by a node. `node.token` must be a regular private file (0600).
The management token and inference key are distinct random 256-bit credentials.
Authenticated `GET /api/node/info` retains its existing fields and adds `id`,
`name`, `handshake:1` and `modules:["engine"]`. Unknown handshake majors are
refused with an update message. Missing handshake fields remain accepted only
for legacy token links. Future `harness`/`observe` modules are not advertised.
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
native benchmark handlers. Mixed queues and saved tests/history belong to the main Loom. The main dispatches
only local rows to the node, using its existing queue API. Updated nodes advertise
`scoped_cancel`; cancellation supplies `X-Loom-Bench-Job` and refuses a different
current job atomically. Older nodes can run tests but need a manual stop when the
main queue is cancelled; their stored responses may be preview-only. Temporary
custom tests are deleted after dispatch; an unreachable node can retain one.

Node queues reject cloud choices before provider
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

## Maintenance for each connected machine

Pairing saves the same maintenance link used by the **Machines** page. As a
fallback, open a machine and configure its engine-node address and management
token under **Engine node management**. The same page can check and
apply a Loom release for that node even when a different engine serves current
discussions. Linking an engine through a matching machine address remembers its
maintenance access automatically. The token is stored with local secrets,
never returned in machine JSON or saved in browser storage. Removing the SSH
machine removes its saved maintenance credential; it does not uninstall the
remote node or remove its data.

`GET/POST /api/machines/{id}/node` observes/saves maintenance access (POST
`{url,key}`, or `{unlink:true}`). `GET /api/machines/{id}/node/update` checks the
node's official release, `POST .../update/apply` applies the reviewed version,
and `GET .../update/ping` observes its restart. These authenticated,
origin-protected routes require an unlocked vault when encryption is enabled.
Only the saved node receives its own credential; redirects are refused. Node
updates use the same checksum verification, installer capability checks and
restart handling as the existing active-node update. Applying a release still
requires confirmation and may interrupt the node's engines.

## Startup from the main interface

Settings → Startup lists installed Loom units on the selected machine. On Linux,
`GET/POST /api/startup` manages the current installation; SSH machines use
`/api/machines/{id}/startup` for the fixed `loom-ui`, `loom-engine` and `loom-node`
units. Arbitrary unit names and shell fragments are rejected. Changing enabled
state never adds `--now`, starts a service, or stops a running workload. Missing
units must first be installed normally. System-unit changes require root or
noninteractive sudo authorization; errors remain visible rather than prompting
for a password through an HTTP process.

`loom-node` is a systemd user service. Without lingering it may start only after
login. The interface reads `loginctl` state and warns when before-login startup
is not established. An administrator can explicitly enable lingering for the
node account (`loginctl enable-linger USER`); Loom does not grant that itself.
Other operating systems currently report this management capability unsupported.

Linked engine nodes also expose `/api/startup` with their existing machine token;
main Loom proxies it through `/api/machines/{id}/node/startup`. A node policy
selects `off`, `llama.cpp` or `vllm` (a cached Hub model ID is required for vLLM).
It is applied on the next node start using existing supervision and saved native
parameters. No UI, Brain, discussion or harness is started by the node. vLLM
startup runs with Hub/Transformers offline flags, without automatic dependency
installation or inference. Main Loom permits `off`/`vllm`; llama.cpp starts via
its installed engine service. Simultaneous saved llama.cpp service and vLLM
startup are rejected. Direct inference links have no lifecycle/startup endpoint.

These settings do not create arbitrary service units or deploy a node. Older
nodes need an update before exposing their engine policy. Unit and real boot
behavior require acceptance on the actual target machine.
