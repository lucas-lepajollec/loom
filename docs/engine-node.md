# Add a machine with Loom Node

1. In Loom, open **Machines › Add a machine**. On the other Linux machine,
   run the copyable command as its normal user:

   ```sh
   curl -fsSL https://raw.githubusercontent.com/lucas-lepajollec/loom/main/install.sh | sh -s -- --node --listen lan
   ```

   This installs one Loom Node user service, without sudo. It installs no
   engines, models or agents. macOS and Windows nodes are not available yet.
2. Enter the address and pairing code printed by the installer, or choose the
   node from Loom's live network discovery list and enter its code. Codes expire
   after ten minutes and work once. Both machines must be able to reach the
   selected listener on port **2511**.
3. Loom opens the paired machine and displays its detected **engine, harness,
   terminal and observe** modules. Use that page to manage what runs there.
   Module availability is read-only; the node currently has no module-toggle API.

Existing SSH machines show **Connected over SSH (old method)**. Select
**Install Loom Node over SSH** to install, start and pair automatically using
that saved connection. **Pair manually** opens the same code dialog. Migration
keeps the machine ID, name, favourite folders, agent installation choices and
terminal history. Existing harness launchers switch to the Node bridge.
Progress and errors appear in place; a failed migration leaves the SSH record
intact. The installed remote service may remain running and can be retried.
Already-open SSH terminals continue using their existing process until closed;
new terminals use the node's advertised terminal module.

Cards show **Update available** when the node is behind the latest release on
this main Loom's channel. **Update node** and the running version appear at the
top-right of its page. A node service update restarts its user service and stops
its owned engines, agents and terminals; Loom asks before applying it.

## Node runtime

Use **one main Loom** for discussions, Brain, projects, providers and harnesses.
On a paired machine, `loom node` serves engine management, inference and
optional harness and terminal transports. It is a mode of the same release binary, sharing existing engine handlers and
native argument construction. It does not start the interface, Brain, MCP
servers, provider keyring reads or harness update jobs. Harness inventory is probed only
when requested; agents and terminals start only for explicit Loom requests.

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

Without an explicit listener, a **non-TTY** installer defaults to
**127.0.0.1:2511** (or preserves an existing saved listener). On a TTY it asks:
“Make this node reachable from your other machines on the local network? [Y/n]”.
Accepting, or passing `--listen lan`, selects the primary private IPv4 address
from `ip route get 1.1.1.1`, then falls back to `hostname -I`. Only RFC1918 and
CGNAT/Tailscale **100.64.0.0/10** addresses are selected automatically; no public
or wildcard address is selected. No private address means an explicit error.
`--listen local` selects loopback; `--listen ADDR:PORT` selects an explicit
listener. Each choice persists across reinstallations.

A foreground `loom node serve` defaults to **127.0.0.1:2511** unless configured. Both its protected control API
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

### Change the listener later

Use the installed node binary (and `--home DIR` for a custom data root):

```sh
~/.local/lib/loom-node/loom node listen lan
~/.local/lib/loom-node/loom node listen 192.168.1.20:2511
~/.local/lib/loom-node/loom node listen local
```

This rewrites the existing user unit, persists the listener and restarts
`loom-node.service`. A failed listener change restores the previous unit and
listener. No sudo or firewall changes are performed.

After a started release installation or update, the installer prints the
address, a fresh pairing code and its UTC expiry. If already paired it prints
`already paired with <loom>` and creates no new code. `--no-start` and
`--no-service` do not claim a running node or print a ready-to-pair block.

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
token. The Machines stepper uses its authenticated backend API. Its authenticated, origin-protected
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
{"machine_token":"private","inference_key":"private","node":{"id":"persistent-node-id","name":"GPU machine","version":"loom-version","role":"engine-node","modules":["engine","harness","terminal","observe"],"handshake":1}}
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
an existing matching machine retains its ID, name, harness choices and
folders while switching SSH launchers to the Node bridge. For manual migration,
include `"machine_id":"existing-id"` in the pairing request: it selects that
record even when the new node listener differs from the old SSH host. Pairing does not change the active inference engine.

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
Existing SSH machines remain editable while awaiting migration;
the terminal module also opens real PTYs without SSH.

All other management routes, including ping/info, require
`Authorization: Bearer <machine token>`. Browser cookies and human passwords
are not accepted by a node. `node.token` must be a regular private file (0600).
The management token and inference key are distinct random 256-bit credentials.
Authenticated `GET /api/node/info` retains its existing fields and adds `id`,
`name`, `handshake:1` and `modules:["engine","harness","terminal","observe"]` by default. Unknown handshake majors are
refused with an update message. Missing handshake fields remain accepted only
for legacy token links. Disabled harness, terminal and observe modules are omitted.
Direct API clients need the inference key for `/v1`, not the management token.

The main Loom strips browser cookies/origin headers and substitutes the machine
credential when forwarding engine actions. Conversations stay on the main Loom; a selected node harness executes on the
paired machine through the authenticated ACP transport below. The node strips client credentials/cookies before
forwarding inference to its local native engine and uses that engine's key.

## Run agents on a paired machine

Pair by code as above; SSH is not required for harnesses. Nodes advertise
`modules:["engine","harness","terminal","observe"]` with `handshake:1` by default, including existing
nodes after updating. To disable harness execution while retaining read-only observation, initialize with:

```sh
loom node init --no-harness
# Restore the harness module explicitly, using the same node home:
loom node init --no-harness=false
```

The persisted node state flag is `node_harness_disabled` (true disables it).
Repeating init without either flag preserves the choice. Refresh/re-pair the
main's saved link after changing advertised modules. Disabling the module also
rejects subsequent inventory, ACP and folder/workspace requests with HTTP 409
and `{"ok":false,"error":"node harness module is disabled"}`.

Install and sign in to the native tools on the node as its OS user. The module
uses the same probe, tool directories on PATH, prerequisites and pinned ACP
launchers as SSH machines: Codex, Claude Code, Pi, OpenCode, OpenClaw and Hermes.
This does not add support for harnesses absent from `remoteHarnessDefs`.
It does not install tools, perform native sign-in or change native permissions.

Pairing tries to read inventory; a failed probe does not undo a successful pair.
To refresh it, use the existing main API `POST /api/machines` with
`{"machine":{"id":"node-machine-id"},"check_only":true}`; omit `check_only`
and supply `harnesses` to save the refreshed inventory and chosen connections.
`GET /api/agents/installations` includes the node's detected agents with the
existing machine/family/runtime IDs. Remote installations remain unmanaged and
disabled until selected. `POST /api/agents/installations` with
`{"machine":"node-machine-id","harness":"hermes","enabled":true}` enables
one. Discussion configuration still requires explicit confirmation before
sharing transcript/context with the external executor. Backend APIs are ready;
new node-specific UI controls are separate work.

The node accepts only its machine token in `Authorization: Bearer …` on these
routes; inference credentials, browser cookies and URL credentials do not grant
access. Redirects are refused by the main. Use the same trusted LAN/VPN or HTTPS
boundary as for engine control; this module does not configure TLS.

| Node route | Response / behavior |
| --- | --- |
| `GET /api/node/harness/inventory` | `{"ok":true,"os":"Linux","home":"/home/agent","hostname":"worker","tools":[{"id":"hermes","path":"/home/agent/.local/bin/hermes","version":"…"}]}`; versions are optional. |
| `GET /api/node/harness/acp?harness=hermes&cwd=/absolute/folder` | WebSocket upgrade; binary frames carry unmodified stdin/stdout bytes, including partial or multiple newline-delimited JSON messages. Token is only in the handshake header. |
| `GET /api/node/folders?path=/absolute/folder` | `{"ok":true,"path":"/absolute/folder","parent":"/absolute","folders":["/absolute/folder/subdir"]}`; directory paths only, sorted by name. An empty path selects the node user's home. |
| `POST /api/node/workspace` with `{"path":"/absolute/folder"}` | Explicitly creates a workspace directory (including parents); returns `{"ok":true,"path":"/absolute/folder"}`. |

ACP accepts only a known harness ID and an absolute existing directory; empty
cwd selects the node user's home. The default limit is eight concurrent agent
processes, including probes; contention returns HTTP 429 with
`{"ok":false,"error":"node agent process limit reached"}`. Closing either
transport direction kills/reaps the owned process group and releases its slot.
Node shutdown also kills its owned agent groups. Stderr is drained continuously,
with only the first 32 KiB per agent written to the node log.

The hidden main-side command is `loom node-bridge <machine-id> <harness> <cwd>`.
It reads the saved maintenance URL/token from local secret state and bridges its
stdio to the WebSocket; no token appears in arguments or URLs. `nodeAgent`
records `@loom-workdir` as its last argument. `runACP` replaces that placeholder
with the session's remote `Workdir` before launching the child; probes replace
it with `RemoteHome`. The local child still starts in the main user's home.
The ACP client and protocol implementation are unchanged.

For encrypted state, the main's vault unlock key remains in its process. The
launch wrapper reads only the selected machine access and writes a private,
encrypted, temporary launch record using a fresh independent key. The bridge
receives the record path/key through its environment, consumes/removes the
record, and clears those environment variables. The parent also removes the
record on failure/close. No vault key, inference key or provider key is passed
to the node or its agents. A manually invoked bridge cannot read locked state.

Main-side `GET /api/machines/folders?machine=ID&path=/absolute/folder` proxies the
node picker response above. Without `path`, the existing `{"ok":true,"folders":
[…]}` favourite-folder response remains unchanged. Workspace validation and
explicit creation also use the node, without SSH. Terminal transport is an
independent module, described below.

## Terminals on a paired machine

The independent `terminal` module is on by default, including existing nodes
when updated. Disable it with `loom node init --no-terminal`, or restore it with
`loom node init --no-terminal=false`, using the same node home. The persisted
`node_terminal_disabled` flag is preserved when init omits the option. Restart
the node and refresh/re-pair the main's saved link after changing modules.
Disabling the module returns HTTP 409 with
`{"ok":false,"error":"node terminal module is disabled"}` on new requests.
Harness and observation opt-outs do not disable terminals.

`GET /api/node/terminal/ws?cwd=&command=&cols=&rows=` requires the machine
Bearer token in the Authorization header, never a URL credential. It upgrades
to a WebSocket and starts the node user's login shell (`$SHELL`, or `/bin/sh`)
in an absolute existing directory; empty `cwd` selects that user's home. A
supplied command runs through the shell with `-lc`. Default dimensions are
100 columns by 30 rows; valid sizes are 1–1000 columns and 1–500 rows. Invalid
cwd, command or dimensions return HTTP 400 before a process starts.

Binary output and text keystrokes use the same framing as local terminals.
Both text and binary input reach the PTY; text `{"resize":[cols,rows]}` is a
resize control message. The main sends keystrokes as binary so JSON typed into
a shell stays input. A completed process sends text
`{"exit":true,"exit_code":7}` (with its actual exit code). Closing the connection
or stopping the node kills the owned process group and reaps the shell. Each
node allows eight concurrent terminals independently of harness slots;
contention returns HTTP 429 with `node terminal process limit reached`.

On the main, `POST /api/terminals` with
`{"target":"paired-machine-id","dir":"/absolute/folder","command":""}` opens
the node PTY through the saved protected maintenance credential. No local SSH
client is needed. The existing registry, request IDs, 30-second single-use
browser tickets, resize and recent-output replay stay shared with local/SSH
terminals. Closing a browser socket leaves the main-to-node connection and
process alive; a new ticket reattaches. Closing the terminal, stopping either
Loom process or losing the node connection ends it. Node transport reconnection
does not replay a command or start a replacement process. An unavailable exit
code remains `-1`.

`GET /api/machines` retains each machine's advertised `modules` and adds
`capabilities:["terminals"]` when it has SSH or a paired node advertising
`terminal` (otherwise `capabilities:[]`). A node target with this module uses
its node even when SSH is also configured. A machine with neither terminal
module nor SSH retains the clear `terminals need SSH for now` error. Backend
only; the existing terminal UX is reused.

## Machine metrics

The independent `observe` module is on by default. Disable it with
`loom node init --no-observe`, or restore it with `--no-observe=false`.
The persisted `node_observe_disabled` flag is preserved when init omits the flag.
Refresh/re-pair links after changing advertised modules. Disabled observation
returns HTTP 409 with `{"error":"node observe module is disabled"}`.

These backend-only, read-only endpoints return `Cache-Control: no-store`:

| Route | Response |
| --- | --- |
| `GET /api/machines/local/metrics` | This machine's `MachineMetrics`. |
| `GET /api/node/observe` | Node `MachineMetrics`; requires its machine token. |
| `GET /api/machines/{id}/metrics` | Proxies an advertised `observe` node, or samples a saved SSH machine. |
| `GET /api/machines/metrics` | `{"metrics":{"local":{…},"machine-id":{…}|{"error":"…"}}}`. Poll every ten seconds. |

```json
{
  "at": 1700000000000, "cpu": 25, "load1": 0.8, "cores": 8,
  "ram_used": 8589934592, "ram_total": 17179869184,
  "disk": {"path": "/home/user", "used": 107374182400, "total": 536870912000},
  "gpus": [{"name": "GPU", "util": 40, "vram_used": 2147483648, "vram_total": 8589934592}],
  "uptime_seconds": 3600, "os": "linux"
}
```

`at` is Unix milliseconds; RAM, disk and VRAM sizes are bytes. CPU/GPU usage is
0–100 percent; `load1` is the one-minute load average. Disk describes the home
filesystem (used = total minus free blocks, including reserved space in total).
Linux CPU uses two `/proc/stat` samples about 300 ms apart, excluding duplicate
guest counters and counting idle/iowait as idle; RAM uses `MemAvailable`.
Local/node samples, including existing NVIDIA/AMD SMI/ROCm readers, are cached
for two seconds and share in-flight reads. Empty `gpus` means no supported reader
returned data. macOS/Windows reuse known RAM/disk data with `"partial":true`;
unknown CPU/load/uptime values are zero. Incomplete Linux samples also set
`partial:true`: unavailable observations must not be treated as measured zeros.

SSH uses one fixed command with a five-second deadline and the same parsers;
samples and failures are cached per machine for ten seconds. Observation never
generates SSH keys. Non-Linux SSH returns exactly `{"supported":false}`.
Individual failures return HTTP 502 with `{"error":"…"}`. Aggregate collection
runs concurrently, with five-second remote deadlines and a six-second overall
collection deadline; failures remain beside successful samples. Remote and
aggregate requests require an unlocked vault when encrypted. No observation
starts inference, installs tools, signs in or exports context. The Machines page polls these observations.

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
Node application updates are available from **Update node** at the top-right
of its Machines page. They use official release
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

No UI/auth-session, discussion, Brain/MCP, cloud/provider, harness lifecycle,
project or environment routes are registered. The main `/api/terminals` routes
are absent; node terminals use only `/api/node/terminal/ws`. The optional harness
module exposes only inventory, ACP transport and directory/workspace control;
the independent terminal module exposes PTY transport, and the optional
observe module exposes read-only machine metrics.
Unknown paths return 404. Native Windows/macOS node mode is explicitly
unsupported in this initial Linux implementation; direct inference-server links
remain available there. Migration from a previous full Loom is manual and
separate: back up data and link existing models/binaries before stopping or
removing old services.

## Maintenance for each connected machine

Pairing saves the same maintenance link used by the **Machines** page.
Use **Pair again** to repair the link with a fresh pairing code. The same page can check and
apply a Loom release for that node even when a different engine serves current
discussions. Linking an engine through a matching machine address remembers its
maintenance access automatically. The token is stored with local secrets,
never returned in machine JSON or saved in browser storage. Removing the
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
parameters. No UI, Brain, discussion or harness is started automatically by the node. vLLM
startup runs with Hub/Transformers offline flags, without automatic dependency
installation or inference. Main Loom permits `off`/`vllm`; llama.cpp starts via
its installed engine service. Simultaneous saved llama.cpp service and vLLM
startup are rejected. Direct inference links have no lifecycle/startup endpoint.

These settings do not create arbitrary service units or deploy a node. Older
nodes need an update before exposing their engine policy. Unit and real boot
behavior require acceptance on the actual target machine.

## SSH migration API

- `POST /api/machines/{id}/node/migrate` explicitly installs the official release
  on an existing Linux SSH machine using its saved user, host, port and SSH key.
  It starts the node, obtains a fresh code privately over SSH and pairs without
  forcing another main's association. The request has a fifteen-minute deadline.
- `GET /api/machines/{id}/node/migrate` returns the current `phase`:
  `installing`, `pairing`, or an empty string when no request is active. Both
  routes require main Loom authentication and an unlocked vault when enabled.
- A successful POST returns `{ok:true,machine:{…}}`; failures return
  `{ok:false,error:"…"}`. Pairing codes and node credentials never reach the UI.
  Contending migrations receive 409. Unsupported existing custom launchers or
  missing harness-module support reject the merge before changing any record.
- Machine JSON, protected maintenance credentials and replacement harness
  launchers commit in one store transaction, with retries on concurrent edits.
  Installation choices and terminal history retain their original machine ID.
  If pairing succeeded but the local save failed, run the migration again or
  generate a fresh code on the node and pair manually.
- `GET /api/machines/{id}/node/update` and its `/apply` route forward the main's
  release channel per request. They leave the node's saved channel unchanged.
  Release updates atomically replace `~/.local/lib/loom-node/loom`, preserve
  `loom.previous`, verify checksums/node capability, and restart the user service.
