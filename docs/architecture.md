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
│ runtime/    RuntimeAdapter registry: local · openai-compatible             │
│ engine/     llama.cpp router helpers; vLLM lifecycle stays in loom       │
│ resources/  projects, skills, MCP definitions (shared across runtimes)    │
│ tools/      internet, MCP client, memory tools for the local tool loop    │
│ store/      bbolt + encryption + snapshots                                │
│ platform/   service/install/tray/paths/update/network per OS              │
└───────────────────────────────────────────────────────────────────────────┘
        │                         │                          │
   llama-server (router)   cloud Chat Completions
```

Ownership rule: **Loom owns intent, persistence and the shared context;
engines and providers own inference.**

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

### 2.2 RuntimeAdapter — discussion execution

`internal/loom/runtime` owns the generic execution contract. Startup registers
only `llama.cpp` (local) and `openai-compatible` (cloud). Adapters translate
requests and events; engines/providers own inference. Native account connection
and quota interfaces have been removed. Legacy display event/state types remain
for reading saved discussions only.

There are no coding agent protocol packages, native installers or registration
HTTP APIs. See [retirement](harness-retirement.md).

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
tool output stays bounded; historical native tools remain display metadata. Reasoning is shown only when reported by the runtime. Missing usage stays
unknown; Generic approval requests are explicit events, not inferred from tool cards.

`features/chat/engine.js` uses one reducer and one SSE subscription, without
session/state polling. Its route flag remains for existing composer/inspector
controls. Send/stop and legacy session endpoints remain available.

### 2.4 Provider — cloud APIs

The common discussion adapter uses Chat Completions today (`runtime/openai/`, with `openai_compat.go` preserving
the Loom adapter). A provider with a different
protocol (Anthropic Messages, Responses API) is a new `runtime/` adapter, not a
branch inside the existing one. Harness model-source routing in
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
the machine's network path, not a model tool’s permission boundary.

## 3. Data ownership

| Data | Owner | Store |
| --- | --- | --- |
| Discussions, turns, provenance, usage | Loom `discussion/` | bbolt |
| Projects, skills, providers (no keys) | Loom `resources/` | bbolt |
| MCP definitions | Loom | `LOOM_HOME/mcp.json`; linked read-only sources/bindings in bbolt (see [MCP files](mcp-files.md)) |
| Model configs, presets | Loom `engine/` | presets/*.env + bbolt |
| Cloud provider API keys | server memory; optional OS keychain or private encrypted server store (`provider_keyring.go`, `provider_secret.go`) | absent from Loom provider records and browser storage |
| Linked engine credentials | Loom | node record in the optionally encrypted Loom store; never returned to the browser |
| KV cache, slots, loaded instances | the engine | upstream |

## 4. Package ownership

The composition root is `internal/loom`; discussion, runtime, Brain, engine,
provider, resource, policy, terminal, storage and platform packages hold focused
contracts and implementations. Runtime execution leaves are `runtime/local`
and `runtime/openai`. The node reuses engine/terminal/voice/observation handlers
without initializing the full UI, Brain or provider accounts.

Historical migration and code audits are dated evidence, not current contracts.
Keep generic execution and historical readers separate; no old runtime ID may
launch software. Test both model paths and retained data with free fixtures.

## 5. Front-end layout (`internal/loom/ui/next`)

```
index.html            import map (preact, hooks, htm vendored in vendor/)
css/app.css           tokens + base + kit     css/chat.css   css/pages.css
js/core/              lib (Preact/htm, store, format helpers) · api · state
js/ui/                kit: icons, dialog/toast, controls (Seg, Tabs, Switch, Slider, Tip, Popover, Menu, Empty)
js/app/               main, shell (sidebar), routes.js (registry), placeholder
js/features/<name>/   one folder per domain: chat, inspector, local, cloud,
                      bench, resources, usage, settings, projects, environment, terminals, onboarding
logos/                provider logos (lobehub, MIT), used by ui/logo.js
```

Rules: features import `core/` and `ui/`, never each other's internals except
through an explicit export (e.g. `chat/picker.js` exports `groupVariants`). New
page = new `features/x/page.js` + one entry in `app/routes.js`. No build step.

## 6. Extending Loom

Add model/provider/engine capabilities in the existing ownership package. Wire
through the shared Local/Cloud discussion path and registry, not a parallel
conversation UI. Use observed upstream capabilities, explicit credentials,
consent and existing policy authorization. Update tests and focused docs.

## Engine-only execution mode

`loom node` enters before dotenv/user adoption and uses a separate data root. `engine_routes.go` registers the shared engine control handlers in either the main authenticated web mux or the node-only mux. The latter never calls `newWebMux`, so it does not load conversations, prewarm MCP or start Brain services. `engine_worker.go` owns the machine-only auth boundary and streaming local inference proxy; `engine_worker_routes.go` adapts native management while a loopback vLLM link serves inference; `engine_worker_install.go` writes a user service. The release installer has a separate user-only node mode. Node release updates are proxied through dedicated main-side endpoints and restart only the node user service. Local-only node benchmarks reuse native handlers and reject external choices. Main benchmark orchestration remains on the control plane and dispatches only local rows to a linked node. The main `engine_node.go` client negotiates a same-origin `/v1` address while retaining older full-Loom links. See [Engine node](engine-node.md) for capability and platform limits.


### Reviewed project continuity leaf (unreleased)

`project/continuity.go` retains bounded validation for compatibility with stored
project core/working-state/reference values. It imports only the standard
library; Loom's project storage, automatic conversation scope, Brain retrieval
and runtime orchestration stay in their adapters. New project setup does not
require this legacy metadata. Native import provenance belongs to
`discussion.NativeImport`; Brain's `PathPrefixes` scope is enforced by lexical,
semantic and read/pack paths.
See [workspace continuity](workspace-architecture.md#continuity-consolidation-unreleased).
