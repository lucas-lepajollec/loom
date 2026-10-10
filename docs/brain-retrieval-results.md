# Measured Brain retrieval refinements — Mission I

Measured on `codex/mission-i`, 2026-10-10, against the unchanged
[retrieval fixtures and pinned LongMemEval input](brain-retrieval-benchmark.md).
No model, extraction, embedding, judge or paid API calls were made.
No service, dependency, runtime protocol, UI layout or memory policy was added.

The retained changes share the two BM25 formula sites and fix loss of distinct
packing citations. Natural-only retrieval quality is unchanged. Three broader
experiments were rejected because improvements in one category came with losses
in another. There is no claim of improved natural semantic or temporal quality.

## Method and reuse decision

The clean starting tree was inspected before editing. The direction, architecture,
workspace contracts, agent guide, implementation brief and reuse audit were read.
The [audit's Mem0/Letta review](audits/2026-10-reuse-audit.md#5-brain-existing-reuse-and-a-free-retrieval-benchmark)
supports traceable provenance, separate current/as-of evidence and direct offline
prediction measurements. Their model-backed ingestion and separate memory stores
are unnecessary here. Brain already implements local BM25, cosine candidates,
RRF (`k=60`), Markdown updates and explicit frozen-context refresh; these were
reused. No upstream code or new dependency was imported. Network was reserved for
the dataset; the already cached file passed the documented importer's SHA check,
so no download was needed.

The documented `--select-once` importer produced the fixed 24 natural cases in
ignored `.project-local/brain-retrieval/subset`, alongside the eight synthetic
cases. Selection and full histories were unchanged across every experiment.
There are 21 natural evidence cases and three natural abstentions; synthetic has
seven evidence cases and one abstention. Abstentions have null recall denominators.
Natural knowledge-update labels remain pending review, so temporal correctness
is measured only for the reviewed synthetic fixtures.

Both `TestRetrievalEvidence` and `TestBrainRetrievalFinalContext` ran before and
after **each** candidate on synthetic and cached natural input. Each experiment
starts from the retained scoring/provenance version, rather than accumulating
rejected edits. All category span/session/complete-evidence top-k, packed recall,
final presence, scope leakage and abstention observations were compared. Final
retained results have no per-category or per-case evidence regression on these
fixtures. This establishes fixture behavior, not a general quality guarantee.

Reports are under ignored `.project-local/brain-retrieval/mission-i/`, in paired
`before-*`, `scoring-*`, `provenance-*`, `auxiliaries-*`, `plurals-*`, `sections-*`
and `final-*` directories (`*-synthetic` and `*-natural`). Each contains
`evidence.json` and `final-context.json`, including per-category JSON numbers.
`*-natural` is the combined 32-case suite; natural-only numbers below filter its
24 natural IDs. Reports contain natural content/IDs and are not public artifacts.

## Results per change

All table values are percentages. `R` is strict supporting-span Recall@1/5/10;
`P` is packed supporting-span recall at 512/1,500/3,000 approximate tokens.
Within a table, the baseline applies to the immediately following retained step;
all three rejected experiments compare against the provenance step.

| Step | Synthetic R | Synthetic P | Decision |
| --- | --- | --- | --- |
| Untouched baseline | 78.57 / 85.71 / 100.00 | 78.57 / 78.57 / 92.86 | Reference |
| Share pure BM25 scoring | 78.57 / 85.71 / 100.00 | 78.57 / 78.57 / 92.86 | Retained; exactly neutral |
| Retain distinct packing provenance | 78.57 / 85.71 / 100.00 | 85.71 / 85.71 / 100.00 | Retained; multi-session evidence improves |
| Drop extra English auxiliary query words | 78.57 / 85.71 / 100.00 | 85.71 / 85.71 / 100.00 | Reverted; natural category losses |
| Normalize regular plural endings | 78.57 / 85.71 / 100.00 | 85.71 / 85.71 / 100.00 | Reverted; natural category losses |
| Pack retrieved parts of a heading together | 78.57 / 85.71 / 100.00 | 85.71 / 85.71 / 100.00 | Reverted; natural category losses |
| Gate packed and all top-k evidence | 78.57 / 85.71 / 100.00 | 85.71 / 85.71 / 100.00 | Retained; evaluation-only, neutral quality |

| Same step | Natural-only R | Natural-only P | Natural observation |
| --- | --- | --- | --- |
| Untouched baseline | 27.78 / 55.56 / 75.79 | 56.75 / 81.75 / 92.46 | Reference |
| Share pure BM25 scoring | 27.78 / 55.56 / 75.79 | 56.75 / 81.75 / 92.46 | Unchanged |
| Retain distinct packing provenance | 27.78 / 55.56 / 75.79 | 56.75 / 81.75 / 92.46 | Unchanged |
| Drop extra English auxiliary query words | 25.40 / 53.17 / 71.03 | 56.75 / 72.22 / 90.08 | Knowledge-update and preference regress |
| Normalize regular plural endings | 30.16 / 55.56 / 75.79 | 59.13 / 76.98 / 92.46 | Temporal gains; knowledge-update and preference regress |
| Pack retrieved parts of a heading together | 27.78 / 55.56 / 75.79 | 60.32 / 67.46 / 85.32 | Assistant gains at 512; multi-session, knowledge-update, preference and temporal losses |
| Gate packed and all top-k evidence | 27.78 / 55.56 / 75.79 | 56.75 / 81.75 / 92.46 | Unchanged |

The prompt's baseline is the **combined** suite: R remains
40.48 / 63.10 / 81.85; P improves from **62.20 / 80.95 / 92.56** to
**63.99 / 82.74 / 94.35**. These gains come entirely from the synthetic duplicate
case, not from natural questions.

## Category results: retained version versus baseline

Natural-only before and after are identical in every listed category:

| Natural category | Evidence cases | R before = after | P before = after |
| --- | --- | --- | --- |
| Single-session user | 3 | 66.67 / 66.67 / 100.00 | 100.00 / 100.00 / 100.00 |
| Single-session assistant | 3 | 33.33 / 66.67 / 66.67 | 33.33 / 66.67 / 100.00 |
| Single-session preference | 3 | 0.00 / 16.67 / 33.33 | 16.67 / 66.67 / 66.67 |
| Multi-session | 3 | 27.78 / 55.56 / 80.56 | 63.89 / 72.22 / 80.56 |
| Temporal reasoning | 3 | 33.33 / 50.00 / 83.33 | 50.00 / 100.00 / 100.00 |
| Knowledge update | 6 | 16.67 / 66.67 / 83.33 | 66.67 / 83.33 / 100.00 |

Synthetic multi-session P rises from 50 / 50 / 50 to 100 / 100 / 100.
Other synthetic categories, all retrieval rankings, abstention observations,
scope leakage, frozen-memory refresh and synthetic as-of evidence are unchanged.
Packing now retains identical text from different source/path/heading citations,
while deduplicating repeated text with the same citation. That uses slightly more
tokens: mean synthetic injection rises by 1.625 tokens; combined-suite mean by
0.40625 tokens, at every budget. Packing still checks the full token budget before
including a passage, including its citation.

| Actual final complete evidence | Before at 512/1,500/3,000 | After at 512/1,500/3,000 |
| --- | --- | --- |
| Synthetic Local and Cloud (each) | 71.43 / 71.43 / 85.71 | 85.71 / 85.71 / 100.00 |
| Synthetic harness context | 14.29 / 14.29 / 14.29 | 14.29 / 14.29 / 14.29 |
| Combined Local and Cloud (each) | 17.86 / 17.86 / 21.43 | 21.43 / 21.43 / 25.00 |
| Combined harness context | 3.57 / 3.57 / 3.57 | 3.57 / 3.57 / 3.57 |

Natural-only final evidence is **0/21** before and after for each route/budget.
At 1,500 tokens the missing-stage report attributes 16 cases to assembly policy
or frozen context, four to packing and one to retrieval. Large natural histories
also encounter the existing path-filter limits when automatic project context is
assembled. Those limits and harness frozen-memory policy were not relaxed to
inflate scores. Improved `PackHits` is not proof of successful natural automatic
context or native harness tool use.

## Transversal inventory

| Consumer / implementations checked | Effect and verification |
| --- | --- |
| Loom-held Local/Cloud: `brain_context.go`, `workspace_prompt.go`, `workspace_frozen_context.go`, `workspace_native.go`, `workspace_sessions.go`, `discussion/prompt.go`, native Conversation | Shared packs benefit for distinct citations; fake production dispatch observes the synthetic final-context gain. Neither assembler nor runtime contract changed. |
| Agents: Codex app-server/ACP, Claude Code ACP, Pi RPC/ACP, OpenCode HTTP/ACP, Antigravity native stream/ACP bridge, DeepSeek Harness preview, Hermes custom/SSH ACP and builtin quota-only entry, OpenClaw ACP, generic/custom ACP, remote SSH and Node transports | `acp_registry.go`, `acp_session.go`, `agent_native.go`, `agent_opencode.go`, `agent_antigravity.go`, `runtime/{codexapp,pirpc,opencodehttp,antigravity,acp}`, harness catalog, `remote_machines.go`, `node_bridge.go` checked. All consume common prepared text or explicitly configured tools; adapters remain unchanged. Harness frozen memory and unsupported previews remain unchanged. No real CLI/account/remote prompt was executed. |
| Brain tools/MCP: `brain/mcp.go`, `brain_http.go`, `mcp_gateway.go`, `mcp_gateway_harness.go`, `brain_agents.go`, `chat_tools.go` | HTTP/MCP `brain_pack` delegates to the shared pack backend and therefore retains distinct citations. `brain_search` ranks/scores are identical. Transport/auth/native memory tools and instruction registration unchanged; real external agent calls unverified. Native Codex/Pi MCP capability limits remain. |
| Jarvis: `voice_jarvis.go`, `brain/markdown_context.go` | Uses `MemoryIndex`/`FileMemoryContext`, not passage packing. Shared scoring is bit-identical; voice behavior is unaffected. Audio and live voice were not tested. |
| Brain search UI and context displays: `features/resources/{brain,memory-items,skills}.js`, `features/projects/page.js`, chat/inspector context consumers | Search, snippets, highlights and schemas unchanged. Context items already preserve one chunk/citation pair per packed unit. No JS/CSS/tokens changed; phone/browser acceptance blocked as described below. |
| Stores/indexes: `brain/{engine,search,text,semantic,markdown_memory,markdown_context,memory,memory_store,scope}.go`; root Brain sources, items, transcripts, project scope, semantic indexing, consolidation/distillation, storage/encryption | No ingestion, chunk ID, vector checkpoint, persistence or migration changes. Same source/path/personal authorization and current Markdown store. Legacy memory scoring remains separate; its TF-IDF is not a third BM25 formula site. |
| Engines: llama.cpp/router, vLLM, linked inference and Node; configured Chat Completions | Use existing local/common or cloud preparation; fake adapters capture results. No engine lifecycle, credentials, model calls or global native permissions changed. |

Changed production files: only `brain/search.go`, `brain/markdown_context.go`
and the small pure `brain/scoring.go` in the existing package. Tests/evaluation,
synthetic baseline and documentation also changed. No adapter, store, UI page or
runtime contract was changed.

## Proof and validation

- BM25 is extracted first with unchanged `k1=1.2`, `b=0.75` and floating-point
  operation order. `TestBM25Compatibility` checks exact historical arithmetic.
  Ordered full-hit JSON SHA-256 hashes (including scores/snippets/highlights)
  match before/after across all 32 cases and all budgets; both stages' category
  summaries also match. Reports now include `ranking_sha256` for repeatable checks.
- `TestPackRetainsDistinctProvenance` failed before the fix with one chunk/citation
  instead of two. It now passes and also checks repeated-hit deduplication and
  narrower path authorization. `TestPackRetainsDistinctHeadingProvenance` failed
  before retaining separate heading ancestry and now passes.
- `TestRetrievalGatePackingAndTopK` failed before gate changes for packed loss,
  span Recall@1 loss, session Recall@10 loss and per-category packed loss. It now
  passes. The committed synthetic baseline retains its prediction records and
  explicitly freezes all span/session/complete-evidence top-k and packed quality.
- `GOTOOLCHAIN=go1.26.9`: `gofmt`, `go vet ./...`, complete Brain package tests,
  both benchmark stages/corpora, importer tests, `make build`,
  `GOOS=windows go build ./cmd/loom`, and `make check-ui` (28 tests) passed.
- `go test ./...` was attempted with a temporary HOME. Socket denial stops
  `internal/loom.TestHandleHubSearch` and
  `internal/loom/platform.TestFetchReleaseChannels` at `httptest.NewServer`:
  `listen tcp6 [::1]:0: socket: operation not permitted`. Other package results
  passed, including Brain. Full root/platform success is unverified.
- After `make build`, `node tools/tests/ui-routes/crawl.mjs bin/loom` was attempted.
  Playwright was missing. `npm ci --offline --prefix tools/tests/ui-routes` failed
  with `ENOTCACHED` for `playwright-core-1.64.0.tgz`; dataset-only network scope
  was respected. Route and 390/320px browser behavior remain unverified.

All executions used temporary HOME/Loom data; the real Pi, Hermes, DeepSeek,
IA and Loom Node state were not used. No commit was created; `COMMIT_MSG.txt`
contains the review summary, root cause, inventory and validation.
