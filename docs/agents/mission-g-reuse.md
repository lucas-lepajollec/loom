# Mission G: native answer and harness metadata reuse

Scope: [reuse audit](../audits/2026-10-reuse-audit.md) §2.3 and §7 ranks 4–5.
This is a behavior-preserving extraction. No protocol, executable default,
public API field, UI markup/style or dependency was added. No user-visible bug
was fixed; failing-before-fix bug evidence is therefore not applicable.

## Reuse decision

The existing `runtime` canonical events and protocol mappers are the reuse
boundary. Reviewed the committed Codex schema, Pi fixture corpus, OpenCode
OpenAPI schema and Antigravity 1.3.1 structured-stream fixtures, plus upstream
[Codex app-server documentation](https://learn.chatgpt.com/docs/app-server),
[Pi sources](https://github.com/earendil-works/pi/tree/main/packages/coding-agent),
[OpenCode server documentation](https://opencode.ai/docs/server/) and
[coder/acp-go-sdk](https://github.com/coder/acp-go-sdk).
These describe different wire protocols; the ACP SDK does not replace native
answer assembly or Loom's launcher/inspect metadata. Reuse the current mappers
and share only pure answer and item-close logic, rather than adopting a new
transport/SDK layer. The audit's ACP TCK/SDK trial remains rank 7, out of scope.

## Rank 4

Three root collectors became one `runtime.AnswerAccumulator`:

| Adapter | Replacement rule | Other behavior retained |
| --- | --- | --- |
| Codex app-server | Replace item and clear `item:` children | Final-only correction, first-item order, cumulative usage |
| Pi RPC | Replace item and clear `item:` children | Indexed text blocks, final-only correction, per-message usage |
| OpenCode HTTP/SSE | Replace one item | Interleaved part order, per-message usage |
| Antigravity stream-json | Replace entire answer | Stream/final correction order, reported turn usage |

`Apply` returns a copied assistant snapshot (including empty replacements),
ignoring reasoning and tools. `Text` supplies final history/results. Pure
`runtime.CloseItems` retains the projected tool kinds/partial output, sorted
item IDs and native terminal raw frame. Root projection still owns display
rows; adapters retain synchronization, usage, state publication and lifecycle.

Before editing production code, the test replayed every committed native JSONL
fixture through its real mapper (Codex also uses the session notification hook)
and the original three collector algorithms. It captured immutable canonical
events, display rows, each answer snapshot and final answer under
`testdata/agents/answers/`. After extraction the same replay uses the shared
accumulator and compares serialized JSON **byte-for-byte**. All four protocols
already had fixtures: 15 Codex, 13 Pi, 4 OpenCode and 11 Antigravity, 43 total;
none needed an initial protocol fixture. Explicit replacement-scope tests add
interleaved items, prefix children, empty replacements and immutable snapshots;
item-close tests cover completion, failure, cancellation and interruption.
A temporary fault injection disabling child clearing produced the expected
`TestAnswerReplacementScopes` assertion failure; the original source was restored.
These validate Loom against synthetic fixtures, not installed harness accounts.

## Rank 5

`harness/catalog.json` replaces `acp_agents.json`, `inspect.json` and the
hand-written `remoteHarnessDefs` catalog. One record owns ID/name/logo/docs,
launcher and inspect metadata. Remote eligibility is explicit; argv and
prerequisites derive from the local launcher, without duplicated pins.
The existing six-recipe ordering is retained. Nil consumer data is unsupported.
The custom ACP record is non-executable documentation of user-owned launchers.

| Family | Builtin ACP launch | Inspect/lifecycle | Builtin SSH/Node recipe |
| --- | --- | --- | --- |
| Codex | Existing pinned bridge | Existing | Existing |
| Claude Code | Existing pinned bridge | Existing | Existing |
| Pi | Existing pinned bridge | Existing | Existing |
| OpenCode | Existing `opencode acp` | Existing | Existing |
| Antigravity | Existing self `agy-acp` bridge | Existing | Unsupported |
| DeepSeek Harness | Existing pinned developer preview | Unverified remains true | Unsupported |
| Hermes | Existing `hermes acp` | Existing | Existing |
| OpenClaw | Existing `openclaw acp` | Existing | Existing |
| Custom/registry ACP | Explicit user/registry command only | No builtin spec | No builtin recipe; user-defined transport retained |

`TestHarnessCatalogFamilyMatrix` independently enumerates all nine families and
checks each consumer's supported/unsupported projection. It checks POSIX SSH,
Windows SSH (decoding its actual PowerShell argv), Node launch and offer
identity, and rejection of pure Node without the harness module. Missing,
duplicate or new catalog families fail until the matrix is updated. A frozen
pre-change `harness-catalog.json` fixture also compares builtin ACP launchers,
all inspection fields and remote recipes byte-for-byte. Antigravity's existing
self bridge is separately checked; no real executable path is stored in a fixture. Temporary omissions from each
of the builtin ACP, inspect and remote recipe consumers independently failed
`TestHarnessCatalogFamilyMatrix`; all source changes were restored before the
final green run. This verifies regression-test sensitivity, not a claimed
pre-existing product bug.

## Transversal inventory

Checked means relevant definitions, dispatch and consumers were inspected;
it does not claim live compatibility or complete browser certification.

| Boundary | Checked implementations/consumers | Changed subset |
| --- | --- | --- |
| Families/transports | Codex app-server/ACP, Claude ACP/extensions, Pi RPC/ACP, OpenCode HTTP/SSE/ACP, Antigravity native/ACP bridge, DeepSeek preview/ACP, Hermes/OpenClaw ACP, custom/external registry ACP, local stdio, POSIX/Windows SSH, authenticated Node bridge | Three native root collectors; generic ACP registration metadata; shared remote/Node recipe inputs |
| Canonical display/persistence | `runtime/canonical.go`, root `agent_projection.go`, `workspace_sessions.go`, `workspace_native.go`, ACP session/state, discussion journal/portable context and `store_compat.go`; runtime local/OpenAI adapters | Pure answer/item-close helper and root projection delegation only; no store/session schema changes |
| Launch/inspect/lifecycle | `acp_registry.go`, `harness_inspect.go`, `harness_lifecycle.go`, `harness_npm.go`, `remote_machines.go`, `node_harness.go`, `node_bridge.go`, custom/catalog registry and compatibility consumers | One declarative catalog plus typed metadata in existing `harness`; root projections/aliases; remote launch lookup removed |
| Family resource hooks | `harness_features.go`, `model_sinks.go`, `skills_sink.go`, `mcp_gateway_harness.go`, `brain_agents.go`, `harness_resume.go`, `harness_providers.go`, `harness_usage.go`, account/probe/evidence paths | None; distinct resource and native schemas stay separate |
| Engines and direct execution | llama.cpp/backend router, vLLM, linked/direct/Node engines; local Conversation and configured OpenAI-compatible cloud paths | None; they do not consume the harness recipe catalog or native answer helper |
| Shared UI data/pages | Core state/shape and runtime descriptors; chat engine/execution/picker/tools, inspector, Usage, local/cloud/models, Resources/Brain, Settings/Machines/Doctor/Policy, workspaces/terminals/environment, projects/tasks/bench/voice and route crawler; harness modules inspected read-only | None, including the reserved `ui/next/js/features/harnesses/` directory; public data shape unchanged |
| Other data tooling | Tested versions/capabilities, agents-watch, curated/external ACP registry tests, lifecycle/install/probe tests | Existing lifecycle catalog test now consumes the shared projection; added deterministic baselines/matrix |

## Validation

All Go commands used `GOTOOLCHAIN=go1.26.9`, temporary HOME/XDG/Loom data,
explicit existing module cache and a temporary build cache. No real `~/.pi`,
`~/.hermes`, `~/.dsh`, `~/IA` or Loom Node data was used. No models/accounts
were called. Binary and validation logs are not deliverables.

| Check | Result |
| --- | --- |
| Pre-extraction capture and post-extraction comparison | All 43 canonical native fixtures and launch/inspect baseline passed |
| Replacement scopes, item closing, complete family/transport matrix | Passed |
| Focused root native/ACP/remote/Node/feature/compatibility/custom regressions | Passed |
| `gofmt`, `go vet ./...` | Passed |
| `go test ./...` | Attempted; root `TestHandleHubSearch` (`backend_hub_test.go:111`) and platform `TestFetchReleaseChannels` (`platform/sys_update_channel_test.go:11`) panic: `listen tcp6 [::1]:0: socket: operation not permitted` |
| Other packages in full run | Passed, including runtime and all native/ACP children, harness, engines, Brain, store, tools and agents-watch |
| Documentation link checker and `git diff --check` | Passed; 199 local documentation targets |
| `make build` | Passed |
| `GOOS=windows go build -o <temporary-output> ./cmd/loom` | Passed; compile only |
| `make check-ui` | Passed; 24 Node test-file suites |
| `npm ci --prefix tools/tests/ui-routes --no-audit --no-fund` | Attempted (bounded fetch); DNS `EAI_AGAIN` for Playwright npm download |
| `node tools/tests/ui-routes/crawl.mjs bin/loom` after build | Attempted; `ERR_MODULE_NOT_FOUND` for Playwright; no routes exercised |

Unverified: full root/platform suite after socket denial, desktop/390px/320px
browser routes, installed/native account execution, real SSH/Node machines,
Windows runtime and ACP TCK conformance. Existing UI/layout and transport
selection are unchanged. `COMMIT_MSG.txt` is the requested review handoff;
no commit was created.
