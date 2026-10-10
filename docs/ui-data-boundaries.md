# UI data boundaries and mobile recovery

An unavailable engine must leave the discussion UI usable. The UI retains the
last valid shared observation and records that its source is unavailable. A
valid empty list is a successful observation; a JSON error object is not an
empty list. No recovery action automatically submits a message or starts an
engine.

## Confirmed failure

The original `get()` parsed error bodies without rejecting HTTP failure.
`refreshLibrary()` published those bodies directly to `app.models` and
`app.presets`. A remote engine proxy's HTTP 502 response
`{ok:false,error:"remote engine unreachable"}` therefore replaced an array
with an object. Picker's `.filter()` threw during rendering, including while
the selector was closed. PageGuard hid the failed render but did not repair
the store. Palette, Local, Hub and Bench shared the same poisoned catalog.

## Boundary contract

`core/shape.js` supplies one container validator and explicit endpoint
contracts, reused by `core/api.js`, `createStore`, shared observations and
stream consumers. Known nested lists are checked before publication. Optional
Go slices may be null where existing consumers already use an empty list;
required envelopes and top-level catalogs must have their expected shape.
Opaque tool payloads stay opaque. Only the flat elicitation form fields that
the UI renders are checked within a request's JSON schema.

HTTP errors, malformed JSON, network failure and invalid read shapes reject
the observation. POST retains the existing error-result contract for actions.
Background GET and discussion stream reads do not prompt for authentication
or reload the page on 401. Explicit actions retain the authentication flow.
Negative records inside a successful response, such as an environment check
with `ok:false`, remain legitimate observations.

Every `refresh*` in `core/state.js` uses this boundary. Models and presets,
status and server info, and local history and runtime sessions refresh
independently using `Promise.allSettled`. The two navigation snapshots are
retained independently before merging. `app.unavailable` records the source
keys `models`, `presets`, `status`, `serverInfo`, `gpus`, `engineNode`,
`workspace`, `history` and `sessions`; a successful read clears its flag.
Picker shows the existing unavailable alert and builds its catalog lists only
while open. Retained values can be stale and are not a claim of reachability.

## Transversal inventory

Paths in this table are relative to `internal/loom/ui/next/js`. Consumers that
already handle rejected reads receive the fix through the common boundary;
they do not need their own collection coercion or render-time try/catch.

| Shared state or input | Implementations and consumers checked | Changes |
| --- | --- | --- |
| `app.models`, `presets`, `workspace` (models, runtimes, providers, projects, capabilities, sessions, profiles), `nav` (conversations, projects), status/node/hardware objects | `app/main`, `shell`, `sections`, `palette`; `chat/engine`, `picker`, `composer`, `slash`, `view`; `cloud/page`; `local/page`, `hub`, `service`; `bench/page`; `harnesses/page`, `history`, `options`; `projects/page`; `inspector/context`, `inspector`, `params`; `resources/brain`, `skills`, `memory-items`; `settings/page`, `machines`, `policy`, `doctor`; `terminals/page`; `usage/page`; `onboarding/welcome` | API and store validation, retained snapshots, independent refreshes, unavailable flags, lazy Picker |
| `chat.items`, `frozen`, harness modes/config/files/commands, session/context and canonical events | `chat/engine`, `view`, `composer`, `messages`, `tools`, `requests`, `slash`, `picker`; `inspector/context`, `inspector`; `app/shell` | Store and event validation before reduction, including nested options and request forms |
| Usage snapshot Map: usage, native harness rows, provider balances | `core/observations`, `usage/page` | Reject invalid shared writes and retain the previous snapshot; existing auth clearing preserved |
| Hub download map | `local/hub` download watcher and cards/detail | Common API validates the status array; watcher already retains failures; Detail now handles rejected initial reads |
| vLLM shared engine, library/cache and download observations | `settings/vllm`, reused by Local and settings | Common API checks shapes; failed engine/library reads retain observations |
| Composer harness probe cache | `chat/composer`, `slash`, `harnesses/options` | Pending requests tracked separately; pending/failed reads retain controls |
| Dialog/toast arrays; language observable | `ui/dialog`, modal/toast consumers; `core/i18n`, translated components | Shared store validation covers dialog/toast writes; language has no API collection input |
| Page-local collections outside the shared stores | All pages under `features/`: machines/panes, harnesses/machines/lifecycle/history, workspaces/folders, environment, terminals, previews, voice/page/jarvis/mode, tasks, resources/brain/memory/memory-items/preferences/skills/mcp, usage, cloud, local, projects, inspector, bench, onboarding and settings; `app/activity`, `ui/folder` | Explicit API collection contracts apply throughout. Missing initial rejection handling repaired in settings, MachineDialog, Hub Detail, MCP and inspector context Preview. Existing page-local empty/error states remain supported |
| Tasks SSE, terminal WebSocket, voice WebSocket | `tasks/page`, `terminals/page`, `voice/mode`; server task envelope, terminal attachment/scrollback and voice frame contracts | Shared visible connection lifecycle, stale callback gating, shape-checked Tasks frames, safe voice JSON/PCM parsing |
| Raw JSON/SSE and binary requests | Composer chunked upload, Bench cancellation, Jarvis turn SSE, voice test TTS, Doctor bundle | Upload metadata and cancellation snapshots use the same shape helper; voice turn deltas and errors are checked before text publication. Existing binary error handling retained |

The server-side inventory checked `workspace_runtime.go`, the runtime contracts
and registry, `acp_registry.go`, `harness/acp_agents.json`, feature declarations,
probe/session/catalog routes and canonical event mappings. It includes Codex
App Server and ACP, Pi RPC and ACP, OpenCode HTTP/SSE and ACP, Claude Code ACP,
Antigravity native Stream JSON and ACP fallback, Hermes ACP, OpenClaw ACP,
DeepSeek Harness ACP, custom/local/SSH ACP entries, and planned descriptors.
All use the shared workspace/session/event UI boundaries. No adapter execution
or native account configuration changed; real harness compatibility was not
retested by this frontend work.

Engine implementations checked were owned llama.cpp single/router mode,
installed vLLM, direct linked llama.cpp/vLLM/OpenAI-compatible servers, remote
Loom Node forwarding and legacy worker routing (`backend_router.go`,
`engine_routes.go`, `engine_direct.go`, `engine_node.go`, `engine_worker.go`).
Local and configured Cloud Chat Completions use the same retained discussion
UI. No engine handler or launch behavior changed. In particular, a node probe's
model list, a harness model-source count, and vLLM's job string keep their
different native contracts.

## Mobile lifecycle

Visible observation polling refreshes status/hardware every four seconds and
library/workspace/navigation/node state every thirty seconds. Visibility,
pageshow (including bfcache) and online events trigger immediate refresh. A
resume that arrives while an aborted read settles queues a fresh read.

The existing canonical discussion journal reconnects from its event sequence.
Tasks, terminal and voice transports now share a connection owner that closes
on hide/offline/pagehide and reattaches on resume. Old callbacks cannot publish
into the new connection. Terminal attachment requests a ticket for the same
terminal ID and replaces display scrollback before server replay. Voice keeps
its existing microphone, cancels transient speech/turn playback on suspension
and reconnects without resending a turn. These are observation/attachment
operations, not process creation or input replay.

## Regression and route checks

The deterministic Node regressions use the real API, store, reducer or page
modules with in-memory responses/resources; they require no sockets, paid
calls or installed model. Original source files were extracted from starting
commit `68c5bc4` into temporary directories and supplied through the tests'
`LOOM_*_BASELINE` overrides. Before-fix failures were observed for the exact
502 model error, other refresh/store shapes, usage and vLLM retention,
Composer cache retention, canonical event corruption, closed Picker traversal,
aborted-read resume race, terminal reattachment, Tasks snapshot corruption,
voice malformed JSON/error frames, raw upload metadata, raw Bench cancellation,
and eleven unhandled initial page reads. The repaired
versions pass. Additional positive checks preserve string discussion
attachments alongside object harness file-change records, negative environment
results and the direct-engine probe response contract.

`tools/tests/ui-routes/crawl.mjs` starts Loom with a temporary HOME and data
directory. Every route/scenario gets a fresh browser context. Scenarios include
fresh/healthy APIs, 401, 502 JSON error, malformed JSON, wrong container shape,
network abort, offline and engine-only failure. Auth bootstrap and onboarding
preferences remain healthy so the domain routes can be reached. The crawl
fails on any pageerror, PageGuard screen, client error or missing/duplicate
view. Recovery switches APIs back to valid responses, refreshes state and
remounts pages with mount-only reads by hash navigation in the same document;
it verifies valid reads and cleared shared failure flags without reloading.

The Chat engine-only scenario opens synthetic Cloud and agent sessions and
checks that drafts and the send control remain usable on resume. It supplies
synthetic journal events and refuses generation/startup requests. Chromium
runs desktop and iPhone viewports; WebKit runs the iPhone viewport. Unsupported
local WebKit installations print a clear skip. CI installs Chromium and WebKit
with system dependencies and treats WebKit launch failure as failure.

Mission A validation passed UI checks, Linux binary build, Go vet, Windows
cross-build and documentation links. The full Go suite was attempted with
Go 1.26.9 and a temporary HOME: sandbox socket denial stopped
`TestHandleHubSearch` and `TestFetchReleaseChannels`; other packages completed.
The route crawl was attempted after building but the sandbox denied its first
loopback listener (`127.0.0.1`, EPERM), before browser launch. Therefore the
expanded crawl, real WebKit/iPhone behavior, physical microphone/audio and live
engine/harness recovery remain unverified here. Run the crawl and full Go suite
in a socket-capable environment before declaring browser acceptance complete.
