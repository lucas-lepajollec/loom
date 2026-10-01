# Loom roadmap

State as of 2026-10-01. Loom is a public product: everything below must work
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
| Harnesses | ACP client: Codex, Claude Code, Pi, Gemini (+ user-defined and remote harnesses); real models via a no-prompt probe; tools, diffs, plans, permission requests (ask/edits/full), work folder, changed files, context and cost, agent modes and settings; import of the harness's own sessions and native resume; `/` commands; machine state (version, account, MCP, plugins, skills, API keys present) and update. Antigravity keeps its native adapter. |
| Resources | Skills (distributed to `~/.claude/skills` and `~/.agents/skills`, `loom-*` only), MCP servers passed per harness session, memory pages of the local agent. |
| Observe | Usage per model/harness (tokens, estimated or declared cost, Codex/Antigravity quotas), Bench local + cloud. |
| Product | Mono design, classic UI removed, settings (vault, snapshots, push, GPU), package split started (`store`, `platform`, engine in progress). |

## Next, in order

1. **Finish the package split** (engine, runtimes, discussion, web) — no visible change, keeps the code maintainable.
2. **Resources ↔ harness bindings.** Choose per harness which Loom skills and MCP servers it receives; adopt a harness's own MCP servers into Loom; credentials as references (OS keychain), never stored in clear.
3. **Harness model source: Native / Loom / Custom.** Let open harnesses (Pi, OpenCode…) use a Loom model (local endpoint or provider), show only what each adapter really supports.
4. **Projects as link objects.** A project links its code folder/repo, context sources, default work folder and preferred executors, instead of being a separate store.
5. **Brain V1 + Context Service.** Sources (context repository, personal notes, conversations, repositories), search, context packs with an explicit token budget, personal sources read only on request; exposed over HTTP and as an MCP server so harnesses use the same context without Loom open.
6. **Environment.** Machines, services and connections (declared + observed through optional providers: Docker, Proxmox…), and which harness can reach what.
7. **Local engines: llama.cpp, vLLM, more later.** At first install the user chooses to install one or several engines and link them, or to link engines already installed, among those Loom supports. Loom only links: each engine keeps working on its own as if Loom were not there. The engine can run on another machine (GPU box) while Loom runs elsewhere. Engines differ (vLLM targets high-throughput parallel serving, llama.cpp consumer GPUs and GGUF), so the Local page and the parameters panel adapt to each engine's capabilities and parameter catalog (Engine interface + ParamSpec per engine).
8. **Public release readiness.** English + French UI, first-run onboarding (hardware detection, engine install, first model, providers, harnesses), tested installers for Linux/macOS/Windows, documentation and site, safe defaults for network exposure.
9. **Advanced memory.** Distillation of conversations into decisions/facts with provenance, semantic search, then a graph index (Graphify) as one more index, never a source of truth.
10. **Quotas everywhere, then routing.** Usage/quotas for every harness and provider, then automatic routing (model/harness/skills per task) last, on top of clean data.

Later candidates: native cloud protocols (Anthropic Messages, Responses API), harness cron/terminal views where the harness exposes them.
