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

### One primary vault, optional secondary ones

- **One primary Brain** is Loom's working memory: retrieved automatically for
  every relevant request and the only place Loom and agents write durable
  knowledge (through Brain operations).
- **Secondary brains** (reference vaults, team docs, archives) are attached with
  explicit rules: consulted only when a request needs them or when the user or an
  agent asks, never injected spontaneously; read-only by default; write/edit
  permissions granted per brain and per path. They keep their own structure and
  are never merged into the primary.

### The primary vault, adopted in place

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
| **Machines** | Where does it run? | Machines (overview and detail), Workspaces, Startup, Terminals, Environment, node maintenance |
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

Jarvis is a product feature for every Loom user, not a personal setup: it must
work with any USB/Bluetooth/built-in microphone and speaker, on any machine that
can run a Loom Node (Linux first, then macOS and Windows), in any language the
chosen speech providers support, and degrade gracefully on modest CPUs (smaller
models, push-to-talk) or use cloud speech providers with explicit consent.
Hardware-specific helpers (speakerphone echo cancellation, a device button as
push-to-talk) are optional providers, never requirements.

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
- STT/TTS/VAD/turn detection are swappable providers. Loom ships sensible
  defaults per hardware class and lets users benchmark and switch them from the
  Jarvis page; no model is fixed in the design.
- Home Assistant is a provider: its entities and services are discovered as
  capabilities and called directly — no generated code is needed for standard
  device control. Agent-built capabilities come much later, sandboxed, tested,
  policy-reviewed and recorded with their author; an agent never grants itself
  unrestricted authority.
- A fast path (deterministic or small-model intent → direct capability) answers
  simple commands without a frontier model.

## Phases

Each phase ends with real-use acceptance on a server install, not only
synthetic tests. One branch and PR per slice, green CI before merge, batched
releases.

### 0. Reliable daily use on a server VM
- Nothing can stay "running" forever: every executor turn closes its open tools
  with a reason; harness modes that cannot ask for approval say so.
- Sending, long threads and long pastes work (done in 0.2.7).
- One spacing rule for settings cards, inner notes and notes below cards; a
  full visual pass of every page on desktop and phone.
- Remote harness work and imports verified from the VM against a GPU machine.
- Session continuity across machines: continue a Claude Code or Codex session on
  another machine with full fidelity (the native session moves with it, same
  harness and account), and a portable handoff (summary + recent turns + linked
  Brain notes) when the destination harness differs or the source is huge.
  Import reads transcripts read-only even while the harness's own app keeps the
  session open.
- **Exit:** the owner works only from the server install for a week.

### 1. Interface architecture
- Pages: Chat, Projects, Models (Local + Cloud + engine libraries), Agents
  (harness families, installations per machine, sessions), Machines (overview,
  capabilities, agents, terminals, services, storage, node, maintenance), Brain;
  Usage and Bench under Activity; Settings for Loom itself.
- Side panel rule enforced; old routes redirect.
- **Exit:** no page mixes "where", "what" and "who"; every setting has one home.

### 2. Brain v2
- Primary and secondary brains with rules; `.loom/` sidecar, `brain.yaml`,
  Brain profile bootstrapped by a chosen model and editable in natural language.
- Memory items as files with classes, scopes, provenance, supersession; cheap
  consolidation; candidate review kept.
- Context engine with per-model budgets and the "why is this in context" view.
- One-time import for non-adoptable formats with a zero-loss report.
- Delivered on 2026-10-08: user profile ("You", always in context), automatic
  continuity (off by default, uses only an already-loaded local model unless a
  consented provider is chosen), skills in the primary brain, MCP gateway.
- **Automatic compaction** (accepted 2026-10-08): an opt-in setting. When it is
  off and a discussion approaches its context limit, the discussion shows a
  warning with a link to the compaction setting and two ways out: continue in a
  new discussion of the same project (continuity carries the state), or create
  a project, attach this discussion and start a new one there.
- **Exit:** export → fresh Loom → identical canonical Brain; context packs stay
  within budget with no quality loss in dogfooding.

### 3. Machines and the universal Node
- Machine/Node split; pairing by code; versioned handshake with modules;
  `engine` keeps working throughout, then `harness` and `observe` modules.
- Harness families → installations → surfaces → session sources; one identity
  per external session; import from the machine page.
- **Exit:** GPU machine = engine node, server = Loom + agents, all managed from
  Machines without SSH copy-paste.

### 3b. Engine as a service (accepted 2026-10-08)
- Clean API use of the local engine by other tools and by Loom's own features
  (continuity, consolidation, bench), with per-key limits and usage.
- Per-request model switching: when several models do not fit in VRAM, swap on
  request (queue, one resident model) instead of trying to load them together;
  `MODELS_MAX`, priorities and per-feature model choice configurable.
- Idle unload: a configurable delay after which the engine frees VRAM (today a
  model stays loaded until another one replaces it or the engine stops).
- **Exit:** a background feature never evicts the model a user is chatting
  with without a declared policy; VRAM is released when idle.

### 4. Tasks, interactions, events, policy, maintenance
- Durable tasks with checkpoints; InteractionRequest for questions, approvals
  and logins answered from chat, tasks or notifications.
- Domain event bus; first automation `task.completed → notify`.
- Central policy (allow / confirm / deny); capability probes and degraded mode;
  compatibility catalog; Loom Doctor and redacted diagnostic bundle.
- **Exit:** an agent works for an hour, asks a question, gets the answer from a
  phone notification and finishes.

### 5. Capabilities and orchestration
- Capability registry; tools as projections of authorized capabilities.
- Routing: deterministic baseline, then a small decision model producing task
  profiles matched to model/agent profiles, with manual override.
- Delegation, role-based teams shown as an execution tree (not agent chatter),
  bounded long-running loops (iterations, cost, deadline, checkpoints, resume).
- **Exit:** "do this task" picks model and agent within budget and explains why.

### 6. Voice and Jarvis
- Browser voice mode → Node `voice` module → Home Assistant provider → presence,
  quiet hours and escalation to phone, all configurable from a Jarvis page.
- **Exit:** "it lives", "it acts", "it works" scenarios on several hardware
  classes.

Graph indexes, automatic memory rewrites and agent-generated capabilities stay out
of scope until phases 2–5 are stable.
