# Loom architecture

Target architecture and migration plan. Read with
[`architecture-principles.md`](architecture-principles.md) (the rules),
[`workspace-architecture.md`](workspace-architecture.md) (discussion/runtime
contracts and historical slices), and [`ROADMAP.md`](ROADMAP.md) (current work).
The leaf package split is complete; §4 records the extracted boundaries and
the application orchestration that deliberately remains in `loom`.
The design goal is to keep extensions focused on a domain folder and a
registry entry. Current lifecycle and domain integration still require Loom
application handlers; §4 distinguishes those from the extracted leaf packages.

Human interface access uses the single-owner password/session flow in
[`access.md`](access.md). `web_login.go` owns credential/session persistence and
the CLI recovery flow; `web/` supplies origin protection. Control keys retain
compatibility for automation, independently of browser cookies and `/v1` keys.

## 1. What Loom is made of

```
                       ┌──────────────── UI (ui/next) ────────────────┐
                       │ app/ shell + routes · features/* · ui/ kit   │
                       └───────────────┬──────────────────────────────┘
                                       │ HTTP /api/* (+ SSE)
┌──────────────────────────── Go server (loom web) ─────────────────────────┐
│ web/        HTTP primitives, auth, public assets, SSE/WebSocket helpers  │
│ discussion/ Loom-owned conversations, routing, shared context, usage      │
│ runtime/    RuntimeAdapter registry: local · openai-compatible · harness  │
│ engine/     llama.cpp router helpers; vLLM lifecycle stays in loom       │
│ resources/  projects, skills, MCP definitions (shared across runtimes)    │
│ tools/      internet, MCP client, memory tools for the local tool loop    │
│ store/      bbolt + encryption + snapshots                                │
│ platform/   service/install/tray/paths/update/network per OS              │
└───────────────────────────────────────────────────────────────────────────┘
        │                         │                          │
   llama-server (router)   cloud Chat Completions    harness CLIs (agy, codex…)
```

Ownership rule: **Loom owns intent, persistence and the shared context;
engines and harnesses own execution and their private state.**

## 2. Core contracts (the only things the core knows)

### 2.1 Engine — local inference servers

```go
type Engine interface {
    ID() string                      // "llama.cpp", "vllm"
    Capabilities() EngineCaps        // router, presets, slots, metrics, estimate…
    Start(ctx) error; Stop() error; Status() EngineStatus
    Load(ctx, ModelConfig) error     // never restarts the engine when router-capable
    Unload(ctx, model string) error
    Endpoint() string                // OpenAI-compatible base URL
    Params() []ParamSpec             // data-driven parameter registry (below)
    Estimate(ModelConfig) Estimate   // VRAM/RAM, optional
}
```

The in-package `Engine` interface and stateless `llamaCppEngine` wrapper are now
implemented in `engine.go`; lifecycle orchestration remains in package `loom`.
Helpers, argument construction and router orchestration now live in
`engine/llamacpp/` (see §4), with explicit inputs/state. Existing
load/unload/estimate handlers delegate through it. Argument construction still
has one implementation (`llamacpp.BuildServerArgs`);
`buildLlamaServerArgsForConfig` resolves Loom-owned inputs and delegates to it.
The original helper delegates through the lifecycle wrapper. Empty `ModelConfig` selects the saved current
configuration; explicit inputs resolve in memory and require a reachable router
for transient loading. They never overwrite saved model settings. Service policy
remains in `loom`; child supervision and caches delegate through
compatibility wrappers to an explicit `llamacpp.Supervisor`, and router execution
to `llamacpp.Router`.
Contexts are checked before lifecycle actions; cancellation of an already-started
legacy service/router job is not provided by this wrapper.

The contract above is schematic: the current caps expose router, presets, slots
and estimate; status exposes active/ready/model; `Estimate` retains the existing
VRAM estimate type. This wrapper is not a generic multi-engine registry.
vLLM is implemented separately in `engine_vllm*.go`, with its own lifecycle,
ParamSpec catalog and Settings UI; direct and linked Loom engines use
`engine_direct.go` and `engine_node.go`. See [engines](engines.md).

**ParamSpec** now replaces hard-coded UI tables (`META`, `KV_OPTS`… in
`features/inspector/params.js`): `{id, flag, key, label, tip, tier
(essential|advanced|expert), kind, choices, min, max, affects_vram,
requires_reload}`, served by `GET /api/engine/params`. Expert tier stays
auto-generated from `llama-server --help`; curated tiers come from a JSON file
versioned in `internal/loom/engine/llamacpp/params/llamacpp.json`. Adding a generic flag to
Advanced requires one JSON entry and rebuilding the embedded binary. Entries
are ordered, with `kind` (`number`, `enum`, `bool`, `textarea`), optional choice
pairs `[value, label]`, and `requires_flag` for controls gated on installed help.
Special `control`/`when` values keep context/layers/reasoning/vision/KV behavior
model-dependent. `native_kind` preserves the installed flag's wire form (for
example `FIT=off` resolves to the valued native `--fit off`), independently of its UI control.
`available` is visibility; `supported` means the native help actually advertised
the flag. Composite/model controls do not claim native support by their presence.
Aliases merge without duplicating curated flags in Expert; hidden, deprecated and
curated-covered Expert flags are filtered. `expert_excludes` preserves the former
panel exclusions. `/api/llama-flags` keeps its legacy tier maps for API clients.

Bare loading now seeds `FIT=on` and leaves CTX/NGL unset; binaries advertising
`--fit` receive it without the former native context/all-layers defaults.
Explicit settings and remembered values retain precedence. Unsupported builds
retain historical native-context/all-layers arguments. `EXTRA_ARGS` remains
explicit and last-wins. Estimates are advisory; the UI no longer writes an
estimated context into an untouched draft. `ctx_effective` is nullable observed
context per slot, read only for the loaded active router section from native
`/props?model=…&autoload=false`. Missing, loading, malformed, legacy single-mode
or transient-variant observations remain unknown; `ctx` retains its legacy
requested/native fallback meaning. Observation never starts a model or saves
native fitted values. Real-GPU fitting still needs separate acceptance.

### 2.2 RuntimeAdapter — who answers a discussion turn

The ordered registry and runtime contracts live in `internal/loom/runtime`;
`runtime_compat.go` preserves the existing Loom names and signatures. Cloud and
Antigravity protocols (native CLI stream, with ACP bridge fallback) and the local
adapter live in `runtime/{openai,antigravity,local}`
behind Loom compatibility wrappers. Startup registration remains in
`workspace_runtime.go`; optional connect/quota HTTP dispatch stays in `web_runtimes.go`.
Planned descriptors stay capability-less. The current contract is a typed,
synchronous event sink, with cancellation through context and sink backpressure:

```go
type EventSink[Event any] func(Event) bool
type RuntimeAdapter[Message, Caps, Event any] interface {
    Descriptor() RuntimeDescriptor
    Run(context.Context, RuntimeTurn[Message, Caps], EventSink[Event]) ([]Message, error)
}
```

The message result preserves the local tool-loop/compaction contract. Loom projects
adapter payloads into the canonical `discussion.DiscussionEvent` journal at the
application boundary; native state and transport controls stay out of portable
messages. `runtime.HarnessEvent` is a neutral tool display type, not owned by
Antigravity. The following channel-based shape remains a future design sketch,
not an interface implemented by the current adapters:

```go
type RuntimeAdapter interface {
    Descriptor() RuntimeDescriptor           // id, name, kind, description, capabilities
    Models(ctx) ([]ModelChoice, error)       // native catalog (harness) or configured list
    Run(ctx, Turn) (<-chan Event, error)     // normalized event stream
    Stop(sessionID string) error
}
// optional capability interfaces, discovered with type assertions:
type Connectable interface { Connect(ctx, consent bool) error }
type QuotaReader interface { Quota(ctx) (QuotaSnapshot, error) }
type Approver   interface { Answer(ctx, approvalID string, allow bool) error }
type SkillSink  interface { ProjectSkills(ctx, []Skill) (LoadedReport, error) }
```

- **Registry**: `registerRuntime(adapter)` at init delegates to an explicit
  `runtime.Registry` owned by Loom; `runtimeCatalog()` and `/api/workspace` read
  its ordered snapshots. No registry global is added to the leaf package.
- **Generic HTTP**: `POST /api/runtimes/{id}/connect`, `GET/POST
  /api/runtimes/{id}/quota`, `POST /api/runtimes/{id}/approval`. The current
  per-harness routes (`/api/workspace/antigravity/connect`,
  `/api/workspace/codex/connect`) become aliases, then are removed.
- **Descriptor carries UI copy** (description, CLI name, consent text) so the
  Harnesses page renders any adapter without a hard-coded `INFO` map.
- **Capabilities are honest**: a capability is declared only when implemented
  and tested (`chat`, `stream`, `cancel`, `usage`, `native-events`,
  `reasoning-summary`, `approvals`, `skills`, `mcp`, `resume`).

### Harness lifecycle API

The embedded `harness/inspect.json` describes binary/version commands, Unix and
Windows install argv, update argv, prerequisites and optional npm/GitHub latest
version sources. An HTTPS `.sh`/`.ps1` argument denotes an official installer:
Loom downloads it to a temporary file and executes its interpreter. Local
commands use exec directly (Windows npm shims use Node); SSH targets reuse
Loom's key and `sshArgs`, with the probe's PATH preamble and quoted argv. Native
Windows OpenSSH targets use encoded PowerShell commands and a Windows probe.

- `GET /api/harness/lifecycle?target=local&id=codex` checks without installing.
  `target` defaults to `local`; otherwise it is a saved remote machine ID.
  The response is `{ok, state, log}`; `state` contains `installed`, `version`,
  `latest`, `update_available`, `channel`, `can_update`, `can_repair`,
  `repair_path` when recoverable, `requires_missing`, `auto`, `last_auto`,
  `unverified`, and any probe `errors`. Unknown versions are empty strings;
  they never trigger updates. Logs retain at most 32 KiB.
- `POST /api/harness/lifecycle` accepts `{target,id,action}` where action is
  `check`, `install`, `update` or `repair`. Each pair has one action at a time (409 for
  contention), a 15-minute timeout, and cancellation of its owned process tree.
  Paired machines dispatch through the authenticated Node harness lifecycle
  endpoint with controller `node.install`/`node.update` policy enforcement;
  recipes execute as the Node user and return their logs. Remote install/update refreshes and saves the machine description and its
  existing links; local actions invalidate inspection and refresh ACP probes.
  Linking still uses `POST /api/machines` with `{machine,harnesses}` or the
  local built-in/custom ACP registry; installation does not grant transcript
  sharing consent. `/api/runtimes/{id}/update` delegates to the same service,
  including linked remote runtime IDs, retaining `log` and local `inspection`.
- `POST /api/harness/lifecycle/auto` accepts `{target,id,auto}`. Auto is off by
  default and stored independently for each pair. After the web port is bound,
  a cancellable background cycle checks every six hours, including a startup
  check when due. Only installed harnesses with a known newer published version
  are updated, while no matching discussion is running; new turns wait during
  the automatic update. `last_auto` is null or `{at,from,to,ok,log}` (milliseconds
  since Unix epoch), retained across restarts and disabling auto. Missing latest
  sources or failed version reads never mean that the installed version is current.

Commands are sourced in the catalog from official upstream documentation:
[Codex](https://github.com/openai/codex/blob/main/README.md),
[Claude Code](https://code.claude.com/docs/en/setup),
[Gemini](https://geminicli.com/docs/get-started/installation/),
[Pi's previous package](https://github.com/badlogic/pi-mono/blob/v0.60.0/packages/coding-agent/README.md),
[Hermes](https://hermes-agent.nousresearch.com/docs/getting-started/installation),
[OpenCode](https://opencode.ai/docs/), and
[Antigravity](https://antigravity.google/docs/cli/install/).
The requested `@mariozechner/pi-coding-agent` package is retained with
`unverified: true` because current Pi docs name a different npm package.
Hermes compares its version with GitHub's latest release; its source installer
tracks main and upstream still owns source updates. Antigravity has no verified
read-only latest-version source in this catalog, so its manual **Check and
update** action uses `agy update`, compares before/after versions, and never
claims currency from a read-only check. Upstream installers, package permissions and supported OS
versions remain upstream's responsibility; Loom never adds sudo or changes
native global permissions. Updates classify the resolved executable as npm, native, Homebrew or unknown.
Native installations use their own updater, Homebrew uses its formula/cask, and
unknown channels refuse. Repair discovers known missing launchers and verifies
the candidate before relinking/remembering it under the install policy.
Local/Node native updates snapshot the selected executable and launcher until
version verification succeeds. On Unix, local/Node npm install/update resolves the existing executable's prefix
first. With no existing installation it probes the global prefix and falls back
to an explicit per-command `--prefix ~/.local` when it is not writable. Local/Node
npm replacements are staged and version-verified, with rollback on failed
promotion. Existing unwritable installations fail without moving the agent.
No npm configuration file is modified. Installation, version inspection and ACP
launches prefer user-installed binaries. SSH probes the target's own prefix.
OpenCode's Unix catalog update uses the same npm path as its installation.

### 2.3 Event — one vocabulary for every runtime

```
turn_start · text_delta · reasoning_delta · tool_start · tool_delta · tool_end
approval_request · usage · error · turn_done{provenance, metrics}
```

Authenticated `POST /api/discussion/events` streams this vocabulary in the
existing SSE `choices[0].delta` envelope. An empty `id` follows the active native
journal; a bound local discussion follows that same journal. Other discussions
replay a stored snapshot, then receive live adapter events. Subscription and
reconnection never submit a turn. Slow readers reconnect from a fresh snapshot.

Native journal names map at the stream boundary; existing archives and classic
`/api/chat` remain compatible. Replay/reset, context and compaction controls sit
beside typed events. `turn_done` retains provenance and reported metrics. Native
tool output stays native; harness tools remain bounded metadata. Reasoning is shown only when reported by the runtime. Missing usage stays
unknown; ACP approval requests are native events, not inferred from tool cards.

`features/chat/engine.js` uses one reducer and one SSE subscription, without
session/state polling. Its route flag remains for existing composer/inspector
controls. Send/stop and legacy session endpoints remain available.

### 2.4 Provider — cloud APIs

The common discussion adapter uses Chat Completions today (`runtime/openai/`, with `openai_compat.go` preserving
the Loom adapter). A provider with a different
protocol (Anthropic Messages, Responses API) is a new `runtime/` adapter, not a
branch inside the existing one. Harness model-source routing in
`harness_providers.go` can already select protocol-compatible Responses or
Anthropic endpoints for native harness launch; that is separate from a common
cloud discussion adapter.

### Environment API (roadmap step 6)

`env_*.go` owns declared services and optional observations, independently of
discussion execution. All `/api/env/*` routes use the control-key middleware
(`Authorization: Bearer …` when configured), never the inference API key.
Responses are `no-store`. The Environment page exposes these services,
reachability matrices, Docker observations and Proxmox resources. Resources
are read on request; automatic service discovery remains future work.

| Route | Request and response |
| --- | --- |
| `GET /api/env/services` | `{ok, services: [{id,name,url,machine,kind,notes}]}` |
| `POST /api/env/services` | A service object; creates an ID when empty or replaces that ID. Returns `{ok,service}`. `machine` defaults to `local`, otherwise a saved SSH machine ID; `kind` is `web`, `api`, `db` or `other`. |
| `POST /api/env/services/delete` | `{id}` → `{ok}`; unknown ID returns 404. |
| `POST /api/env/docker` | `{machine,enabled}` explicitly enables/disables observation per machine (off by default). |
| `GET /api/env/docker?machine=local` | `{ok,machine,enabled,containers:[{name,image,state,status,ports}],error}`. Runs `docker ps -a --format '{{json .}}'` only when enabled; local execution uses argv, SSH uses Loom's key and existing PATH preamble. Missing Docker, permissions and unreachable machines return a per-machine error with HTTP 200. |
| `GET/POST /api/env/proxmox` | GET returns `{ok,config:{enabled,url,token_id,fingerprint}}`. POST accepts these config fields plus optional `token_secret` and `confirm_fingerprint`. An empty/omitted secret keeps the existing keychain entry. A new/changed nonempty fingerprint requires `confirm_fingerprint:true`. |
| `GET /api/env/proxmox/resources` | `{ok,enabled,resources:[{vmid,name,node,type,status,cpu,maxmem,mem,uptime}]}`; only `node`, `qemu` and `lxc` from `/api2/json/cluster/resources`. Unreported numeric fields are null. Provider errors return 502 (unavailable keychain: 503). |
| `POST /api/env/check` | `{target,from}` → `{from,ok,status,latency_ms,error}`. `from` defaults to `local`, otherwise a saved machine ID. `target` is an HTTP(S) URL without embedded credentials, or `host:port` (IPv6: `[host]:port`). |
| `GET /api/env/matrix` | `{ok,results:[{service_id,target,from,ok,status,latency_ms,error}]}` for every service from Loom and every saved machine, including unreachable machines. |

Services (at most 256) and provider settings are stored in Loom's database;
the Proxmox token secret is stored **only** in the OS keychain through the
existing keyring helpers, with a separate Environment identity. Failure to
save it is reported; there is no file or in-memory persistence fallback.
Proxmox requires an HTTPS origin and uses normal OS certificate verification
unless the user explicitly confirms a SHA-256 leaf certificate fingerprint
(hex, optionally colon-separated). Pinning checks that exact certificate and
its validity period on every connection; certificate changes are rejected.
The pin supplies identity verification in place of CA/hostname verification.
Obtain and verify the fingerprint independently before confirming it. Redirects
are never followed and provider error bodies are never returned.

Checks use a five-second timeout, HTTP GET without following redirects, or TCP
dial. HTTP 2xx/3xx is `ok`; other status codes are retained with an error.
Successful TCP checks have `status:0`. SSH HTTP checks use curl with
`--max-time 5`; when curl is absent, bash `/dev/tcp` tests only the socket,
also reported as `status:0`. Remote observation/checks require a POSIX shell
(and curl or bash for checks); Loom itself builds on Linux, macOS and Windows.
Matrices use eight workers and a shared eight-check concurrency limit per web
router, with a twenty-second overall deadline. Unfinished cells retain their
service/source and report cancellation/deadline errors. Reachability describes
the machine's network path, not an individual harness's sandbox permissions.

## 3. Data ownership

| Data | Owner | Store |
| --- | --- | --- |
| Discussions, turns, provenance, usage | Loom `discussion/` | bbolt |
| Projects, skills, providers (no keys) | Loom `resources/` | bbolt |
| MCP definitions | Loom | `LOOM_HOME/mcp.json`; linked read-only sources/bindings in bbolt (see [MCP files](mcp-files.md)) |
| Model configs, presets | Loom `engine/` | presets/*.env + bbolt |
| Cloud provider API keys | server memory; optional OS keychain or private encrypted server store (`provider_keyring.go`, `provider_secret.go`) | absent from Loom provider records and browser storage |
| Linked engine credentials | Loom | node record in the optionally encrypted Loom store; never returned to the browser |
| Native sessions, approvals, private memory | each harness | upstream |
| KV cache, slots, loaded instances | the engine | upstream |

## 4. Target Go layout and migration

The sequence below is a record of completed leaf extractions. It is not a
list of packages still to create; application state and orchestration remain
in Loom as described for each slice.

Much of the application still lives in package `internal/loom`. Splitting it in one pass would
break everything; do it **leaf-first, one package per PR, zero behavior change**:

1. **Registries in place** (still package `loom`): runtime registry +
   generic `/api/runtimes/{id}/…` routes; `Engine` interface wrapping the
   current llama.cpp functions; `ParamSpec` JSON + `/api/engine/params`.
2. `store/` (bbolt helpers, crypto, snapshots) — pure leaf.
3. `platform/` (`sys_*`) — depends on store only.
4. `engine/llamacpp/` (`backend_*`, `llm_oai_*`, `backend_router.go`).
5. `runtime/` + `runtime/{local,openai,antigravity,acp}/`.
6. `discussion/` (`workspace_*`, native conversation bridge) and unify events.
7. `tools/` (internet, MCP, memory) and `resources/`.
8. `web/` last — done: generic HTTP plumbing; application handlers stay in Loom.

Package-level globals (`conv`, bucket helpers, owned process state) move into
small structs passed explicitly; do not add new globals.

The third leaf slice now extracts a coherent, stateless subset into package
`internal/loom/engine/llamacpp`, with historical names preserved by one
`engine_compat.go` in `loom`. Existing immutable tables and embedded catalog move
with their implementation; no runtime globals are added. Standalone tests move
with the parsers/calculations, while integration tests remain in `loom`.

The fourth slice continues the same package: `backend_args.go` owns the
config-to-argv implementation, supplied with resolved binary/model paths, backend
port and accessors for runtime overrides, context, native flags, credentials and
auxiliary model resolution. `router.go` owns orchestration through a small
`Router` struct: binary path, backend port, INI path, models limit, state interface,
shared owner lock, auth/key accessors and last-error reporting. It adds no runtime
globals. Selection still precedes loading; variants never replace the active
selection; observation retains `autoload=false`. Existing fake-router/fake-help
integration tests remain in `loom`; package tests exercise argv precedence and
independent router owners with a socket-free fake transport.

The fifth slice replaces the scattered process/cache globals with one explicit
`llamacpp.Supervisor`, instantiated at the Loom compatibility boundary. It owns
the child handle, completion channel, generation guard, managed state, last error,
launch ports, native-help cache, GGUF metadata cache, two-second router `/props`
cache, slot observations/history/statistics and their separate locks. Router INI
and public model-switch locks also belong to that owner. It adds no runtime globals
in the engine package. Loom still resolves configured ports on each call; recorded
launch ports do not freeze or replace configuration reads.

`supervisor.go` owns start/wait, interrupt then kill after four seconds, atomic
legacy replacement and router readiness polling. Loom supplies command/environment
preparation under the owner lock, the diagnostic writer and asynchronous cleanup
callback for an unexpected exit. Requested stops retain the generation guard and
never invoke that cleanup. `observation_cache.go`, `model_cache.go` and `slots.go`
retain cache keys, lifetimes, copy behavior, slot parsing and completion accounting.
No launch resets a cache that was not reset before this slice. Independent-owner
and socket-free child tests cover lifecycle, cleanup and cache isolation; existing
Loom integration tests still exercise the historical compatibility functions.

Moved responsibilities:

- `backend_help.go`: native help parsing, flag metadata, tiers and config keys.
- `engine_params.go` and `params/llamacpp.json`: ParamSpec types, curated catalog
  and merge with supplied native flags.
- `backend_gguf.go`: bounded GGUF header reading and chat-template capabilities.
- `backend_vram_est.go`: KV/compute/MTP/weight calculations, fit search and JSON
  projection, using supplied model metadata/options.
- `backend_router.go`: resolved entry type, argv-to-options conversion using a
  supplied flag catalog, deterministic entry names and INI rendering.
- `backend_args.go`: launch argument assembly, defaults, fit gating, reasoning,
  vision/draft flags, extra arguments and final loopback binding, using explicit
  inputs/accessors.
- `router.go`: router argv, persisted-entry retention through a supplied state
  interface, INI publication, authenticated management HTTP, model load/unload,
  active/variant selection and observed context.
- `backend_config.go` and `backend_presets.go`: preset text parsing/formatting,
  quoted argument splitting and display names.

Still in `loom`, deliberately:

- `engine.go` and the remainder of `engine_params.go`: active configuration,
  lifecycle adapter and authenticated HTTP handler.
- `backend_serve.go`, `backend_llama_owned.go` and `backend_engine.go`: service
  startup/stop/restart policy, application signal handling, configured argv,
  command working directory/environment, readiness integration and service status.
  The unexpected-exit callback still clears Loom config, preset/router selection
  and API overlays; the engine supervisor does not know those application stores.
- The remainder of `backend_router.go` and `engine_compat.go`: current config,
  installed-binary/model/shard resolution, library/GPU environment setup, preset
  labels and adapters for credentials, runtime overlays and persisted state.
  The supervisor supplies the shared owner lock explicitly; no router
  implementation reads it as a package global.
- The remainders of `backend_help.go`, `backend_gguf.go` and
  `backend_vram_est.go`: installed-binary/library resolution,
  model path resolution, runtime overrides and hardware collection.
- The remainders of `backend_config.go`/`backend_presets.go`: active config and
  credentials, preserved machine-key policy, persistence, selection and CLI.
- `llm_oai_*.go` and `backend_server.go`: global overlays, chat/session coupling,
  proxy/authentication, observation adapters and web handlers. The config-key lookup
  delegates through compatibility instead of exposing the moved table.
- Other `backend_*` files (build/prebuilt/install, GPU discovery, catalog/Hub,
  model directories/downloads/shards/capabilities): configuration, installation
  and application integrations remain for a later coherent slice.

The sixth slice extracts only the runtime contracts and registry into
`internal/loom/runtime`: `RuntimeDescriptor`, `RuntimeTurn`, `RuntimeAdapter`,
optional `Connectable`/`QuotaReader`, capability checks and an ordered `Registry`
with register/upsert/remove/lookup/list operations and its own lock. Validation,
errors, insertion order, replacement/removal behavior and detached capability
snapshots are unchanged; empty catalog capabilities still serialize as `[]`.

`runtime_compat.go` supplies aliases and thin wrappers for every historical name
and unexported registry method, so ACP and other existing callers stay unchanged.
Turn/adapter/registry contracts use type parameters for the existing `Message`,
`Caps`, `ChatCallback` and `QuotaSnapshot` types: those remain in Loom, avoiding
an import cycle or prematurely moving discussion, tool and usage contracts.
There is no payload conversion or JSON tag change and no runtime package global.

Still in Loom: the registry instance, startup registration/panic policy, planned
descriptor adapter, concrete llama.cpp/OpenAI-compatible/Antigravity/ACP adapters,
per-turn configuration, session/context assembly, native account reads, quota
cache/throttle and authenticated HTTP actions. These depend on application state
and execution integrations rather than the registry contracts alone.

The seventh slice extracts the OpenAI-compatible cloud text protocol into
`internal/loom/runtime/openai`: endpoint/local-network validation, request JSON
and headers, bounded Chat Completions SSE parsing, token usage with field-presence
tracking, sanitized errors, cancellation, no-redirect enforcement and manual-price
cost arithmetic. `Adapter` receives provider configuration, credential lookup and
an HTTP client through `ProviderSource`, `CredentialSource` and `ClientSource`.
Loom supplies the same per-turn provider/key snapshot and optional client; there
are no package globals or implicit provider discovery/retries.

`openai_compat.go` preserves `cloudRuntimeAdapter`, its descriptor, historical
validation helpers and shared `RuntimeUsage` (including private presence flags).
Messages are marshaled directly without a schema conversion; streamed content and
usage map back to the existing `StreamEvent`, and the result remains one assistant
`Message`. Usage JSON tags, missing-versus-zero semantics, request budgets,
`usage_mode: "none"`, limits, timeouts and all error text remain unchanged.
Explicit `ToolAccess` optionally enables a bounded function-call loop; Loom
supplies only `web_search` when Internet access is enabled, and Bench supplies no
tools. Source selection and encrypted search keys remain application-owned.
Socket-free transport tests cover the extracted protocol and the Loom boundary;
existing cloud/session/provider/benchmark tests remain in Loom.

Still in Loom, deliberately:

- `workspace_cloud.go`: persistent provider records, because settings/credentials
  ownership remains with the application (no key in provider JSON).
- `openai_compat.go`, `workspace_runtime.go` and `workspace_sessions.go`: descriptor,
  registry startup, stored provider/key resolution, consent, prepared discussion
  context, history, replay and per-turn persistence. These depend on Loom state;
  the protocol receives resolved values only.
- `workspace_provider_probe.go` and `web_workspace_sessions.go`: explicit catalog
  probe and authenticated provider/session actions, including consent, credential
  validation/lookup and response shapes. Catalog discovery is a separate
  non-generation application action, outside this text-turn extraction.
- `workspace_usage.go`: retained-turn aggregation, price persistence/validation,
  account quota reads/cache, ACP-reported costs and HTTP actions. Only the pure
  manual-price arithmetic delegates through compatibility. The cloud protocol
  reported token usage but no provider charge before this slice; none is invented.
- `llm_bench_cloud.go`: model/key lookup, queue integration, timing and benchmark
  metric attribution; execution delegates through the historical adapter.

The eighth slice extracts the Antigravity text runtime into
`internal/loom/runtime/antigravity`: bounded CLI reads, installed-CLI checks,
ordered native model discovery, fresh-text subprocess execution, cancellation,
bounded stream parsing, native tool metadata redaction, reported token usage,
duration/session ID and read-only `/usage` + `/credits` parsing. `Adapter` receives
the selected model and optional executable explicitly; `ReadQuota` receives a
small read function. No mutable application state or runtime global is introduced.
Native authentication, permissions and private sessions remain with the CLI.

`antigravity_compat.go` preserves historical names, aliases the native wire and
tool-event types, marshals the original messages without conversion, maps events
to `StreamEvent` and builds the existing assistant `Message` and `QuotaSnapshot`.
JSON tags, null-versus-zero values, empty window arrays, event order, argv,
timeouts, limits and error text remain unchanged. Shared `RuntimeUsage` keeps
its historical private presence flags; native usage does not gain cloud flags.
Focused leaf tests cover subprocess stdin/cancellation, malformed/inconsistent
streams, redaction, usage replacement by step, catalog order/bounds and nullable
quotas with explicit read commands. Loom tests retain consent/integration coverage
and check compatibility JSON/event order.

The same slice extracts the llama.cpp discussion `RuntimeAdapter` into
`internal/loom/runtime/local`, separate from the inference engine. Its generic
`Adapter` implements the existing runtime contract and receives the original
`runChat` pipeline through a `Runner` function. `local_compat.go` preserves the
zero-value `llamaRuntimeAdapter` and injects the same runner on every turn.
Messages, temperature, capabilities, context, callback, partial result and error
pass through unchanged; `MaxTokens` remains unused as before. Focused tests verify
these inputs/results and detached descriptor capabilities.

Still in Loom, deliberately:

- `workspace_antigravity.go`: native catalog persistence, explicit connect
  consent, vault checks and application persistence errors; these require Loom's
  store and security state. The full descriptor remains at the compatibility
  boundary because its connect/quota capabilities include those application actions.
- `workspace_runtime.go`, `workspace_catalog.go`, `workspace_sessions.go` and
  `web_runtimes.go`: registry ownership/startup, model readiness/selection,
  transcript-sharing consent, prepared discussion inputs and authenticated HTTP
  actions. Leaf protocols receive only already-resolved turn inputs.
- `workspace_usage.go` and the quota wrapper: shared account snapshot types,
  account identity/source, cache/throttle, retained discussion usage, price
  persistence and HTTP actions. The Antigravity parser returns observations only;
  Codex quota execution remains unchanged for the ACP slice.
- `llm_client.go` (`runChat`), `workspace_native.go` and the conversation pipeline:
  tools, reasoning, sampling/configuration reads, reactive compaction, retries,
  archive binding and persistence depend on Loom's application state. Moving
  them with the small local adapter would prematurely extract discussion/tools
  and risk changing the existing native pipeline. Local engine lifecycle and
  supervision remain at their established engine/application boundaries.

The ninth slice extracts the ACP protocol layer into
`internal/loom/runtime/acp`, with historical names delegated through
`acp_compat.go`:

- `protocol.go` and per-OS process helpers: bounded NDJSON JSON-RPC frames,
  bidirectional calls/notifications, ordered update reading, sanitized errors,
  cancellation and termination of the owned process group. `NewClient` receives
  the already-resolved `exec.Cmd`; Loom keeps native launcher/PATH/environment
  preparation. The local reply-completion callback is excluded from wire JSON.
- `session.go` and `events.go`: unchanged session-response JSON, legacy model
  option projection, nested select/boolean validation, session/update decoding,
  live event projection and tool-call merging/clipping limits. Tool maps are
  supplied explicitly; live filtering and state application remain in Loom.
- `diffs.go`: bounded line LCS, addition/deletion counts and unified diff text,
  independent of file access or persisted baselines.
- `replay.go`: a self-contained replay builder owns only its buffer, lock and
  tool map. Generic message/turn/event constructors preserve the original Loom
  types without importing application state or converting their JSON. Replay
  keeps its historical event mapping (including tool_delta for an unfinished
  initial tool call), command payloads and trailing-update flush behavior.
- `agy.go`: the Antigravity ACP bridge, its private agent sessions, modes,
  native stream translation and existing file-observation/diff behavior. Loom
  injects the `agyRead` function and `agy` executable. No bridge operation reads
  Loom's session manager, store, account configuration or runtime registry.

The independent Antigravity bridge and replay tests move with their code.
Socket-free leaf tests cover bidirectional framing while a request handler waits,
ordered notifications, cancellation, malformed frames/results, error sanitization,
wire/null/empty shapes, detached tool snapshots, legacy model options and bounded
line diffs. Loom retains fake-agent lifecycle/permission integration and
file-confinement tests; a compatibility test verifies original discussion JSON.

Still in Loom, deliberately:

- `acp_session.go`, `acp_permissions.go`, `acp_shutdown.go` and
  `acp_registry.go`: live bindings, consent, approvals, managed session lifecycle,
  context hashes, configuration application and runtime registration require
  `workspaceSessions`, prepared discussions and application security state.
- `acp_files.go`, per-OS `acpOpenRead`, and the remainder of `acp_events.go`:
  authorized roots/symlink checks, bounded filesystem access, persisted baselines,
  changed-file records and publication belong to the discussion. Pure protocol
  projection cannot grant file access or persist agent-reported diffs.
- `acp_import.go`, `acp_probe.go`, `acp_custom.go` and `acp_http.go`: native
  listing/import orchestration, duplicate detection, store writes, probes/cache,
  launcher resolution, custom agent records and authenticated HTTP actions depend
  on Loom state. Session import now delegates only replay construction.
- `acp_fake.go` remains the existing Loom fixture agent used by lifecycle tests.
  Codex app-server quota execution and shared usage accounting remain unchanged.

The tenth slice extracts the portable discussion model and pure logic into
`internal/loom/discussion`, with historical names and signatures preserved by
`discussion_compat.go`:

- `model.go`: `Message` and tool-call wire metadata, `RuntimeSession`,
  `RuntimeTurnRecord`, `DiscussionEvent`, the inert persisted `ACPState` and
  changed-file records, plus the existing snapshot-copy behavior. Session/turn
  types accept Loom's usage and timing types through generic aliases, retaining
  private usage-presence flags without converting JSON. ACP state remains
  runtime-private display metadata; moving its schema does not make it portable.
- `prompt.go`: context/preview read models, portable message preparation and
  limits, and the identical ordered JSON/SHA-256 context revision. Loom supplies
  the assembled context explicitly. Preparation still rejects non-text/tool
  history, omits empty answers and never truncates or mutates stored history.
- `turn.go`: the historical 70-rune automatic title, portable prefix comparison
  and construction of resolved turn provenance. Clock reads and live state
  application stay at the application boundary.
- `events.go`: native/runtime event projection, display metrics and snapshot
  replay. Loom injects context reads at the same two replay boundaries; event
  order, provenance, running markers and unknown historical metrics are unchanged.
- `usage.go`: retained-turn token aggregation, last-turn usage fallback,
  cumulative native-session cost attribution, optional manual-price estimates
  and summary ordering. Loom supplies catalog/price rows, session snapshots,
  runtime classification, choice IDs and the historical token/cost arithmetic.
  The leaf introduces no store, registry, mutable global or account reads.

Independent prompt-limit and event-projection tests move with their code.
Leaf tests also cover ordered revision compatibility, snapshot isolation and
empty/null shapes, rune titles, turn provenance, unknown/zero/negative usage,
last-turn fallback, cumulative costs and replay metrics. Loom compatibility tests
freeze the persisted wire shape and check private usage flags; existing consent,
stale-context, cloud/native/ACP and HTTP integration tests remain in Loom.

Still in Loom, deliberately:

- `workspace_sessions.go`, `workspace_routing.go` and `workspace_runtime.go`:
  `workspaceSessions`, locks, storage/vault checks, idempotency, selection,
  consent, adapter resolution and generation lifecycle need application state.
  Live turn updates and publication retain their cancellation/persistence order.
- `workspace_prompt.go`, `workspace_context.go`, project files, Brain and skill
  helpers: project/skill lookup, selected-file reads and Brain retrieval assemble
  the context from read snapshots outside the global session registry lock.
  The registry revalidates the original session before committing execution.
  Pure preparation and hashing receive
  only the resulting read model; there is no parallel memory engine.
- `workspace_native.go` and the existing Conversation/archive pipeline: native
  activation, journal projection/annotation, tools, compaction, archive binding
  and persistence use the native archive/Conversation types and ownership. Only
  portable prefix comparison delegates in this slice. Native archive title
  derivation retains its separate historical 80-rune/ellipsis policy.
- `openai_compat.go`, `llm_client.go` and `workspace_usage.go`: shared
  `RuntimeUsage` retains private field-presence flags; native `StatsEvent`
  remains the inference timing schema. Provider prices/catalog persistence,
  native quota execution/cache and account identity remain application-owned.
  Their inert discussion containers and pure aggregation use aliases/wrappers.
- ACP bindings, permissions, file confinement/baselines, resource assembly,
  subscription fan-out and all HTTP handlers stay in Loom because they require
  live runtime, security, filesystem or transport state.

The eleventh slice extracts the native tool and shared resource logic into
`internal/loom/tools` and `internal/loom/resources`, with historical names and
wire types delegated through `tools_compat.go` and `resources_compat.go`.
Neither leaf imports `loom` or owns mutable application globals.

Moved into `tools`, deliberately:

- `model.go` and `memory.go`: unchanged tool schemas and memory modes, page-name
  validation, page listing/ranking/snippets, bounded line reads and exact edits,
  add/save/delete operations and output truncation. `MemoryStore` supplies the
  resolved directory and the original encrypted page reads/writes/locked error;
  Loom's vault and verified writes remain the only storage implementation.
- `internet.go`, `fetch.go` and `web_tools.go`: Crawl4AI request/response parsing,
  DuckDuckGo search, served-HTML extraction/conversion, URL/line normalization,
  cache-key formatting, tool schemas and open/read/grep results. Small source
  interfaces supply current engine/key/URL, cached pages and HTTP clients.
  Cookies, headers, budgets, DOM limitations and French tool output are unchanged.
- `mcp.go` and `mcp_pool.go`: SDK transports, header injection, schema/content
  projection and an explicit `MCPManager` with parallel connection deduplication,
  namespaced tools, disabled-tool filtering, one reconnect, status and prompt
  projection. Loom supplies the configuration loader, resolved subprocess,
  version, connector and HTTP client; the manager owns only its sessions/lock.

Moved into `resources`, deliberately:

- `mcp.go`, `mcp_file.go` and `mcp_sources.go`: original server/source/status JSON,
  default-enabled decoding, validation, bounded linked-file reads, project-scope
  selectors, sanitized errors, credential placeholders/adoption and the MCP file
  codec preserving unknown extension fields.
- `skills.go` and `sinks.go`: `Capability`/source/target wire types, front-matter
  parsing/formatting, slugs, folder reads/scans/writes, owned-link detection and
  manifest-driven skill distribution/cleanup. `Library` receives its root and
  ID generator; sink synchronization receives skills, bindings, manifests and
  path display explicitly. Foreign folders and linked sources keep their policy.
- `bindings.go`: historical default/all-versus-empty selections, skill binding
  lookup and persistence through `JSONStore`; native MCP-selection validation is
  supplied by Loom, in the same read/validate/write order.

Independent ranking, front-matter, adoption and MCP deduplication tests move with
these implementations. Leaf tests cover supplied HTTP/protocol requests,
Markdown shapes, page reads/grep, memory storage/locked behavior, unknown MCP
fields, skill binding/cleanup and in-memory MCP discovery/calls/reconnection.
Loom retains storage/vault/migration/HTTP/native integration tests and freezes
compatibility JSON, including empty/null/omitted fields.

Still in Loom, deliberately:

- Internet configuration, keys, enablement, reachability/page caches, the HTTP
  client/cookie jar and CLI: their globals, settings and native lifecycle are
  application-owned. Pure protocols receive those resolved inputs.
- MCP configuration locks, authoritative file cache, revision checks, legacy
  migration/backup marker, atomic publication and invalidation policy; linked
  source registration/adoption/collision writes and native path suggestions:
  these coordinate Loom storage and the one live pool. Only the codecs/readers
  move. MCP prewarm/shutdown and command hiding/environment preparation also
  remain native application policy.
- Skill migration (`sync.Once` and legacy buckets), source/sink registration,
  manifests/opt-in writes, global locks and capability/project CRUD: these need
  Loom's store, shared project context and security policy. Resource bindings
  retain runtime lookup and native selection checks at the compatibility boundary.
- All HTTP handlers, native tool-loop registration/dispatch, shell/file tools,
  workspace resolution, vault/encryption and Brain integration: these require
  authenticated requests, cancellation, application state or native permissions.
  No parallel memory engine or public workflow/API change is introduced.

The twelfth and final leaf slice extracts generic HTTP plumbing into
`internal/loom/web`, with historical names delegated by `web_compat.go`.
Roadmap step 1 is done: every planned leaf package now exists. This is a package
boundary extraction, not removal of all application orchestration or globals.

Moved into `web`:

- `http.go`: JSON response encoding, the no-store method guard and strict
  application/json decoding with the existing 128 KiB limit, unknown-field and
  trailing-value rejection, status codes and French error bodies.
- `auth.go` and `routes.go`: SHA-256 control-key hashing, constant-time Bearer
  validation, unforgeable E2E request context, fail-closed middleware and the
  protected API registrar. Loom supplies the key reader and node-aware wrapper;
  authentication stays outermost and rereads the key for every ordinary request.
  Standard cross-origin protection remains limited to the Brain MCP transport.
- `assets.go`: root and `/next` UI routing, module MIME types, SPA fallback,
  public PWA/brand/font routes, redirects, GET/HEAD guards and cache policies.
  `Assets` receives filesystems and icon bytes explicitly. The obsolete mutable
  MIME table is replaced by a fixed lookup; the leaf has no mutable globals.
- `sse.go` and `websocket.go`: SSE headers, serialized data frames and the
  existing four-second heartbeat, plus WebSocket acceptance with the library's
  default same-origin check and an explicit route read limit.

Socket-free leaf tests freeze JSON/error/status/header behavior, decoding limits,
key rotation and E2E bypass, middleware order, origin rejection, all public asset
routes and cache policies, supplied-filesystem isolation, SSE framing and heartbeat.
Existing Loom tests retain persistence, PWA manifest and discussion integration
coverage. No route, JSON shape, public workflow or UI file changes.

Still in Loom, deliberately:

- `web_compat.go`: `go:embed` declarations must remain beside `ui/` (Go embedding
  cannot reference a parent directory); brand PNG generation and its existing
  lazy cache remain at that boundary. Serving delegates to `web.Assets`.
- `web_auth.go` and `web_network.go`: key persistence/migration, CLI commands,
  configured exposure and listener state require Loom's store and service policy.
- `web_server.go`: startup, conversation loading, migrations, MCP prewarm,
  background jobs, lifecycle/listener ownership and the domain route list remain
  application orchestration. Only registration plumbing moves.
- Domain HTTP handlers, `nodeAware`, engine/API proxies, tunnel/E2E crypto,
  Brain transport construction and terminal handlers: these resolve application
  configuration, sessions, consent, vault state, tickets, owned processes or
  remote machines. WebSocket frame/PTY coupling and SSE subscriptions/payloads
  therefore stay with their application owner, using generic transport helpers.
- Earlier slices' active stores, registry, conversation/session owners, process
  supervisor, caches and locks remain live application state. No unused runtime
  global is removed speculatively; service/environment resolution and application
  cleanup remain in Loom for future coherent changes.

## 5. Front-end layout (`internal/loom/ui/next`)

```
index.html            import map (preact, hooks, htm vendored in vendor/)
css/app.css           tokens + base + kit     css/chat.css   css/pages.css
js/core/              lib (Preact/htm, store, format helpers) · api · state
js/ui/                kit: icons, dialog/toast, controls (Seg, Tabs, Switch, Slider, Tip, Popover, Menu, Empty)
js/app/               main, shell (sidebar), routes.js (registry), placeholder
js/features/<name>/   one folder per domain: chat, inspector, local, cloud,
                      harnesses, bench, resources, usage, settings, projects, environment, terminals, onboarding
logos/                provider/harness logos (lobehub, MIT), used by ui/logo.js
```

Rules: features import `core/` and `ui/`, never each other's internals except
through an explicit export (e.g. `chat/picker.js` exports `groupVariants`). New
page = new `features/x/page.js` + one entry in `app/routes.js`. No build step.

## 6. How to add…

- **A harness**: `runtime/<id>/` implementing `RuntimeAdapter` (+ optional
  interfaces) is the target layout. Today registration is owned by
  `workspace_runtime.go` through `registerRuntime`; the leaf registry has no
  global `runtime.Register` function. Existing ACP launchers are declared in
  `internal/loom/harness/acp_agents.json`. Test with a fake CLI; the Harnesses
  page, picker and Usage read descriptors.
- **An engine**: `engine/<id>/` implementing `Engine`; register; declare
  capabilities; provide `ParamSpec` JSON is the target. Today `engine.go` exposes
  the llama.cpp wrapper; vLLM uses separate lifecycle handlers and an existing
  Settings › Engines UI. A generic multi-engine registry remains work.
- **A llama.cpp parameter in Advanced**: one entry in the curated ParamSpec JSON.
- **A page**: `features/<x>/page.js` + one line in `app/routes.js`.
- **A cloud protocol**: new runtime adapter (see 2.4).
- **A known cloud provider** (OpenAI-compatible): one line in `ui/next/js/features/cloud/catalog.js`
  (+ its logo in `ui/next/logos/` and `ui/logo.js`). Plain HTTP is accepted only for
  loopback / private / Tailscale addresses and local host names (`localNetworkHost`).

## 7. Known debt (ordered)

1. Unified SSE events are implemented (2.3); durable native archives and portable text snapshots still have separate storage formats.
2. In-package Engine/ParamSpec and runtime metadata are implemented; the engine leaf subset is extracted; further execution-boundary extraction and a generic engine registry remain
   future work. vLLM and linked inference servers are already implemented.
3. Real-GPU acceptance of native `--fit` and observed context remains separate
   from the implemented automatic defaults and fixture tests (§2.1).
4. Real-platform acceptance of optional cloud-provider OS keychain storage.

The classic UI (`ui/src`, `/classic`, `tools/assemble-ui`) is already removed.
Its features live in `ui/next` (multi-GPU split through Expert parameters);
the release build embeds these files directly without an assembler.

Application release updates are separate from engine updates. `sys_update.go` shares verified binary installation between the CLI and authenticated interface; `sys_update_linux.go` confines system-install updates to a root-owned fixed target and helper, without user configuration or arbitrary command arguments. The interface reports installation separately from confirmed restart. See [Updating Loom](updates.md).

## Engine-only execution mode

`loom node` enters before dotenv/user adoption/ACP lifecycle setup and uses a separate data root. `engine_routes.go` registers the shared engine control handlers in either the main authenticated web mux or the node-only mux. The latter never calls `newWebMux`, so it does not load conversations, prewarm MCP or start Brain/harness services. `engine_worker.go` owns the machine-only auth boundary and streaming local inference proxy; `engine_worker_routes.go` adapts native management while a loopback vLLM link serves inference; `engine_worker_install.go` writes a user service. The release installer has a separate user-only node mode. Node release updates are proxied through dedicated main-side endpoints and restart only the node user service. Local-only node benchmarks reuse native handlers and reject external choices. Main benchmark orchestration remains on the control plane and dispatches only local rows to a linked node. The main `engine_node.go` client negotiates a same-origin `/v1` address while retaining older full-Loom links. See [Engine node](engine-node.md) for capability and platform limits.


### Reviewed project continuity leaf (unreleased)

`project/continuity.go` retains bounded validation for compatibility with stored
project core/working-state/reference values. It imports only the standard
library; Loom's project storage, automatic conversation scope, Brain retrieval
and runtime orchestration stay in their adapters. New project setup does not
require this legacy metadata. Native import provenance belongs to
`discussion.NativeImport`; Brain's `PathPrefixes` scope is enforced by lexical,
semantic and read/pack paths.
See [workspace continuity](workspace-architecture.md#continuity-consolidation-unreleased).
