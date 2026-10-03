# Changelog

Notable changes to Loom are documented here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and releases use [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- Saved workspaces and per-machine defaults in Settings, first-run setup and harness discussions; project folder precedence and explicit save/create/default actions preserve existing files and discussions.
- Explicit harness connect/disconnect, connected-first grouping and native account login terminals. Installed CLIs alone no longer populate the selector; existing harness usage migrates without account reads.
- Native filesystem policy controls for known Codex ACP launchers, separate from Loom approval automation. Unsupported strict read confinement remains disabled; selected modes are reapplied before turns and unsupported native modes fail before prompting.
- Authenticated development previews for local/SSH HTTP apps, separate browser origins, one-time bootstrap, scoped cookies, assets/WebSocket/HMR proxy and owned stream/listener cleanup. HTTPS preview origins require explicit infrastructure configuration.
- Multiple linked Brain sources for folders, Git checkouts, Obsidian and mounted WebDAV, with source-scoped search, include/exclude rules, editing and explicit personal access. Source directories remain read-only; direct remote sync is unsupported.

### Changed

- Resources navigation is now Brain, with sources/context first and memory, skills and MCP together. Existing Resources links remain supported.
- Gemini is labelled Gemini CLI, distinct from the Antigravity executor.
- Runtime adapters use a typed synchronous event sink; the portable discussion no longer imports Antigravity's event type. Architecture docs distinguish the implemented stream from future protocol sketches.

### Fixed

- Repeated terminal opens are coalesced in the UI and optionally idempotent in the API; process-limit checking is serialized. Unix SSH terminals no longer start a nested login shell, preventing duplicate startup banners.
- Control-plane HTML rejects framing by developer applications. Legacy mutation routes (engine actions, vault lock, chat reset/stop and related controls) reject GET and require origin-protected POST. Preview upstreams do not receive Loom authentication, and closing a preview stops upgraded WebSocket connections too.

## [0.1.4] - 2026-10-03

### Added

- Linux `loom node init|serve|install|update|capabilities`: engine-only control API and streaming inference proxy, separate node state, machine credentials, shared native engine handlers, optional systemd user service and connection from the main Loom. No discussion, Brain, harness or interface services run in node mode.

- User-only `install.sh --node` with checksum/feature checks, separate binary, service start, retained custom home/listener, portable mode and installation rollback. Connected nodes can update their release binary from the main interface or `node update`, independently of the main Loom.
- Native node health/slots/props/metrics forwarding and local benchmarks. vLLM inference translates Loom's model alias while retaining explicit IDs and request parameters.

### Changed

- Default interface port is 2510 for foreground, desktop and installed services. Explicit ports remain supported; adjust existing URLs, tunnels, firewalls or reverse proxies when upgrading a service using the old default. Node control/inference defaults to loopback 2511.

### Fixed

- Node and privileged updater entry points skip full-app runtime registration, avoiding harness probes and main data creation before their isolated startup. The unpublished v0.1.3 candidate failed clean-environment installation validation and was superseded.

- SSH machines can be saved and edited with zero selected harnesses; detected harnesses remain optional. A machine saved without a harness list no longer crashes the edit dialog.
- Engine node controls retain the GGUF library while vLLM is selected, stop/restart the selected owned engine, and display the protected node endpoint instead of an internal port. Node path inspection never provisions full-app workspace directories. Presets/unload retain the private front binding, and remote chat uses the selected engine’s sampling configuration instead of a stale main-machine preset.

- Unix npm harness install/update falls back to an explicit user prefix when the global prefix is not writable, without sudo or changing npm configuration. User-installed binaries are preferred for inspection and execution; SSH makes the decision on the target machine.
- Engine installers reject unwritable source/environment destinations early. vLLM discovers user-installed uv/Python from service PATHs; noninteractive system build dependency installation cannot wait on a sudo password prompt.

## [0.1.2] - 2026-10-02

### Added

- Install official Loom releases from Settings → About, with confirmation, checksum verification, a retained rollback binary and verified reconnect after the interface restart. Linux system installs provision a narrowly scoped, clean-environment updater for the service user; existing installs require one-time administrator setup.

- Single-owner interface access password with browser session cookies, Settings controls for setup/change/sign-out, and masked `loom password` CLI setup/recovery. Browser passwords use salted Argon2id; persistent sessions are hashed, expire and are revoked on password changes. Existing control keys and `/v1` inference keys remain independent. Browser keys are removed from localStorage; login and browser mutations have origin protection and password attempts are rate-limited.

### Fixed

- `install.sh` can update a running Loom: the binary is replaced by rename instead of an in-place copy (which failed with "Text file busy"), and running `loom-ui` / `loom-engine` services restart on the new version.

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
