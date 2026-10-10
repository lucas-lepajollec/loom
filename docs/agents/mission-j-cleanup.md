# Mission J cleanup and guard verification — 2026-10-10

Branch `codex/mission-j`, starting commit `5985560`, clean starting tree.
No product behavior, UI markup/style, runtime protocol, dependency, credential
configuration or public API changed. No commit was created.

## Documentation consolidation

| Earlier report | Durable destination | Historical evidence retained |
| --- | --- | --- |
| UI data boundaries | Architecture: validated observations, full consumer inventory, visibility/connection ownership; agent UI validation guide | Exact 502 cause and original validation limits |
| Mobile layout audit | Workspace phone rules; architecture module ownership; UI validation commands, route inventory and geometry proof | Layout cause, original outcomes and pending physical-device acceptance |
| Mission E evidence | Agent compatibility; implementation guide inventory and evidence ownership | Reuse decision, failing-before proofs, first-stage limits and dated results |
| Mission G reuse | Architecture answer assembly; ACP consumer/coverage matrix | Reuse decision and original results; all 43 baseline fixture rules retained canonically |
| Brain retrieval benchmark | `agents/brain-retrieval.md`, Brain overview and Voice verification limits | Complete commands, licence/hash/selection, metrics, baseline, inventory and natural-data limitations moved intact |
| October reuse audit | Dated original preserved with rank/delivery tables | Original measurements/proposals remain explicitly historical; #117 unverified in available history |

Old report paths remain short references or dated records so inbound links still
resolve. README indexes canonical guides; no instruction to maintain parallel
mission documentation remains. Earlier runs are not Mission J acceptance.

## Reuse and checked inventory

Use Go's official [deadcode tool](https://pkg.go.dev/golang.org/x/tools/cmd/deadcode)
from cached `golang.org/x/tools@v0.50.0`, pinned outside `go.mod`. Tests are included
and Linux/Windows/macOS call graphs are compared. Its official documentation
warns that unreachable results are configuration-specific and are not alone
safe deletion proof. Require the three-platform intersection **and** no other Go
identifier reference across every platform, test and tool. Retain public leaf
APIs, interface methods and any candidate with uncertain reachability. `go vet`
also runs. No staticcheck/runtime dependency was added.

Family declarations checked: Codex, Claude Code, Pi, OpenCode, Antigravity,
DeepSeek Harness, Hermes, OpenClaw and custom ACP; external registry/generic ACP
stays separate user configuration. Existing feature cases cover Codex app-server,
Pi RPC, OpenCode HTTP/SSE, AGY native stream and ACP fallbacks; the launch matrix
covers local, POSIX/Windows SSH and authenticated Node, including unsupported
recipes, preview and pure Node without a harness module. Changed: **tests and
documentation only** for these contracts. No adapter execution changed.

The static inventory includes original/local Conversation and configured Cloud
Chat Completions, llama.cpp single/router, vLLM, direct linked engines and Loom
Node forwarding; private store/discussion/Brain/resource/voice ownership remains
with its current implementation. Removed declarations are only compatibility
wrappers/aliases; their leaf functions stay. Full Go leaf suites and focused
compatibility/fixture tests validate the retained paths; actual accounts, engines,
SSH/Node machines and Windows/macOS runtime were not exercised.

All 101 authored JS modules were scanned for import reachability, exported
identifier references and i18n literal references; all four authored CSS files
were scanned for class references. The shared consumers checked include chat,
picker/composer/inspector, Local/Cloud/Models, Agents and history/resources/account,
Projects, Brain/MCP/skills/memory, Machines/workspaces/environment/terminals,
Usage/Bench, Tasks, Settings/Doctor/Policy, onboarding, Voice/Jarvis, core
state/shape/observations and all registered routes. Changed: **no product JS or
CSS**. Only the existing test lexer gains template-string observation, reused by
the guard.

## Proven Go removals

78 uncalled compatibility functions and 26 unreferenced aliases/constants were
removed, plus imports made unused by those deletions. No entire Go file, exported
leaf API, platform implementation or product UI code was deleted.

| File under `internal/loom` | Removed functions |
| --- | --- |
| `acp_compat.go` | `acpClip` |
| `engine_compat.go` | `canonicalHelpFlag`, `chatTemplateThinks`, `cloneEngineConfig`, `containsFold`, `effortDefaultIn`, `estimateComputeBytes`, `estimateKVBytes`, `estimateKVBytesPerLayer`, `estimateMTPBytes`, `finishLlamaFlag`, `flagTierFor`, `ggufReadInt`, `ggufReadIntArray`, `ggufReadString`, `ggufScalarSize`, `ggufSkipArrayKeepLen`, `ggufSkipValue`, `helpTokenArg`, `inferHelpKind`, `inspectChatTemplate`, `isHiddenLlamaFlag`, `kvBPE`, `kvHeadDims`, `layerIsSWA`, `layerNKVHeads`, `looksNumeric`, `lookupFlagConfigKey`, `mergeUniq`, `paramMatchesFlag`, `parseHelpAllowed`, `parseHelpChoices`, `parseHelpChoicesFromHelp`, `parseLlamaHelp`, `parseLlamaHelpFlagLine`, `pickGGUFContext`, `readGGUFMeta`, `rememberRouterEntry`, `renderRouterINI`, `routerDo`, `routerModels`, `splitArgs`, `splitHelpComma`, `splitHelpFlagAndDesc`, `writeRouterINI` |
| `openai_compat.go` | `safeFinishReason` |
| `platform_compat.go` | `fetchLatestRelease` |
| `resources_compat.go` | `asciiFold`, `copySkillDir`, `isLinkInto`, `mcpJSONError`, `mcpStringKeys`, `parseSkillMarkdown`, `placeSkill`, `skillBound`, `skillFileContent`, `skillSinkCurrent`, `yamlQuote` |
| `tools_compat.go` | `baseString`, `decodeHTMLEntities`, `decodeUddg`, `extractOutline`, `formatBytes`, `formatLines`, `goArticleMarkdown`, `goAttr`, `goCloneDoc`, `goConvert`, `goFetch`, `goFindByClass`, `goFirstByClass`, `goHasClass`, `goParseHTML`, `goText`, `snippetAround`, `titleOf`, `uniqueTerms` |
| `web_compat.go` | `handleNext` |

| File under `internal/loom` | Removed aliases/constants |
| --- | --- |
| `antigravity_compat.go` | `agyResult` |
| `discussion_compat.go` | `maxPortableMessages` |
| `engine_compat.go` | `chatTemplateCaps`, `ggufTypeUint8`, `ggufTypeInt8`, `ggufTypeUint16`, `ggufTypeInt16`, `ggufTypeInt32`, `ggufTypeFloat32`, `ggufTypeBool`, `ggufTypeUint64`, `ggufTypeInt64`, `ggufTypeFloat64`, `ggufMaxKV`, `ggufMaxString`, `ggufMaxArrayLen`, `estCUDAReserveB`, `estComputeSafety`, `estCtxComputePerEmbd`, `estCtxComputePerEmbdMLA`, `estCtxComputeF16Mask`, `estDefaultVocab`, `routerMaxEntries`, `routerLoadBudget`, `routerStateEntries` |
| `store_compat.go` | `bkTasks` |

## Retained frontend candidates

Static graph candidates: `features/resources/memory.js` and `preferences.js`.
Single-reference export candidates: `remoteHarnessTarget`, `SharedPreferences`
and `MachinesLink`. `MachinesSection` and its `MachineHarnesses` helper have no
external product reference but retain local references. The scan also finds
76 CSS class candidates and 534 dictionary keys without literal references.
These counts are **candidates, not unused-code proof**: computed class/key names,
`tSource` server catalogs, test imports and runtime consumers matter. Playwright
installation/crawl is blocked, so none is deleted. The canonical MachineDialog
and Machines page remain. No feature disappearance is inferred from a regex.

## Guard sensitivity and proof

`make check-guards` is included in `make check-ui` and CI. Reuse the existing
feature/launch matrix and UI lexer, Node built-ins and existing Go fixtures.
No parser package, new runtime layer or network check is introduced. A temporary filesystem fixture also proves the real check entry point rejects root-file additions, oversized modules, untranslated text and dictionary drift together. The small
snapshot parity helper permits deliberately invalid dictionaries without editing
the real dictionaries; existing i18n key/plural/interpolation tests remain.

- Contract coverage rejects each missing feature/inspect/lifecycle/model-source/
  docs dimension, new/duplicate families and obsolete entries. Before adding
  the docs matrix, the new test failed with missing docs coverage for all nine
  catalog families. After adding it, the test passes. This is guard sensitivity,
  not a pre-existing runtime bug.
- Structure fixtures reject a new root Go file and growth in new/grandfathered
  JS modules, including UTF-8 byte growth; passing counterparts establish the
  exact ceilings. Root Go files require ownership reasons, and obsolete entries
  fail. The default JS ceiling is 24 KiB; five existing larger modules are frozen
  at measured bytes.
- Copy/parity fixtures reject missing translation keys, variables and plural
  forms, new English/French HTML text, attributes, nested expressions, labels and
  notifications. Exact file/literal/count baselines prevent duplication and fail
  on stale exceptions. A regression for backtick/conditional notification copy
  **failed first** (`actual: ''`, expected untranslated diagnostic), then passed
  after extending the shared lexer/template/call-range inspection. No product
  copy was changed. Computed strings beyond lexical observation still need review.

## Validation on final code

All Go commands use `GOTOOLCHAIN=go1.26.9`, temporary HOME/XDG/Loom roots and
explicit caches. No real Pi/Hermes/DSH/IA/Node profile, login, updater, account,
engine or paid model was used. No private paths or logs are deliverables.

| Check | Result |
| --- | --- |
| Three-platform `deadcode -test ./...` before deletion | Completed, pinned v0.50.0; 78 removed functions are in all three reports and have no other Go identifier reference |
| Final Linux deadcode run | Completed; removed wrappers absent; remaining candidates retained |
| `make check-guards` / eight direct Node guard tests | Passed, with deliberate negative and positive fixtures |
| Focused Go contract/feature/catalog, compatibility, native answer and update-channel tests | Passed |
| `gofmt`, `git diff --check` | Passed |
| `go vet ./...` | Passed |
| `make build` | Passed, Linux embedded UI binary |
| `GOOS=windows go build -o <temporary-output> ./cmd/loom` | Passed; compile only |
| `make check-ui` | Passed, guards, syntax and 28 UI test-file subtests; not 28 assertions |
| `go test ./...` | Attempted; root `TestHandleHubSearch` and platform `TestFetchReleaseChannels` panic on `listen tcp6 [::1]:0: socket: operation not permitted`. Remaining packages passed; affected suites incomplete |
| Broader focused root suite | Attempted; socket denial in `TestRouterActivateLoadsWithoutRestartingEngine`, `TestRouterFrontRewritesModelAndHealth`, `TestRouterAPIOverridesBecomeVariantWithoutTouchingConfig`, `TestRouterUnloadKeepsEngineAndClearsSelection`; then `TestEngineNodeLinkAndForward` socket panic |
| `npm ci --prefix tools/tests/ui-routes --no-audit --no-fund` | Attempted; `EAI_AGAIN` resolving npm Playwright download |
| `node tools/tests/ui-routes/crawl.mjs bin/loom` after build | Attempted; `ERR_MODULE_NOT_FOUND` for Playwright; no desktop/390px/320px browser routes exercised |
| Documentation checker | Passed; no missing local targets |

Remaining acceptance: complete socket-dependent suites and desktop/phone crawl
in a socket-capable environment with Playwright. Frontend candidate removals,
real accounts, native machines, GPU/speech and Windows/macOS execution are not
claimed verified. Mission J introduces contributor guard behavior only; product
behavior and design remain unchanged.
