# Changelog

Notable changes to Loom are documented here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and releases use [Semantic Versioning](https://semver.org/).

## [Unreleased]

## [0.1.1] - 2026-10-02

### Fixed

- Linux/macOS installation: `sudo loom install` and later `sudo loom …` commands created the user's data folder and `loom.db` as root, so the services (running as the user) could not use them and the web UI answered every request with 503. Data is now created for the user, and installations affected by 0.1.0 are repaired by running any `sudo loom` command (for example `sudo loom ui restart`).

## [0.1.0] - 2026-10-02

First public release.

### Added

- Shared discussions across local models, cloud providers and coding harnesses, with explicit context-sharing consent, per-response provenance, cancellation and non-destructive archive import. Local discussions retain the original Conversation pipeline, attachments, tools, reasoning, presets and compaction.
- Projects linking local or connected-machine folders, with shared instructions, selected context files, skills, MCP selections, default execution choices and opt-in Brain retrieval within a token budget. Discussion titles, project associations and additional instructions can be edited without rewriting history; a prepared-text preview includes the draft.
- Generic ACP v1 execution for Codex, Claude Code, Pi, Gemini and OpenCode, plus custom/SSH launchers including Hermes. Native tools, diffs, plans, settings, working roots, permission requests and reported usage appear in the shared journal. A deterministic development agent exercises these events without model calls.
- Antigravity through Loom's ACP bridge to its native CLI, with native models, access modes, tool observations, sessions and reported usage. Native authentication remains with the CLI; Loom supplies no provider keys or MCP servers. Codex app-server is retained for quota reads only.
- Native harness-session import and supported resumption, including **Resume in a terminal** on the harness's machine and in its working folder. Protocol-compatible Native / Loom model sources use launch configuration/environment; Pi's opt-in provider file uses environment references.
- Harness inspection and install/check/update controls for local and saved SSH machines, with bounded logs, refreshed offers/probes and opt-in idle automatic updates. Windows npm launchers and OpenSSH targets are supported. Unknown upstream versions remain unknown; Pi's installer package remains marked unverified and Antigravity updates are manual.
- Manifest-driven, opt-in distribution of owned `loom-*` skills, read-only linked skill folders and per-harness resource selections. Editable `LOOM_HOME/mcp.json` preserves extension fields, reloads editor changes and retains the last good configuration after invalid edits. Linked MCP sources can be adopted disabled, with explicit opt-in for environment values.
- Brain BM25 search over local sources, discussions and memory pages, incremental indexing, cited context packs, authenticated HTTP APIs and read-only Streamable HTTP MCP tools at `/mcp/brain`. Personal sources require explicit IDs and opt-in. See [Brain](docs/brain.md).
- Optional Brain semantic search using downloaded CPU GGUF embeddings, resumable vector checkpoints and hybrid ranking, or connected cloud embeddings with stored consent. On-request distillation stores individually deletable decisions, facts, todos and preferences with discussion/message provenance. Resources › Brain exposes source/search, indexing and distillation controls.
- Environment page and protected APIs for declared services, HTTP/TCP reachability matrices, opt-in local/SSH Docker observation and read-only Proxmox resources. Proxmox tokens use the OS keychain; certificate pinning requires explicit confirmation.
- Local and SSH terminals that replay recent output while Loom runs. Linux/macOS use PTY; Windows 10 1809+ uses native ConPTY with resizing, Unicode arguments/environment and owned process-tree cleanup. Connected Windows OpenSSH machines use interactive PowerShell. See [terminals](docs/terminals.md).
- vLLM installation in a Loom-managed Python environment on supported Linux NVIDIA CUDA / AMD ROCm hardware, with validated per-model parameters, Hugging Face safetensors library/search, advisory VRAM sizing, resumable downloads and idle upgrades. Settings › Engines exposes library, model settings, version, update and auto-update controls. See [engines](docs/engines.md).
- Direct links to running llama-server, vLLM or OpenAI-compatible servers, and remote management of another Loom's engine. llama.cpp installation, compilation, binary updates and opt-in idle automatic updates remain available.
- Usage page with retained Loom token counts, manual-price estimates, native harness activity over 7 or 30 days, and explicitly refreshed Codex/Claude Code/Antigravity/Hermes subscription quotas. Unknown metrics stay unknown and refresh never redeems resets. See [native usage](docs/harness-usage.md).
- Read-only cloud provider balances and key limits for connected OpenRouter, DeepSeek, Moonshot/Kimi and SiliconFlow accounts, displayed in Usage. Protected readers use bounded concurrent requests, five-minute session caching and sanitized errors; unsupported providers expose no balance capability. See [provider balances](docs/usage.md).
- Bench tests for local and cloud models, including local prefill/decode throughput, streamed cloud timing, reported token rates and cancellation. Cloud benchmarks require sharing consent.
- First-run guide for language, hardware, engine setup, a first llama.cpp model sized to the machine, harnesses, providers and machines; reopen it in Settings › About. English is the default UI language, with French available and saved through browser/shared preferences.
- PWA manifest, brand icons, contextual installation guidance and a public offline fallback. Conversations, credentials and API traffic are never cached; offline messages are not queued.
- Settings for vault locking/unlocking, decryption, recovery keys, additional key wraps, memory snapshots, opt-in Web Push and native GPU device selection. Optional OS keychain storage restores remembered cloud provider keys when available.
- OpenAI-compatible `/v1/models` and `/v1/chat/completions` server with native slots, live observations and request history; local GGUF library, Hugging Face downloads, saved presets and Essential/Advanced/Expert parameter controls with VRAM estimates.

### Changed

- One native ES-module UI with vendored Preact + htm and embedded fonts, directly included in the Go binary. The classic interface, `/classic` and the UI assembler are removed; `make check-ui` validates modules and regression tests.
- Common execution selector and contextual side panel, responsive dialogs/touch controls, preset ordering and contextual model drawers. Merely inspecting a model does not load it, connect a runtime or change the executor. Motion respects `prefers-reduced-motion`.
- Unified discussion SSE vocabulary, replay and one chat reducer for local/cloud/harness turns. Reconnection does not resubmit generation. Revision-checked sends/context edits and combined input bounds reject stale or oversized requests without silent truncation.
- llama.cpp router mode starts the engine once, then loads models through its API. Request-time load parameters create ephemeral variants without changing saved settings. Older engines or explicit single mode retain the legacy process-replacement path.
- Bare models use native `--fit on` when supported, leaving context/GPU layers unset unless explicitly configured. Nullable observed context per slot never loads a model or persists an advisory estimate.
- Ordered runtime registry with descriptor-driven capabilities and optional connect/quota dispatch; generic protected routes retain legacy aliases. Unsupported descriptors do not claim execution.
- Leaf packages extracted for store, platform, llama.cpp helpers, runtime protocols, discussions, tools, resources and generic web plumbing. Application state, lifecycle policy and domain handlers remain in Loom.
- Public README, contributor and architecture guidance updated for current workflows, English copy and new screenshot paths; private deployment references and obsolete screenshots removed.

### Security

- Model Markdown escapes raw HTML and neutralizes unsafe link schemes.
- Management APIs retain control-key authentication, vault-lock checks, bounded JSON and no-store responses. Network exposure requires a control key distinct from the inference key.
- ACP delegated filesystem operations enforce working-root confinement and bounded regular-file access; native commands/MCP execution retain upstream permissions. Full Loom permission mode requires explicit confirmation.
- Local storage and inference remain local by default; Loom adds no telemetry. External transcript/context sharing, cloud embeddings and remote distillation retain explicit consent requirements.

### Fixed

- Dialog placement and viewport-centered confirmations, including bounded scrolling and keyboard focus handling on small screens.
- Official llama.cpp binary selection, Linux CUDA runtime library installation and AMD-only ROCm selection.
- Historical response attribution no longer inherits the current model or fabricates metrics. Antigravity reasoning variants use discovered native IDs; end-to-end output-token rates remain distinct from llama.cpp decode throughput.
- Cloud send controls report missing credentials after disconnect/restart; offline guards preserve the current draft. Returning a shared thread to local preserves its original archive and rich pipeline.
- Snapshot restoration rejects invalid identifiers, empty snapshots and non-regular entries. Legacy plaintext control keys remain protective until hash migration succeeds.
- Occupied web ports are reported without stopping unrelated processes. Prebuilt engine extraction rejects escaping links and stale destination symlinks.
- Installer and in-app binary updates reject missing or invalid SHA-256 manifests. Windows retains only Loom's canonical installed binary.
- Release automation validates tagged source and prepares an unpublished draft; pushing a tag alone does not publish a release.
- Demo introduction starts at its heading on narrow screens. Public build instructions document direct UI embedding and the actual Makefile checks.
