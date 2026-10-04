# Loom roadmap

Implementation inventory as of 2026-10-04. Changes after v0.2.3 are unreleased;
implemented code and synthetic regression checks are distinct from real-device,
hardware and provider acceptance. Read the [product vision](VISION.md) first.

**Loom controls; engines and harnesses execute.** Discussions and project context
belong to Loom across Local, Cloud and Harness turns. Native private state stays
with its executor. Sources remain truth; indexes select context.

## Delivered foundation

| Area | Current contract |
| --- | --- |
| Discussions | One portable transcript across executors, route identity separate from model, display journal and per-turn provenance, native local rich conversation pipeline, explicit external-sharing confirmation and revision checks. |
| Engines | External llama.cpp router; engine/model states separate. vLLM on supported Linux GPUs, direct compatible servers, full remote Loom or headless engine node. Lifecycle, library, parameters and inference stay with the engine/node. See [engines](engines.md) and [engine node](engine-node.md). |
| Harnesses | Codex, Claude Code, Pi, OpenCode, Hermes, user-defined ACP adapters and Antigravity's native CLI bridge; local or supported SSH machines, native model catalogs, tools/permissions and Native/Loom model sources only where supported. Explicit account login uses browser links/codes where available with a terminal fallback. |
| Workspaces | Saved folders and machine-specific defaults, project folders/extra directories, explicit context files and native filesystem-policy capabilities. Loom does not invent a universal sandbox. |
| Brain | Multiple canonical local/mounted/Git second brains, one writable primary, incremental BM25, optional semantic retrieval and explicit distillation with provenance; authenticated HTTP and read/write-primary MCP. See [Brain](brain.md). |
| Observation | Visible automatic Usage/quota refresh, nullable unknown data, declared/estimated cost distinction; Bench local/cloud and supported native model-only accounts with tools suppressed, unsupported adapters disabled. |
| Infrastructure | Local/SSH terminals, project previews, machine connections, declared service reachability, opt-in Docker and read-only Proxmox observation. |
| Product | English/French UI, responsive shell/settings/Bench/Usage, first-run guide, browser password sessions, separate API/node credentials, vault, installers, updates and public release CI. |

## Current unreleased consolidation

Implemented, with synthetic Go/UI/browser checks:

- Serialize visible status, hardware and activity observations; bound stalled
  JSON bodies; cancel stale parameter reads and avoid unchanged-state rerenders.
- Portal anchored menus outside transformed parents and dismiss on completed
  clicks. Keep actions accessible at narrow viewport sizes.
- Prepare context and remote workdirs outside the global session mutex, then
  revalidate the session, credentials, limits and request ID before execution.
- Present Brain as second brains, Skills and MCP while keeping native Loom
  retrieval/memory layers automatic. Support local, mounted and managed Git
  sources, one proactive writable primary and bounded write tools.
- Keep project setup to title, machine, workspace and default executor/model.
  Inherit the machine's default workspace when none is selected.
- Scope project conversation retrieval to its assigned discussions, including semantic
  search and direct passage reads. Retrieve against the current draft, once on
  the native local path, with bounded semantic-to-lexical fallback.
- Review, edit/keep or reject newly distilled candidates before retrieval.
  Preserve legacy accepted items. Show source freshness and refresh errors.
- Offer a multilingual local embedding model and opt-in incremental semantic
  refresh using the existing resumable indexer. No implicit model download.
- Preserve native-import provenance, deduplicate by runtime/machine/session and
  default the UI to a fresh native execution after import/project attachment.

## Acceptance and remaining priorities

1. **Real mobile and slow-network acceptance.** Confirm the reported blocked
   clicks on physical Brave mobile, long/large discussions and remote machines.
   Viewport/synthetic touch tests do not establish real-device acceptance.
2. **Continuity dogfooding.** Verify first-turn context after Local/Cloud/Harness
   switches and real ACP imports on local/SSH hosts. Native histories require
   advertised session-list/load support; arbitrary file parsers and consumer
   web-account conversation import remain unsupported.
3. **Semantic hardware/provider acceptance.** Validate the new multilingual
   GGUF on the installed llama.cpp version and realistic multilingual corpora.
   Cloud automatic indexing requires explicit destination consent and budget
   awareness; synthetic provider tests do not prove provider compatibility.
4. **Maintenance and security.** Continue reviewing lock boundaries, native
   configuration RPCs, background tasks, path scopes and secret handling. The
   small pure `project` package owns continuity validation/text; runtime/storage
   orchestration remains in Loom. Extract additional domains only with a real
   boundary, tests and reduced coupling.
5. **Platform coverage.** Exercise release/install/update paths on Linux,
   macOS and Windows; Windows ConPTY and SSH need native platform acceptance.
   Extend engine/provider capabilities only with evidence from their APIs.

## Later

Graphify/graph indexes, Jev/context scheduling and advanced automatic routing
follow clean state, predictable retrieval and stable daily use. They must not
replace canonical sources or conceal unsupported capabilities. Automatic memory
rewrites, destructive transcript migration and account scraping are not part of
this consolidation.
