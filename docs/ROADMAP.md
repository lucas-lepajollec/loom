# Loom roadmap

Implementation inventory as of 2026-10-02. Delivered code is distinguished
from remaining platform, hardware and provider acceptance checks. Public
workflows must work independently of the maintainer’s own setup.

**Principle:** Loom is the control post, not the engine. It never reimplements
llama.cpp, a provider or a harness; it drives them, shows their real state and
keeps the user's shared resources (conversations, projects, skills, MCP,
providers, context) in one place.

```
Discussion  →  execution target  →  Local (llama.cpp) | Cloud (APIs) | Harness (Codex, Claude Code, Pi…)
                                                                  └─ a harness may itself use a Local or Cloud model
Brain / Context Service  →  what each execution needs to know, within a token budget
```

## Done

| Area | What works |
| --- | --- |
| Local | llama.cpp router mode (engine and model states separate, several models, slots/parallel requests, API variants for client parameters), `--fit` auto config, presets and bare models, side panel Essential/Advanced/Expert with VRAM estimate, VRAM + RAM, Hugging Face hub, engine & API page. |
| Common core | One discussion across Local/Cloud/Harness, unified event stream, finished turns folded under one "work" dropdown, Ctrl K palette, activity center. |
| Cloud | 23 catalog presets (including self-hosted/custom endpoints and protocol-specific entries; account/model compatibility requires validation), model visibility, Markdown rendering, cost estimate, LAN-only plain HTTP. |
| Harnesses | ACP client: Codex, Claude Code, Pi, OpenCode, Hermes (+ user-defined harnesses); remote machines over SSH (Loom key, copy-paste setup block that describes the machine, harnesses detected there such as Hermes, grouped by machine in the selector, older ACP model API supported); real models via a no-prompt probe; tools, diffs, plans, permission requests (ask/edits/full), work folder, changed files, context and cost, agent modes and settings; import of the harness's own sessions and native resume, other discussions with the harness in the side panel; two-level `/` menu (command choices, harness settings, sessions); machine state (version, account, MCP, plugins, skills, API keys present) and update. Antigravity uses Loom’s ACP bridge to its native CLI. OpenCode and Hermes have built-in ACP entries, locally or on SSH machines. |
| Resources | Skills (distributed to `~/.claude/skills` and `~/.agents/skills`, `loom-*` only), MCP servers passed per harness session, memory pages of the local agent. |
| Observe | Usage per model/harness (tokens, estimated or declared cost, Codex/Antigravity quotas), Bench local + cloud + guarded native Claude accounts (other adapters explicitly unsupported for model-only tests). |
| Product | Mono design, classic UI removed, settings (vault, snapshots, push, GPU), leaf package split done (`store`, `platform`, `engine/llamacpp`, runtimes, `discussion`, `tools`, `resources`, `web`; application orchestration stays in Loom). |

## Milestone: daily use from a server

Loom runs on a machine without a GPU and is used from other devices; it must
drive engines and harnesses wherever they run.

- Done: server mode (interface on the network, always behind a control key),
  `/v1` on the network, remote harness machines over SSH, Loom models in
  Codex and Claude Code (local + compatible cloud providers), projects linked
  to their folder.
- Also done: remote engine (a Loom on the GPU machine linked by address and
  control key; every engine route forwarded, completions through its /v1),
  engine auto-update (applied only when no model is loaded), terminals.
- Also done: harness install/update/auto-update on this machine or a connected
  one (version vs latest published, official installers, never during a
  discussion of that harness).
- Also done: Settings › Machines, one page per machine (engine, model folders,
  harnesses, work folders, terminals); connecting a machine lives there.
- To do: install Loom itself on a remote machine over SSH, configure Hermes
  model sources through its machine’s environment with consent, and complete
  real-provider checks. Pi and OpenCode local/cloud sources are implemented.

## Next, in order

1. **Package split — done.** Leaf packages extracted through `web/`: generic HTTP/auth, public assets and SSE/WebSocket helpers; state-bound handlers, startup, persistence and execution orchestration remain in Loom through compatibility wrappers. No visible/API behavior change. See architecture §4.
2. **Resources ↔ harness bindings.** Done: per-harness choice of skills and MCP servers, adopting a harness's MCP servers, provider keys in the OS keychain. Skills are folders (Agent Skills format) in Loom's folder plus linked read-only folders, given to harnesses as links. MCP servers live in an editable `mcp.json` (standard format), and other tools' MCP files can be linked read-only and their servers adopted.
3. **Harness model source: Native / Loom.** Each harness speaks one API format; Loom offers the sources that serve it. Done: Codex (Responses) and Claude Code (Anthropic Messages) with Loom's local models (llama.cpp serves both through Loom's API) and compatible cloud providers, passed in the launch environment only; Pi local models through its own file (opt-in). Pi and OpenCode (now an ACP harness) list Loom's local models and connected cloud providers in their own model list, keys passed only at launch (Pi's file holds `$VAR` references, OpenCode gets OPENCODE_CONFIG_CONTENT); Codex receives the real context size of local models. Remaining: Hermes (only through `custom_providers` in its own config on its machine, plus Loom's /v1 reachable on the network: needs the user's consent), live checks with each cloud provider. Antigravity retains its native model catalog.
4. **Projects as link objects.** Done: a project links its folder (Git branch, remote without credentials, changes, last commit), context files chosen in that folder (re-read each message, 8 files / 48 KB, nothing outside the folder), the default work folder of harnesses and the execution a new discussion starts with. A project's folder can live on a connected machine (its harnesses there work in it, terminals open there), with extra folders of the same machine passed to harnesses. Context files are read only for folders of this machine.
5. **Brain V1 + Context Service.** Backend delivered: sources (context repository, personal notes, conversations, repositories), incremental local search, context packs with an explicit token budget, personal sources read only with explicit IDs and opt-in; exposed over HTTP and as a read-only MCP server so harnesses use the same context with Loom's browser closed (the Loom process remains running). The Resources › Brain UI and opt-in project retrieval are implemented; projects choose sources and a token budget. See [`brain.md`](brain.md).
6. **Environment.** Done: Environment page with declared services and a reachability matrix (from Loom and from every connected machine, where harnesses run), Docker containers per machine (opt-in, over SSH for remote ones), Proxmox nodes/VMs/containers through a read-only API token kept in the OS keychain, with certificate fingerprint detection and explicit pinning. Remaining: more providers (Kubernetes, systemd services), services discovered automatically from Docker/Proxmox.
7. **Local engines: llama.cpp, vLLM, more later.** Done: llama.cpp installed, linked, updated (auto-update when no model is loaded); vLLM installed by Loom in its own Python environment (uv when present), started with a Hugging Face model (reasoning parser per model family) and used as engine; any inference server (llama-server, vLLM, OpenAI-compatible) on another machine linked directly by address, without Loom there; or the engine of another Loom. Backend done for vLLM: shared ParamSpec catalog, validated per-model saved settings and launch overrides, Hugging Face cache library/search/prefetch with progress/cancel/resume/delete, installed version and idle-only manual/opt-in automatic upgrades. Local vLLM requires Linux and a supported NVIDIA CUDA or AMD ROCm GPU; Loom remains available on Linux/macOS/Windows and can link a remote engine. See [`engines.md`](engines.md). Engine choice at first install is in the step 8 guide. UI done in Settings › Engines: per-model settings, Hugging Face library (search with VRAM estimate, download with progress, start, delete), version, update and auto-update. Remaining: more engines.
8. **Public release readiness.** English + French UI (done; see [`i18n.md`](i18n.md)), first-run guide (done: language, detected hardware, engine choice — install llama.cpp or vLLM (the guide currently offers vLLM on Linux/NVIDIA; supported AMD/ROCm setups use Settings › Engines), link an engine by address, or none — a first model sized to the GPU, harnesses/cloud/machines; shown once on a fresh install, reopened from Settings › About), tested installers for Linux/macOS/Windows, demo, landing page and documentation. Public documentation and release workflow are maintained; release publication and real-platform installer acceptance remain maintainer checks.
9. **Advanced memory.** Backend delivered: optional local GGUF semantic index with a separate CPU embedding server, resumable float32 checkpoints and BM25/cosine reciprocal rank fusion; explicitly selected connected embedding providers require stored consent. On-request discussion distillation stores decisions/facts/todos/preferences with validated discussion/message provenance as the read-only, individually deletable `distilled` Brain source. Authenticated HTTP controls and hybrid MCP search/packs share Brain access rules. See [`brain.md`](brain.md). Done in the UI (Resources › Brain): enable search by meaning, download the model with progress, compute the index, choose a cloud provider with consent; distill one conversation or everything since a date, filter by kind, jump to the source message, forget an item. Verified with nomic-embed-text and a local Gemma model. Remaining: a graph index (Graphify) as one more index, never a source of truth.
10. **Terminals.** Done on Linux/macOS and Windows 10 1809+ (ConPTY): real shells (PTY over WebSocket, one-time tickets behind the control key) on this machine or a connected one, in a folder, optionally running a command (an agent's CLI, an app); they outlive the tab and replay recent output. Opened from Terminals, a project, a harness discussion or a remote machine. Windows OpenSSH targets use interactive PowerShell and literal working-folder paths. See [`terminals.md`](terminals.md). Harness discussions resume their native session in a terminal (claude --resume, codex resume, opencode --session, pi --session, hermes --resume) on the harness's machine. Remaining: native ConPTY tests on a Windows machine.
11. **Quotas everywhere, then routing.** Done for harnesses: subscription quotas (Codex, Claude Code, Antigravity, Hermes local or on a connected machine) and native usage of every harness inside and outside Loom (Claude Code/Codex/Pi logs, OpenCode/Hermes statistics) over 7 or 30 days, on the Usage page. Done for cloud balances in the Usage page and APIs: connected OpenRouter, DeepSeek, Moonshot/Kimi and SiliconFlow accounts through read-only, control-key protected endpoints, with concurrent ten-second reads, five-minute session caching, nullable amounts and sanitized errors; other providers explicitly lack the capability. OpenRouter account credits can require a management key; key limits remain available independently. See [`usage.md`](usage.md). Remaining: automatic routing (model/harness/skills per task) last, on top of clean data.

Later candidates: native cloud protocols (Anthropic Messages, Responses API), harness cron/terminal views where the harness exposes them.
