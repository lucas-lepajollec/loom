# Free Brain retrieval benchmark

This implements the offline test commands specified by the
[reuse audit §5.4–5.6](audits/2026-10-reuse-audit.md#54-benchmark-dataset-and-fairness).
It calls production Brain ingestion, search, packing and discussion preparation,
then captures the messages passed through production dispatch to a fake adapter.
There are zero completions, embeddings, extraction, consolidation or judge calls.
No engine, harness, account, model weights or Python dependencies are required.
Brain runtime behavior and the UI are unchanged. There is no `loom brain bench`
flag: the audit's `go test` commands are the supported entry points.

Reuse decision: use the [official LongMemEval input/evidence schema](https://github.com/xiaowu0162/LongMemEval#-dataset-format)
and prediction conventions. Its QA judge is excluded, and the
[Mem0 benchmark runner](https://github.com/mem0ai/memory-benchmarks) is not adopted:
predict-only still has model-backed ingestion paths. Direct production Loom
retrieval plus shared test measurement helpers supplies the needed evidence
checks without a model service or additional dependencies.

## Run the committed offline suite

From the repository root:

```sh
GOTOOLCHAIN=go1.26.9 sh tools/brain-retrieval/run.sh
python3 -m unittest discover -s tools/brain-retrieval -p 'test_*.py'
GOTOOLCHAIN=go1.26.9 go test ./internal/loom/brain -run '^$' \
  -bench '^BenchmarkRetrievalEvidence$' -benchmem -benchtime=1s
```

The runner creates and removes a temporary HOME and Loom store, preserves Go's
explicit cache paths, and writes `evidence.json` and `final-context.json` under
gitignored `.project-local/brain-retrieval/results/`. Set
`LOOM_BRAIN_RETRIEVAL_REPORT_DIR` to choose another output directory. Each test
prints a short summary with `-v`. The root test clears credential environment
variables, isolates the runtime registry, explicitly disables Brain semantic
settings and rejects attempted HTTP/model requests through an injected transport.
The leaf evaluator has no network/model path.

The exact audit commands remain available:

```sh
LOOM_BRAIN_RETRIEVAL_MODE=lexical LOOM_BRAIN_RETRIEVAL_OUT=/tmp/evidence.json \
  GOTOOLCHAIN=go1.26.9 go test ./internal/loom/brain -run '^TestRetrievalEvidence$' -count=1 -v
LOOM_BRAIN_RETRIEVAL_MODE=lexical LOOM_BRAIN_RETRIEVAL_OUT=/tmp/final-context.json \
  GOTOOLCHAIN=go1.26.9 go test ./internal/loom -run '^TestBrainRetrievalFinalContext$' -count=1 -v
```

The tests themselves use `t.TempDir`; the shell runner also isolates process
startup. On a sandbox with a read-only default compiler cache, provide writable
`GOCACHE` and `CCACHE_DIR`, and preserve the existing `GOMODCACHE`.

## Corpus and licence decision

**The committed baseline is synthetic-only.** Eight original authored cases
cover real Markdown replacement/Refresh, historical as-of queries, missing
evidence, budget loss, duplicate text, project source scope, frozen-memory update
and explicit refresh, and precomputed-vector invalidation. `cases.jsonl` is
intentionally empty. No external conversation text or natural question IDs are
invented or committed. `temporal.jsonl` contains indexed input; `evidence.json`
holds gold supporting spans separately. Answers and gold never enter queries,
source titles, indexed documents or model-facing context.

Natural input is pinned to original **LongMemEval cleaned S**, revision
`98d7416c24c778c2fee6e6f3006e7a073259d48f`, file `longmemeval_s_cleaned.json`,
277,383,467 bytes, SHA-256
`d6f21ea9d60a0d56f34a05b609c79c88a451d2ae03597821ea3d5a9678c3a442`.
The [dataset card declares MIT](https://huggingface.co/datasets/xiaowu0162/longmemeval-cleaned/blob/98d7416c24c778c2fee6e6f3006e7a073259d48f/README.md),
and the [immutable upstream commit records the file digest](https://huggingface.co/datasets/xiaowu0162/longmemeval-cleaned/commit/98d7416c24c778c2fee6e6f3006e7a073259d48f).
The card does not supply a complete copyright/permission notice for downstream
redistribution. This PR keeps natural content in ignored local cache pending
notice and content review. Code licences from other projects are not treated as
dataset licences. The cache's `licence-manifest.json` records that declaration,
its immutable source and this redistribution decision.

The sandbox could not fetch the source because DNS was unavailable. Consequently
the committed `selected_ids`, category counts and natural converted hashes are
empty, explicitly marked pending. The importer and its selection/hash guards
are tested with synthetic upstream-shaped input; real LongMemEval retrieval
quality, download success and actual selected IDs are **unverified** here.

## Lucas: run the larger local LongMemEval subset

Only the first command downloads anything. Neither tests nor importer fetch data:

```sh
python3 tools/brain-retrieval/fetch_longmemeval.py \
  --manifest internal/loom/brain/testdata/retrieval/manifest.json \
  --cache .project-local/brain-retrieval
python3 tools/brain-retrieval/import_longmemeval.py \
  --input .project-local/brain-retrieval/longmemeval_s_cleaned.json \
  --manifest internal/loom/brain/testdata/retrieval/manifest.json \
  --output-dir .project-local/brain-retrieval/subset --select-once
```

Selection sorts SHA-256 of `loom-retrieval-v1 + question_id` within six buckets,
takes three non-abstention cases per type, three abstentions, then three disjoint
additional knowledge-update cases: 24 fixed cases. Insufficient buckets and
duplicate IDs fail. `--select-once` is permitted only for an empty selection in
ignored cache. Subsequent conversions use **those fixed IDs**, verify the source
SHA and converted case/sidecar hashes, and never reselect:

```sh
python3 tools/brain-retrieval/import_longmemeval.py \
  --input .project-local/brain-retrieval/longmemeval_s_cleaned.json \
  --manifest .project-local/brain-retrieval/subset/manifest.json \
  --output-dir .project-local/brain-retrieval/subset
LOOM_BRAIN_RETRIEVAL_CASES="$PWD/.project-local/brain-retrieval/subset" \
  GOTOOLCHAIN=go1.26.9 sh tools/brain-retrieval/run.sh
```

Every haystack session and all its user/assistant turns are ingested. Source paths
are opaque ordinal discussion paths; session IDs, `has_answer`, full supporting
turn offsets and gold session mappings stay in the sidecar. Dates are ordinary
source text. No oracle-only history, extracted answer memory or distractor
trimming is allowed. Oversized/skipped histories fail the corpus accounting.
The converter also copies the eight synthetic fixtures into the cache.

Knowledge-update cases initially have `temporal_review: pending`; `has_answer`
is insufficient to infer stale facts. Review the fixed natural IDs/histories and
add `current`, `stale`, `query_kind` (`current` or `as-of`) and query-time labels
in the local sidecar before claiming temporal quality. Span entries use
`source`, `path`, full `text`, `session`, optional `turn`, and UTF-8 byte
`start`/`end`. Recompute the sidecar file SHA in the cache manifest after an
explicit label review. Do not overwrite that reviewed sidecar by rerunning the
converter. The committed natural ID/hash manifest can be filled only after
source access and review; there is no natural baseline gate until then.

An override produces observations without comparing a different corpus to the
synthetic baseline. Reports identify corpus/fixture hashes, upstream revision,
query type/time, scope, budget, commit, lexical mode and null embedding digest.
Brain's existing source/path limits still apply during real assembly. A large
history that retrieval accepts can encounter an assembly policy limit; report
that loss rather than modifying runtime behavior to improve a score.

## Metrics and gate

JSON schema 1 contains per-case/route/budget predictions and macro/per-type
summaries. Recall denominators are null for abstention, never forced to zero:

| Metric | Measurement |
| --- | --- |
| Session Recall@1/5/10/20 | Unique gold session paths represented by hits |
| Strict evidence Recall@k | Full supporting-turn byte-span coverage by production chunks, including overlap; every non-whitespace byte required |
| Complete evidence@k | Every required span covered, independently of partial recall |
| Packed evidence | The same spans in `PackHits` at 512/1,500/3,000 approximate tokens |
| Assembled / final evidence | Actual assembled context and messages received by the fake adapter after `PrepareDiscussion` and portable window fit |
| Temporal evidence | Reviewed current/stale supporting-unit ranks and packed presence; stale transcript history distinguished from authoritative memory; as-of correctness requires complete desired support packed and ranked before contrary dated support, without stale authoritative memory |
| Frozen updates | Current/stale authoritative system memory before/after explicit context refresh, sent tokens and assembly latency |
| Abstention | Unsupported hit/packed unit counts, scope leaks and token use; no QA correctness claim |
| Tokens | `brain.Tokens` estimates for injected Brain/memory text and full prepared message texts; not billing, tokenizer output or hidden native tool prompts |
| Latency | Warm monotonic search, packing and assembly p50/p95, 30 repetitions; index/Refresh separate; allocations from Go benchmark |

Evidence is mapped to source documents, not short answer strings or snippets.
Citations alone do not count. Identical text in distinct supporting units needs
distinct retained provenance, so deduplication cannot fabricate complete evidence.
Raw Markdown headings that the index omits are not magically reconstructed as
supporting text; strict coverage may conservatively report a miss.

`baseline.json` freezes the measured deterministic counts and quality values,
with a tolerance of **0.01 absolute recall/presence**. The gate checks fixture
digests, scope leakage, each case's Recall@5 and final complete-evidence presence,
and per-type/macro quality. Missing rows/denominators fail. Timing is informational.
An existing incomplete result is not presented as successful retrieval or
automatic harness context: the baseline documents current behavior, including
duplicate packing loss and the frozen-harness boundary.

Review baseline changes explicitly: run both stages, inspect supporting units
and missing-stage reasons, then update deterministic baseline rows/macros and
fixture digest in a focused review. Tests never rewrite their own baseline and
there is no automatic acceptance flag. Regression tests also deliberately lower
Recall@5, remove final presence, and remove duplicate provenance to check that
measurement/gating detects those failures. No pre-existing runtime bug is fixed.

## Transversal inventory and coverage

| Path checked | Benchmark coverage / remaining boundary |
| --- | --- |
| Loom-held chat: `brain_context.go`, `workspace_prompt.go`, `workspace_frozen_context.go`, `discussion/prompt.go`, `workspace_native.go`, `workspace_sessions.go`, `chat_conversation.go` | Production provider/folder ingestion, project retrieval and membership scope, local Conversation capture, cloud common preparation, window fit, dispatch to fake adapter, memory update/refresh |
| Native and ACP agents: `acp_registry.go`, `acp_session.go`, `agent_native.go`, `agent_opencode.go`, `agent_antigravity.go`, `runtime/{codexapp,pirpc,opencodehttp,antigravity,acp}`, `remote_machines.go`, `node_bridge.go` | Common frozen context measured. Codex app-server/ACP, Claude Code ACP, Pi RPC/ACP, OpenCode HTTP/ACP, AGY native/fallback, Hermes, OpenClaw, DeepSeek Harness preview, generic/custom ACP and SSH/Node checked; their wire prompts and native tool-driven recall are not exercised |
| Agent Brain MCP: `brain/mcp.go`, `brain_http.go`, `mcp_gateway.go`, `mcp_gateway_harness.go`, `brain_agents.go` | Shared lexical search/pack backend measured through assembler; gateway registration, MCP transport/auth, agent requests and native instruction-file sync not benchmarked. Pi/native MCP capability limits preserved |
| Jarvis voice: `voice_jarvis.go`, `brain/markdown_context.go` | Inventory only: `jarvisMessages` rereads `MemoryIndex`/`FileMemoryContext`; voice, speech and Jarvis's own assembly are not measured by these benchmark scores |
| Brain UI: `ui/next/js/features/resources/{brain,memory-items,skills}.js`, `features/projects/page.js`, HTTP search/read/pack handlers; chat/inspector context displays | Shared lexical backend measured; source/memory/skill/project management, browser rendering and endpoint transport excluded from evidence scores; existing UI checks/crawl remain separate |
| Stores: `brain/engine.go`, `search.go`, `text.go`, `semantic.go`, `markdown_memory.go`, `markdown_context.go`, legacy memory/migration, `brain_{store,sources,items,transcripts,project_scope,semantic,consolidation,distill}.go` | Fresh temporary engine, provider conversations, actual folder Markdown, active Markdown memory and scope; vector edit/prune with synthetic vectors. Persistent migration/encryption, ingestion model extraction and semantic quality not claimed |
| Engines: llama.cpp/router, vLLM, linked inference, Node and configured Chat Completions | All use existing local/common or cloud preparation; execution replaced with fake capture. No engine launch, load, restart, inference or remote transmission |

No shared runtime contract, agent, engine, store implementation or UI page was
changed. Changes are evaluation fixtures/helpers/tests, the explicit offline
import/download tools, documentation and CI. The small `tools/brain-retrieval`
Go helper shares one evidence definition between leaf and assembly tests instead
of duplicating measurement/fixture/baseline code; the production binary never
imports it. No new runtime layer or dependency was added.
