# Mobile layout audit — Mission F / rank 6

Status: implementation and socket-free regressions verified, browser acceptance
pending. The implementation sandbox denies loopback listeners (`EPERM`) and
Chromium startup (`sandbox_host_linux.cc`, shutdown operation not permitted).
There is no claim that every route has been observed working on a phone. The
required rendered before-failure proof and before/after screenshots remain
unverified; source-render regressions are a separate, narrower proof.

## Root cause and changes

The agent header shared one flex row between its logo, shrinking text column
and actions. Its fixed-height state pill allowed multiple lines of text to paint
outside its box. On phones, installation grids reduced the Manage/Use columns
to 56–64px and hid version data. Machine detail used the same pattern. Desktop
Environment tables required horizontal scrolling; local model tables hid metrics
when their header disappeared.

The existing `mobile.css` overrides now supply shared wrapping buttons, pills,
segments, page tabs, long values, key/value rows, dialogs and popovers. An object
header grid places identity/refresh in its first row, state in the second,
description and metadata across the width, and actions below. Both installation
views reuse `scope-control` labels and stack their rows. Model and Environment
tables keep labeled metrics on phones. No colors, tokens, decorative surfaces,
execution capabilities, data schemas or runtime policies were added.

Reuse: existing vendored Preact/htm, pinned Playwright 1.64.0, Modal/Tip/Switch/Seg
and Usage/Bench's `data-label` pattern. No new dependency, protocol implementation
or runtime layer was needed for a presentation change.

## Transversal source inventory

| Scope checked | Consumers / implementations | Changes |
| --- | --- | --- |
| Agent descriptors and transports | `workspace_runtime.go`, `acp_registry.go`, `harness/acp_agents.json`, `harness_features.go`, `agent_native.go`: Codex app-server/ACP; Claude Code ACP; Pi RPC/ACP; OpenCode HTTP/SSE/ACP; Antigravity native CLI stream/ACP bridge; DeepSeek Harness, Hermes, OpenClaw; custom/generic ACP and SSH/node registrations | No Go, descriptor, transport, credential or protocol changes. Shared agent presentation applies to all descriptors. |
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

## Rank 6 extraction

`features/harnesses/page.js` falls from 63,874 bytes to 4,336 bytes. The largest
extracted module is `agent-page.js` (15,868 bytes).

| Module | Responsibility |
| --- | --- |
| `page.js` | Route, catalog selection and dialog/drawer wiring |
| `catalog.js` | Catalog search/add UI, family cards, installation grouping |
| `agent-page.js` | Agent observations, header/actions and agent/native preview composition |
| `agent-machines.js` | Per-machine lifecycle, version, Manage/Use controls |
| `discussions.js` | Loom discussions, native history, import selection/actions |
| `models.js` | Model option helpers, default model chips and expansion |
| `settings.js` | Model sources, MCP/skills bindings, settings/resource dialog |
| `dialogs.js` | Add detected/custom agents, custom ACP configuration |
| `capabilities.js` | Capability labels/evidence and existing ACP page selection |
| `machine-state.js` | Existing native installation/account/resource inspection |

`account.js`, `history.js`, `lifecycle.js`, `machines.js` (the existing connection
dialog) remain separate and retain their behavior. Eleven extracted function
bodies were compared byte for byte with the original, excluding the functions
intentionally changed for layout/composition. Model expansion and settings dialog
state moved into their respective sections; choice dispatch, feature gating,
consent and dialog lifecycle have deterministic behavior tests. Unused imports
(`useVisibleRefresh`, `locale`, `remoteHarnessTarget`) were removed after verifying
there were no symbol uses. No speculative function/branch deletion was made.

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
git archive HEAD internal/loom/ui/next | tar -x -C .project-local/mission-f/baseline-ui
CRAWL_LAYOUT=1 CRAWL_LANG=fr CRAWL_ROUTES='^harnesses/codex$' \
  CRAWL_BASELINE_UI=.project-local/mission-f/baseline-ui/internal/loom/ui/next \
  CRAWL_SHOTS=before node tools/tests/ui-routes/crawl.mjs bin/loom
```

This baseline command is intended to exit nonzero on layout failures; the after
command must pass. Use the pre-mission commit for `git archive` once these changes
have been committed. `CRAWL_BASELINE_UI` serves those original UI assets into the
same isolated, synthetic crawl. It has not been executed successfully here.

Requested screenshot paths (ignored, **not generated in this environment**):

- `.project-local/mission-f/before-agent-390.png`
- `.project-local/mission-f/after-agent-390.png`

Local implementation evidence is in `.project-local/mission-f/`: original
sources, `before-unit.log`, `before-header-unit.log`, `before-machine-unit.log`,
`before-tables-unit.log`, `extraction-verification.txt`, build/check logs and
Playwright/socket failure logs. These files are excluded from version control.

## Validation outcome and remaining acceptance

- Passed: `make check-ui`, Go 1.26.9 `make build`, `go vet ./...`, Windows
  `GOOS=windows go build ./cmd/loom` (output directed to `/tmp`), fixture shape and
  mutation guards, static imports and extraction behavior tests.
- No Go files changed; gofmt has no changed-file target.
- Attempted full `go test ./...` with a temporary HOME: `TestHandleHubSearch`
  (`internal/loom`) and `TestFetchReleaseChannels` (`internal/loom/platform`)
  panic because httptest cannot bind a loopback socket. The affected package
  suites cannot complete; this is not reported as a green Go test run.
- Attempted the required binary route crawl after rebuilding: it cannot bind
  `127.0.0.1` (`EPERM`). Direct Chromium launch is also denied. Therefore no
  rendered route results, screenshot pair or real-before failure proof exists.
- Pending: run baseline and full after crawl, inspect screenshots on desktop and
  both widths, resolve any measured layout failures, and physical iPhone
  acceptance. No native CLI/account/GPU behavior is claimed as observed here.

No commit or external publication was performed. `COMMIT_MSG.txt` records the
proposed summary, inventory and checks, including these acceptance limitations.
