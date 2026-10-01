# Loom architecture

Target architecture and migration plan. Read with `architecture-principles.md`
(the rules) and `workspace-architecture.md` (discussion/runtime contracts).
The goal: Loom stays easy to extend at 50 000+ lines. Adding a harness, an
engine, a cloud provider or a page must touch **one new folder plus one registry
entry**, never the core.

## 1. What Loom is made of

```
                       ┌──────────────── UI (ui/next) ────────────────┐
                       │ app/ shell + routes · features/* · ui/ kit   │
                       └───────────────┬──────────────────────────────┘
                                       │ HTTP /api/* (+ SSE)
┌──────────────────────────── Go server (loom web) ─────────────────────────┐
│ web/        handlers, one file per domain, thin (decode → call → encode)  │
│ discussion/ Loom-owned conversations, routing, shared context, usage      │
│ runtime/    RuntimeAdapter registry: local · openai-compatible · harness  │
│ engine/     Engine registry: llamacpp (router mode) · later vllm          │
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
for transient loading. They never overwrite saved model settings. Service
supervision remains in `loom`; router execution delegates through compatibility
wrappers to an explicit `llamacpp.Router`.
Contexts are checked before lifecycle actions; cancellation of an already-started
legacy service/router job is not provided by this wrapper.

The contract above is schematic: the current caps expose router, presets, slots
and estimate; status exposes active/ready/model; `Estimate` retains the existing
VRAM estimate type. This is a migration boundary, not a delivered second-engine
registry or complete execution package split. A later engine (vLLM) implements the interface in its
own folder.

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

The ordered registry and optional connect/quota action dispatch are implemented
in package `loom` (`workspace_runtime.go`, `web_runtimes.go`). Planned descriptors
stay capability-less; existing execution/stream contracts remain unchanged. The
following is the target after later migration steps, not the current interface:

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

- **Registry**: current in-package `registerRuntime(adapter)` at init, later
  `runtime.Register(adapter)` after the package split; `runtimeCatalog()` and
  `/api/workspace` read the registry. No `switch runtimeID` anywhere else.
- **Generic HTTP**: `POST /api/runtimes/{id}/connect`, `GET/POST
  /api/runtimes/{id}/quota`, `POST /api/runtimes/{id}/approval`. The current
  per-harness routes (`/api/workspace/antigravity/connect`,
  `/api/workspace/codex/connect`) become aliases, then are removed.
- **Descriptor carries UI copy** (description, CLI name, consent text) so the
  Harnesses page renders any adapter without a hard-coded `INFO` map.
- **Capabilities are honest**: a capability is declared only when implemented
  and tested (`chat`, `stream`, `cancel`, `usage`, `native-events`,
  `reasoning-summary`, `approvals`, `skills`, `mcp`, `resume`).

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
tool output stays native; harness tools remain bounded metadata. Codex reasoning
is a reported summary. Missing usage stays unknown; no approval stream is invented.

`features/chat/engine.js` uses one reducer and one SSE subscription, without
session/state polling. Its route flag remains for existing composer/inspector
controls. Send/stop and legacy session endpoints remain available.

### 2.4 Provider — cloud APIs

Chat Completions today (`workspace_cloud.go`). A provider with a different
protocol (Anthropic Messages, Responses API) is a new `runtime/` adapter, not a
branch inside the existing one.

## 3. Data ownership

| Data | Owner | Store |
| --- | --- | --- |
| Discussions, turns, provenance, usage | Loom `discussion/` | bbolt |
| Projects, skills, MCP definitions, providers (no keys) | Loom `resources/` | bbolt |
| Model configs, presets | Loom `engine/` | presets/*.env + bbolt |
| API keys | memory only (today) → OS keychain (later) | never on disk in clear |
| Native sessions, approvals, private memory | each harness | upstream |
| KV cache, slots, loaded instances | the engine | upstream |

## 4. Target Go layout and migration

`internal/loom` is one 190-file package today. Splitting it in one pass would
break everything; do it **leaf-first, one package per PR, zero behavior change**:

1. **Registries in place** (still package `loom`): runtime registry +
   generic `/api/runtimes/{id}/…` routes; `Engine` interface wrapping the
   current llama.cpp functions; `ParamSpec` JSON + `/api/engine/params`.
2. `store/` (bbolt helpers, crypto, snapshots) — pure leaf.
3. `platform/` (`sys_*`) — depends on store only.
4. `engine/llamacpp/` (`backend_*`, `llm_oai_*`, `backend_router.go`).
5. `runtime/` + `runtime/{local,openai,antigravity,codex}/`.
6. `discussion/` (`workspace_*`, native conversation bridge) and unify events.
7. `tools/` (internet, MCP, memory) and `resources/`.
8. `web/` last: handlers become thin files per domain.

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
- `backend_serve.go`, `backend_llama_owned.go` and `backend_engine.go`: child
  process startup/supervision, signals, readiness tied to the owned process,
  service restart/stop, preflight and service status. These remain coupled to
  Loom's service layer and application cleanup (config, overlays and selection).
- The remainder of `backend_router.go` and `engine_compat.go`: current config,
  installed-binary/model/shard resolution, library/GPU environment setup, preset
  labels and adapters for credentials, runtime overlays and persisted state.
  The existing shared owner lock stays in `loom` and is passed explicitly; no
  router implementation reads it as a package global.
- The remainders of `backend_help.go`, `backend_gguf.go` and
  `backend_vram_est.go`: existing caches, installed-binary/library resolution,
  model path resolution, runtime overrides and hardware collection.
- The remainders of `backend_config.go`/`backend_presets.go`: active config and
  credentials, preserved machine-key policy, persistence, selection and CLI.
- `llm_oai_*.go` and `backend_server.go`: global overlays, chat/session coupling,
  proxy/authentication, server watches and web handlers. The config-key lookup
  delegates through compatibility instead of exposing the moved table.
- Other `backend_*` files (build/prebuilt/install, GPU discovery, catalog/Hub,
  model directories/downloads/shards/capabilities): configuration, installation
  and application integrations remain for a later coherent slice.

Next engine slice: introduce an explicit owned-process/supervisor boundary and
cache owner, with service/environment callbacks from `loom`, before extracting
process execution that can move cleanly. Keep service policy and application
cleanup in `loom`. The runtime package split follows that boundary; the current
extraction does not restart an engine, alter flags or change stored configuration.

## 5. Front-end layout (`internal/loom/ui/next`)

```
index.html            import map (preact, hooks, htm vendored in vendor/)
css/app.css           tokens + base + kit     css/chat.css   css/pages.css
js/core/              lib (Preact/htm, store, format helpers) · api · state
js/ui/                kit: icons, dialog/toast, controls (Seg, Tabs, Switch, Slider, Tip, Popover, Menu, Empty)
js/app/               main, shell (sidebar), routes.js (registry), placeholder
js/features/<name>/   one folder per domain: chat, inspector, local, cloud,
                      harnesses, bench, resources, usage, settings, projects
logos/                provider/harness logos (lobehub, MIT), used by ui/logo.js
```

Rules: features import `core/` and `ui/`, never each other's internals except
through an explicit export (e.g. `chat/picker.js` exports `groupVariants`). New
page = new `features/x/page.js` + one entry in `app/routes.js`. No build step.

## 6. How to add…

- **A harness**: `runtime/<id>/` implementing `RuntimeAdapter` (+ optional
  interfaces), `runtime.Register` in its `init`, tests with a fake CLI. The UI
  needs nothing: the Harnesses page, picker and Usage read the descriptor.
- **An engine**: `engine/<id>/` implementing `Engine`; register; declare
  capabilities; provide `ParamSpec` JSON. The Local page adapts.
- **A llama.cpp parameter in Advanced**: one entry in the curated ParamSpec JSON.
- **A page**: `features/<x>/page.js` + one line in `app/routes.js`.
- **A cloud protocol**: new runtime adapter (see 2.4).
- **A known cloud provider** (OpenAI-compatible): one line in `ui/next/js/features/cloud/catalog.js`
  (+ its logo in `ui/next/logos/` and `ui/logo.js`). Plain HTTP is accepted only for
  loopback / private / Tailscale addresses and local host names (`localNetworkHost`).

## 7. Known debt (ordered)

1. Unified SSE events are implemented (2.3); durable native archives and portable text snapshots still have separate storage formats.
2. In-package Engine/ParamSpec and runtime metadata are implemented; the engine leaf subset is extracted; execution boundaries and a second engine remain future work.
3. Done: the old UI (`ui/src`, `/classic`, `tools/assemble-ui`) is removed; its
   features live in `ui/next` (multi-GPU split through Expert parameters).
4. Auto config forces native context and `-ngl 999` → let `--fit` decide unset
   values and show the fitted result.
5. API keys memory-only → OS keychain with explicit consent.
