# UI validation and recovery

Use vendored Preact/htm, existing UI tests and the pinned Playwright route harness. No new parser, runtime layer or paid model is required. Source checks are separate from rendered browser/device acceptance.

## Data boundary regressions

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


## Free tests and proof scope

`make check-ui` checks syntax, component bindings, relative imported files and
named exports. Deterministic tests exercise model dispatch/expansion, resources,
consent/readiness, header placement and visible row/cell labels. Old source
fixtures fail seven structural regressions: header refresh ownership, Agents
machine labels, Machines agent labels, and Local/Services/Docker/Proxmox metric
labels. Those tests pass with the changes. Browser detector unit cases reject
squeezed pills, painted text outside controls, sibling overlap and oversized
buttons at both widths.

The crawl keeps crash/degraded/recovery checks and adds real DOM geometry checks
at **390 × 844** and **320 × 740**, with a 2px rounding tolerance:

- content and control bounds inside the viewport; root horizontal overflow;
- buttons wider than the viewport;
- multi-line heading/pill/button text in columns narrower than 40px;
- text painting below a fixed control box;
- intersecting sibling header boxes.

Intentional clipped thread/picker scroll content is distinguished from page
layout. Table data, native history, expanded models, account/resource/add/custom
agent dialogs, provider/service dialogs and tooltips have synthetic cases. The
fixtures refuse generation, login, installs, imports and writes; estimates and
quota observations are answered synthetically. Homes/data roots are temporary,
with harness-specific home overrides removed. No paid model call is required.

Run on a machine allowing loopback sockets and Playwright:

```bash
GOTOOLCHAIN=go1.26.9 make build
npm ci --prefix tools/tests/ui-routes --no-audit --no-fund
node tools/tests/ui-routes/crawl.mjs bin/loom
CRAWL_LAYOUT=1 CRAWL_LANG=fr CRAWL_SHOTS=after node tools/tests/ui-routes/crawl.mjs bin/loom
```

`CRAWL_LAYOUT=1` runs populated Chromium phone cases only; the default command
also checks fresh/degraded/recovery states, desktop and available WebKit.
`CRAWL_FULL=1` runs all states on both phone widths and desktop.
`CRAWL_ROUTES` is an optional route regex; `CRAWL_LANG` defaults to English.

To prove the rendered defect on the original UI while using the current binary:

```bash
mkdir -p .project-local/mission-f/baseline-ui
git archive 69cdca9 internal/loom/ui/next | tar -x -C .project-local/mission-f/baseline-ui
CRAWL_LAYOUT=1 CRAWL_LANG=fr CRAWL_ROUTES='^harnesses/codex$' \
  CRAWL_BASELINE_UI=.project-local/mission-f/baseline-ui/internal/loom/ui/next \
  CRAWL_SHOTS=before node tools/tests/ui-routes/crawl.mjs bin/loom
```

This baseline command is intended to exit nonzero on layout failures; the after
command must pass. The example pins the pre-layout merge commit; choose a deliberately recorded baseline for later changes. `CRAWL_BASELINE_UI` serves those original UI assets into the
same isolated, synthetic crawl. It has not been executed successfully here.


Screenshots and local evidence belong in ignored `.project-local/`, never public docs. Earlier Mission A/F sandbox runs could not bind loopback sockets or launch Chromium; no physical-phone/microphone acceptance is implied. Later checks must record their own results.

## Layout inventory and registered routes

| Scope checked | Consumers / implementations | Changes |
| --- | --- | --- |
| Agent descriptors and transports | `workspace_runtime.go`, `acp_registry.go`, `harness/catalog.json`, `harness_features.go`, `agent_native.go`: Codex app-server/ACP; Claude Code ACP; Pi RPC/ACP; OpenCode HTTP/SSE/ACP; Antigravity native CLI stream/ACP bridge; DeepSeek Harness, Hermes, OpenClaw; custom/generic ACP and SSH/node registrations | No Go, descriptor, transport, credential or protocol changes. Shared agent presentation applies to all descriptors. |
| Engines | llama.cpp/local runtime, vLLM and linked engines; Models/Local/Engine/Settings consumers | Local library metric labels and shared CSS only; no loading/lifecycle changes. |
| Stores and execution choices | `core/state.js`, `core/shape.js`, workspace/nav/model/project data; Chat picker/engine, inspector/selection, Cloud, Agents, Projects, Usage, Bench, Machines | No store/schema/API changes. Imports continue using existing exports. |
| Brain / resources | Brain sources, memory, skills, MCP and Resources alias | Shared CSS only; no memory/retrieval/distribution changes. |
| Route registry | Every entry of `app/routes.js`, section navigation, route list in `tools/tests/ui-routes/crawl.mjs` | The inventory test checks that every registered top-level route remains in the crawl. Visual checks are pending. |

Routes retained in the crawl:

- `chat`, `models` (existing placeholder), `cloud`, `local`, `engine`, `workspaces`,
  `project`, `resources`, `terminals`, `environment`, `voice`, `jarvis`.
- `harnesses`, `harnesses/history`, and `harnesses/<id>` for **every** runtime ID
  returned by the isolated server, including non-agent fallback routes.
- `machines`, `machines/local`, synthetic remote machine detail,
  `machines/workspaces`, `machines/terminals`, `machines/environment`.
- `tasks`, `brain`, `brain/sources`, `brain/memory`, `brain/skills`, `brain/mcp`,
  `usage`, `bench`.
- `settings` and `general`, `internet`, `startup`, `notifications`, `policy`,
  `doctor`, `security`, `about` subsections.
- Added populated cases: `project/fixture-project`, `local/hub`,
  `environment/docker`, `environment/proxmox`.

Page markup changed in Agents, Machines' agent controls, Local metric labels and
Environment cell labels. Every other page consumes the shared CSS. The inventory
above is source inspection, not physical-device or native-account acceptance.
Existing protocols were not changed or newly certified by this mission.

