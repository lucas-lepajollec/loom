# Loom direction

Status: accepted direction, 2026-10-07. It orders future work; it does not claim
anything below is implemented. Read [VISION.md](VISION.md) for the current product
contract and [ROADMAP.md](ROADMAP.md) for delivered work.

Loom is a local-first **control plane** for personal AI. It owns identity,
sessions, context, knowledge, policies and orchestration. Models, engines,
harnesses and external services **provide capabilities**; machines run them.
Interfaces (web chat, phone, voice) are clients of the same control plane. None
of them is a second assistant with its own memory, tools or routing.

The bet: AI products, models and runtimes will keep changing. What lasts is the
user's continuity — context, knowledge, decisions, methods — and one place that
makes every capability work together. Loom must survive the replacement of any
model, harness or runtime it drives.

## Target vocabulary

These names are the contract for new code and UI copy.

| Concept | Meaning | Today |
| --- | --- | --- |
| **Machine** | A host Loom knows (identity, OS, hardware, reachability). May exist with SSH only. | `RemoteMachine` |
| **Node** | The Loom daemon installed on a machine. One program with modules (`engine`, `harness`, `observe`, `voice`, …) advertised as versioned capabilities. Never separate per-feature daemons. | engine-only `loom node` |
| **Provider** | Software that supplies capabilities: llama.cpp, vLLM, a cloud API, Codex, Claude Code, Home Assistant, an STT or TTS engine. Not every provider is a `RuntimeAdapter`. | runtimes, providers |
| **Capability** | A first-class operation Loom can discover, authorize and invoke. A tool shown to a model is a *projection* of an authorized capability, not its definition. | descriptor strings, chat tools |
| **Surface / session source** | Where a harness family is used (CLI, desktop, IDE, cloud) and where its sessions live. One identity per external session, whatever surface shows it. | per-machine ACP import |
| **Session** | Loom-owned interaction state; portable history stays executor-independent. | `RuntimeSession` |
| **Task** | Durable asynchronous work with lifecycle `queued → running → waiting_input / waiting_approval → done / failed / cancelled`, progress, checkpoints and results. | — |
| **InteractionRequest** | A question, approval, choice or login a running executor needs from the user. Answerable from chat, a task view, a notification or voice. | ACP approvals in chat only |
| **Event** | A domain event (`task.completed`, `node.offline`, `service.down`). Distinct from `DiscussionEvent`, which renders a turn. | — |
| **Automation** | Trigger + condition + action over events and capabilities. | — |
| **Policy** | Central `allow / confirm / deny` decision for capability use, cost and data sharing. | scattered checks |
| **Orchestrator** | Decides whether an intent becomes a direct capability call, a model request or a delegated task. | direct routing |
| **Brain** | Knowledge and memory: the user's vault plus Loom's cognitive memory and the context engine. *Not* the orchestrator. | second brains, distillation |

Rule for every refactor: **a new kind of capability must become possible without
editing switch statements across the codebase.** Registering a provider,
advertising node modules and declaring capabilities is the extension path.

## Brain

The Brain is Loom's most important asset. Requirements: the user's knowledge is
never locked in Loom, the structure the user chose is kept, and the model gets
the smallest context that is sufficient — quality per token, not volume.

### One vault, adopted in place

- A Brain is **a folder** the user owns (Markdown/Obsidian vault or a new one
  Loom creates). Loom adopts it as is; it does not impose a taxonomy or move
  notes. Other tools (Obsidian, other LLMs) keep working on the same folder.
- Loom's additions live in **`.loom/` inside the vault**: `brain.yaml` (format
  version), a **Brain profile** (what the user's folders, tags and frontmatter
  mean, which paths are read-only/append-only/writable), Loom memory items,
  relations and reflexes — plain, documented files.
- Indexes (BM25, vectors, a future graph) are derived and rebuildable; they may
  live outside the vault. Deleting them loses nothing.
- Backup, archive, move or share = copy the folder. A fresh Loom pointed at it
  rebuilds its indexes. A remote Brain is a mounted folder first; a dedicated
  protocol only if a filesystem mount proves insufficient.
- Formats Loom cannot adopt in place (exports from closed apps) are **converted
  once** into a Brain folder, with an import report and zero discarded content.
  Loom never maintains two brains in parallel.
- The Brain profile is bootstrapped by a model the user chooses, from a prompt
  Loom provides, and corrected in natural language. It changes mappings and
  metadata, never the user's file layout.
- Loom is the single writer of `.loom/`; agents write through Brain operations
  (`remember`, `update`, `forget`), not by editing files directly.

### Cognitive memory

Memory items share one model with attributes — `class`, `scope`, `importance`,
`confidence`, `created/last_used`, `provenance`, `supersedes`, `status`
(`active / superseded / uncertain / expired`) — instead of separate stores:

| Class | Holds | Lifetime |
| --- | --- | --- |
| Working | current objective, plan, state, open issues of a task | task |
| Session | what happened in the current discussion | session |
| Episodic | dated events and decisions, the project trajectory | weeks–months |
| Semantic | durable facts and preferences, current value + history | long |
| Procedural | how the user/Loom does things (release steps, review method) | long |
| Reflex | a few hundred tokens of near-always rules | permanent, rare promotion |

Scopes (`global`, `project:…`, `machine:…`, `agent:…`, `task:…`) are filtered
before any semantic search. Every item answers "why does Loom believe this?"
through provenance. Contradictions supersede rather than delete.

Consolidation is cheap by default: heuristics collect candidates during turns;
promotion runs at session end, task end, before compaction or when idle, with a
small/local model for routine work and a strong model only for conflicts.
Candidate review stays available (already implemented for distillation).

### Context engine

`intent → context plan → scoped candidates (metadata, BM25, then vectors, then
optional reranker) → dedupe + conflict resolution → per-model token budget →
context pack`. Budgets are explicit per class (reflex, working, semantic,
episodic, procedural, vault documents, recent turns). The existing Context panel
grows into **"why is this in context"**: every included item, its class, source
and token cost. A deterministic baseline must work without any routing model.

## Interface architecture

The UI is organised by the user's questions, not by internal components:

| Page | Question | Absorbs |
| --- | --- | --- |
| **Chat** | What am I talking to now? | — |
| **Projects** | What am I working on? | project settings |
| **Models** | What can think? | Local, Cloud, engine libraries (llama.cpp, vLLM, linked servers) |
| **Agents** | What can act for me? | Harnesses, installations per machine, surfaces, sessions/import |
| **Machines** | Where does it run? | Settings › Machines, Terminals, Environment, node maintenance, engine location |
| **Brain** | What does Loom know? | second brains, memory, skills, MCP |
| **Tasks** | What is working right now? | (with phase 4) |
| Activity | secondary group | Usage, Bench |
| **Settings** | How is Loom itself configured? | general, security, updates, diagnostics only |

A machine page shows overview, capabilities, agents installed there, terminals,
services, storage/workspaces and its node. Models and Agents link to the machine
that hosts them; Machines link back. Two views of the same objects, one
configuration.

**Side panel rule:** it contains only properties that change the object or the
execution currently shown (model parameters, agent mode, workdir, permissions,
project context). Installing, updating or system configuration never lives there.

## Maintenance and supportability

- Adapters are **capability-probed and degradable**: an external update that
  breaks one feature disables that feature, not the harness or Loom.
- A **compatibility catalog** (detection rules, known versions, broken versions,
  install metadata) can be refreshed without a Loom release. It is signed data,
  never downloaded executable code.
- **Loom Doctor**: core, machines, integrations, capabilities and security checks,
  plus opt-in acceptance tests on the user's real hardware and accounts, and a
  **redacted diagnostic bundle** (versions, failed probes, sanitized config and
  protocol traces; no secrets, prompts or Brain content) for bug reports.
- Release cadence: features are batched; a patch release ships only for a
  blocking regression. Day-to-day testing uses a preview channel or `make dev`,
  not public releases.

## Voice and Jarvis

Voice is an interface to the same control plane: browser voice mode and a
**voice module of the Loom Node** feed the same sessions, memory, policies and
orchestrator as text. The voice module only listens, speaks, detects turns,
handles barge-in and streams to and from Loom; it knows nothing about Home
Assistant, agents or schedules.

- Build order: browser voice mode (no wake word; reuses everything) → node voice
  module on a Linux machine with a USB speakerphone (native audio, systemd, no
  container for audio) → wake by transcript ("Jarvis" fuzzy-matched by local STT
  on VAD-gated speech, plus a physical button fallback) → presence-aware
  speaking and automations.
- STT/TTS/VAD/turn detection are swappable providers chosen by benchmarks on the
  user's hardware, not fixed in the design.
- Home Assistant is a provider: its entities and services are discovered as
  capabilities and called directly — no generated code is needed for standard
  device control. Agent-built capabilities come much later, sandboxed, tested,
  policy-reviewed and recorded with their author; an agent never grants itself
  unrestricted authority.
- A fast path (deterministic or small-model intent → direct capability) answers
  simple commands without a frontier model.

## Phases

Each phase ends with real-use acceptance, not only synthetic tests.

0. **Daily use on a server VM.** Fix what blocks moving the user's environment to
   a headless Loom: sending, long threads, remote harness work, imports, updates.
1. **Information architecture.** Models / Agents / Machines pages, slimmer
   Settings, side panel rule. Move UI, keep APIs.
2. **Brain v2.** `.loom/` sidecar and Brain profile, memory items as files with
   classes and scopes, context engine budgets and the "why in context" view,
   export/restore acceptance test (create → export → fresh Loom → identical
   canonical Brain).
3. **Machines and universal Node.** Machine/Node split, versioned node handshake
   with modules; `engine` stays first and keeps working; then `harness` and
   `observe` modules.
4. **Tasks, InteractionRequest, Events, Policy.** Harness questions and approvals
   become task interactions; first automation: `task.completed → notify`.
5. **Capabilities and orchestration.** Capability registry with tools as
   projections; task classifier (e.g. a small decision model) producing a task
   profile matched against model/agent capability profiles, with manual override;
   then delegation, role-based agent teams and bounded long-running loops
   (max iterations/cost/deadline, checkpoints, resume).
6. **Voice and Jarvis**, as described above.

Graph indexes, automatic memory rewrites and agent-generated capabilities stay out
of scope until phases 2–5 are stable.
