# Changelog

Notable changes to Loom are documented here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and releases use [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- Data-driven harness install/check/update API for local and saved SSH targets, Windows npm execution without a local shell, refreshed machine offers/ACP probes, exclusive actions with bounded logs, and opt-in six-hour idle auto-updates with persisted results. Native Windows SSH probes and commands are supported; CGO-free macOS builds retain CLI/web access without the menu-bar icon; Pi's requested npm package remains marked unverified and Antigravity updates remain manual.

- The previous interface (`/classic`, `ui/src`, `tools/assemble-ui`) is removed; `make check-ui` replaces the assembled-UI check in CI.
- Generic ACP v1 harness execution for Codex, Claude Code, Pi and Gemini through an embedded pinned agent registry; explicit working roots, native session lifecycle, confined filesystem writes/diffs, per-discussion permission policies and approvals, enabled MCP definitions, persisted discussion events and a token-free development/test agent. Codex app-server remains quota-only. Frontend changes store the new events/state without adding rendering or CSS; skills sinks are deferred.

- New Settings controls for vault unlock/lock/decryption, one-time recovery keys and an additional key wrap, local snapshot listing/restoration, secure-context Web Push, and active-model GPU selection using native `--device` identifiers. Local preset ordering uses up/down buttons and the complete catalog even when filtered. Existing components/styles are reused; snapshot sizes remain unknown because the current API does not report them.

- Bench queue supports cloud model choices with explicit consent, streamed TTFT/reported token throughput and request cancellation, preserving local benchmarks.

- Unified discussion SSE events and one chat reducer for local, cloud and harness turns; replay/reconnection replaces polling without resubmitting generation. Legacy endpoints, provenance, reported usage and cancellation remain available.

- Contextual selection drawers on Local models/presets, Cloud models and Harnesses/model names, reusing existing styles with keyboard/focus support. Inspection does not load/connect/select an executor. Filtered presets retain the correct load index.
- Bare-model `--fit on` on supported llama.cpp builds, leaving CTX/NGL unset while preserving explicit settings and older-engine fallback. Nullable `ctx_effective` reports native router context per slot without auto-loading or persisting fitted values. Untouched drafts no longer freeze a VRAM estimate or create remembered settings on load.

- In-package `Engine` wrapper over the existing llama.cpp lifecycle, router, argument builder and VRAM estimator; authenticated read-only `GET /api/engine/params` merges curated JSON controls with installed native help. Essential/Advanced/Expert reuse the same UI components and design; generic Advanced additions require only a catalogue entry and rebuild.

- Ordered runtime registry with descriptor-driven harness and quota pages, optional native connection/quota actions, and authenticated generic `POST /api/runtimes/{id}/connect|quota` endpoints. Existing routes remain aliases; planned runtimes stay capability-less and native quota reads retain explicit consent/action, caching and throttling.

- New interface, rebuilt from scratch on the approved design: one discussion view with an execution picker (Local · Cloud · Harness), a contextual right panel (llama.cpp parameters with live VRAM estimate and Essential/Advanced/Expert tiers, cloud model, harness session, shared context), and dedicated Local (library, Hub, engine & API, bench), Cloud, Harnesses, Ressources, Usage, project and Settings pages. Dark by default, embedded fonts, native ES modules without a build step. Pre-load tuning provides an advisory VRAM estimate; native fitting resolves unset memory settings at load time.

### Security

- Markdown produced by models is rendered with raw HTML escaped and unsafe link schemes neutralized in the new interface.

- Interface restructure: sidebar Local · Cloud · Harnesses · Ressources · Usage with projects, recents and a live engine card; Local groups Library, Hub, Engine & API and Bench as tabs; Ressources groups skills and MCP servers; Usage is a full page. The model picker becomes an execution selector with Local/Cloud/Harness tabs and search; the right panel gains Parameters/Context tabs.
- New visual system: dark theme by default, embedded Geist and Geist Mono fonts (OFL, no font CDN), warm neutral surfaces, functional-only colour, and motion for pages, popovers, dialogs, toasts, tabs and new messages that respects `prefers-reduced-motion`.

- llama.cpp router mode: when the engine supports `--models-preset`, Loom starts `llama-server` once without a model and loads, switches and unloads models through its API instead of restarting the process. Each resolved configuration becomes a generated INI section; concurrent `/v1` requests reach native slots directly. `ENGINE_MODE=single` or an older engine keeps the previous one-process-per-model mode.
- Load-time parameters sent by an API client (`num_ctx`, `parallel`, KV cache types…) now create an ephemeral router variant beside the user's configuration instead of restarting the engine with a global overlay.

- Grouped Antigravity reasoning variants, an adjacent composer effort selector, collapsible harness model management and persistent per-response runtime/model/token/duration attribution across native replay.
- Reported Antigravity thinking-token counts, end-to-end average throughput and bounded native tool success/failure/write-target metadata; local response footers explicitly distinguish no API charge from hardware/electricity cost.

- Experimental Antigravity `agy` text bridge: explicit native catalog connection, same-discussion context handoff, streamed text, per-turn usage/session provenance and cancellation. No private-memory migration, provider key export or interactive tool approval bridge.
- Composer usage sheet with read-only native Antigravity/Codex quotas, reset dates and available reset credits when reported; retained Loom token totals and explicitly indicative, manual-price cloud cost estimates.
- Harmonized workspace pages using Loom's native visual language, responsive dialogs/touch targets and landscape/safe-area handling without replacing the original chat or local parameter editor.
- Explicit cloud connection presets, bounded `/models` catalog discovery and selective model registration; optional streamed usage requests for compatible providers.
- PWA manifest/brand icons, contextual installation guidance and a public-only offline fallback; no cached conversations, credentials or background sends.
- Editable discussion titles, project association and discussion-specific instructions; a local-only prepared-text preview including the draft, with no model call or draft persistence.
- Revision-checked discussion edits and sends, prompt provenance and combined context limits that reject oversized inputs without silent truncation.
- Conversation-first navigation: Discussions, Models, Harnesses, Projects, Skills, Connections, Services and Settings; a shared model selector and Model/Context side panel.
- Persistent project instructions, optional working-directory references and explicitly selected reusable skills; context is injected into the next local chat turn without rewriting history.
- Persistent common text discussions with explicit local/cloud switching, per-turn model attribution, cancellation and idempotent send requests; non-destructive text import of existing archives.
- Thin llama.cpp and Chat Completions-compatible adapters, multi-model providers, in-memory-only cloud credentials and model selector visibility controls.
- Persistent harness/model configurations; adapters other than the experimental Antigravity text bridge remain non-executable.
- Protected workspace/context/capability APIs with bounded JSON requests and validation.
- Support for local `.env.local` dynamic dev configuration.
- Real-time task activity & background job indicator in UI.
- Initial private-development version of Loom as a workstation control plane and test bench for llama.cpp.
- Single-process ownership model for external `llama-server`.
- OpenAI-compatible `/v1` proxy with dynamic model loading, slot-level continuous batching, and real-time token throughput metrics.
- Server dashboard with live slots inspection, active/pending request queue, and configurable request history limit.
- Model library with local GGUF catalog, three-tier parameter editing (Essentials, Advanced, Expert), and direct Hugging Face Hub integration.
- Hardware test bench for automated prompt testing and raw prefill/decode throughput measurement.
- Local-first privacy architecture with data isolated in `$LOOM_HOME` and zero telemetry.

### Fixed

- Repository onboarding now documents direct UI embedding, current navigation,
  router model loading, optional OS keychain storage and the real Makefile
  targets. Release builds no longer call the removed UI assembler; the security
  policy no longer assumes a private repository. Historical migration briefs
  identify delivered work and point to the current roadmap.

- Viewport-centered confirmations on phone/tablet and low-height screens, bounded scrolling, compact actions and keyboard focus containment/return.
- Imported display metadata no longer inherits the currently selected local model or treats a replayed text chunk as a measured output token.

- Cloud send controls report missing in-memory credentials after disconnect/restart; the panel refreshes reported usage after each turn.
- Offline send guards preserve the current draft instead of dispatching a request.
- Restored the original discussion/composer and complete native model/preset parameter panel; removed the redundant workspace header. The same sidebar now adapts to local, cloud and preparatory harness states.
- Returning a common discussion to local reuses the rich native conversation pipeline and preserves original archives, tools, reasoning and compaction state instead of relegating them to a separate advanced-chat page.

- Memory snapshot restoration rejects invalid identifiers, empty snapshots, and non-regular entries before changing current files.
- An older plaintext web key continues to protect the control API until its hash migration succeeds.
- The web UI reports an occupied port without attempting to stop the process using it.
- Prebuilt llama.cpp archive extraction rejects escaping links and cannot write through stale symlinks in its destination.
- The demo introduction opens at its heading on narrow screens instead of jumping to its off-screen action buttons.
- Release installers verify the matching SHA-256 manifest before installing and Windows keeps only Loom's canonical binary.
- In-app binary updates reject releases that lack a checksum manifest.
- Release automation prepares an unpublished draft only after an explicit tagged-source check; a pushed tag no longer publishes a release by itself.

No version has been tagged or published yet. Move these notes into a versioned section only when a release is actually shipped.
