# Loom roadmap

State as of 2026-10-02. Loom is a public product: everything below must work
for any user on consumer hardware, not only for its author's setup.

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
| Cloud | 23 known providers (logos, verified endpoints), model visibility, Markdown rendering, cost estimate, LAN-only plain HTTP. |
| Harnesses | ACP client: Codex, Claude Code, Pi, Gemini (+ user-defined harnesses); remote machines over SSH (Loom key, copy-paste setup block that describes the machine, harnesses detected there such as Hermes, grouped by machine in the selector, older ACP model API supported); real models via a no-prompt probe; tools, diffs, plans, permission requests (ask/edits/full), work folder, changed files, context and cost, agent modes and settings; import of the harness's own sessions and native resume, other discussions with the harness in the side panel; two-level `/` menu (command choices, harness settings, sessions); machine state (version, account, MCP, plugins, skills, API keys present) and update. Antigravity keeps its native adapter. |
| Resources | Skills (distributed to `~/.claude/skills` and `~/.agents/skills`, `loom-*` only), MCP servers passed per harness session, memory pages of the local agent. |
| Observe | Usage per model/harness (tokens, estimated or declared cost, Codex/Antigravity quotas), Bench local + cloud. |
| Product | Mono design, classic UI removed, settings (vault, snapshots, push, GPU), package split started (`store`, `platform`, engine in progress). |

## Milestone: daily use from a server (author's target: a Proxmox dev VM)

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
- To do, in order: merge `refactor/runtime-registry` into `main` and install on the
  VM, installing Loom on a remote machine over SSH, remaining harness sources
  (Pi and OpenCode cloud, Hermes through its machine's environment).

## Next, in order

1. **Finish the package split** (engine, runtimes, discussion, web) — no visible change, keeps the code maintainable.
2. **Resources ↔ harness bindings.** Choose per harness which Loom skills and MCP servers it receives; adopt a harness's own MCP servers into Loom; credentials as references (OS keychain), never stored in clear.
3. **Harness model source: Native / Loom.** Each harness speaks one API format; Loom offers the sources that serve it. Done: Codex (Responses) and Claude Code (Anthropic Messages) with Loom's local models (llama.cpp serves both through Loom's API) and compatible cloud providers, passed in the launch environment only; Pi local models through its own file (opt-in). Remaining: Pi and OpenCode with cloud providers, Hermes (its machine must reach Loom's API: server mode), live check with each cloud provider, real context size of a Loom model in Codex. Gemini CLI only accepts Google models.
4. **Projects as link objects.** Done: a project links its folder (Git branch, remote without credentials, changes, last commit), context files chosen in that folder (re-read each message, 8 files / 48 KB, nothing outside the folder), the default work folder of local harnesses and the execution a new discussion starts with. Remaining: several folders/repositories per project, remote-machine folders.
5. **Brain V1 + Context Service.** Sources (context repository, personal notes, conversations, repositories), search, context packs with an explicit token budget, personal sources read only on request; exposed over HTTP and as an MCP server so harnesses use the same context without Loom open.
6. **Environment.** Machines, services and connections (declared + observed through optional providers: Docker, Proxmox…), and which harness can reach what.
7. **Local engines: llama.cpp, vLLM, more later.** At first install the user chooses to install one or several engines and link them, or to link engines already installed, among those Loom supports. Loom only links: each engine keeps working on its own as if Loom were not there. The engine can run on another machine (GPU box) while Loom runs elsewhere. Engines differ (vLLM targets high-throughput parallel serving, llama.cpp consumer GPUs and GGUF), so the Local page and the parameters panel adapt to each engine's capabilities and parameter catalog (Engine interface + ParamSpec per engine).
8. **Public release readiness.** English + French UI, first-run onboarding (hardware detection, engine install, first model, providers, harnesses), tested installers for Linux/macOS/Windows, demo, landing page and documentation. Repository hygiene pass done (docs match the code, release workflow fixed; open decisions in `docs/agents/repo-hygiene-report.md`).
9. **Advanced memory.** Distillation of conversations into decisions/facts with provenance, semantic search, then a graph index (Graphify) as one more index, never a source of truth.
10. **Terminals.** Done on Linux/macOS: real shells (PTY over WebSocket, one-time tickets behind the control key) on this machine or a connected one, in a folder, optionally running a command (an agent's CLI, an app); they outlive the tab and replay recent output. Opened from Terminaux, a project, a harness discussion or a remote machine. Remaining: Windows (ConPTY), terminals tied to harness sessions' native resume.
11. **Quotas everywhere, then routing.** Usage/quotas for every harness and provider, then automatic routing (model/harness/skills per task) last, on top of clean data.

Later candidates: native cloud protocols (Anthropic Messages, Responses API), harness cron/terminal views where the harness exposes them.
