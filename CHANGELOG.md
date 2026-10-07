# Changelog

Notable changes to Loom are documented here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and releases use [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- Agents: each agent on each machine (Loom's own included) has two separate
  choices, **Manage** (Loom follows it: account, versions, updates, history
  import) and **Use in Loom** (it can run discussions). On other machines both
  start off. Machines › a machine lists its detected agents with these two
  switches; the Agents page groups "Used in Loom" and "Managed" agents with a
  chip per machine, and **Add an agent** proposes agents detected on your
  machines, installs one here, or adds an ACP command.
- Edit the last message: a pencil on your last message lets you change it and
  resend; the previous exchange is removed and the agent restarts from the
  remaining history.

### Fixed

- Command output is shown again for Codex (output sent in `_meta` terminal
  chunks) and Antigravity (`tool_info.output`) instead of "No output".
- Antigravity in Cautious mode: refused commands now become a Loom approval
  request; once allowed, Antigravity resumes with exactly those actions.
- Large discussions open fast: sessions are sent without turn events, replayed
  events are sent once with long tool texts trimmed (full output on demand).
  An imported 100 MB Codex thread went from 58 MB to 4.6 MB on opening.
- Navigation labels follow the interface language; import page note spacing.
- Session panel: changing an agent's model no longer closes its session; when
  the agent offers that model, it switches in place and keeps its context,
  modes and options. Agents that fold the reasoning level into the model name
  (Antigravity) show one model choice and a separate reasoning level. The
  model choice is always shown (from Loom's catalog before the agent session
  exists), sections follow one order, and menus no longer overflow the panel.
- One engine page: **Models › Engine** shows what runs (state, start/stop,
  slots, OpenAI-compatible API, recent requests) then where and with what
  (location, llama.cpp, vLLM, installation). Local keeps Library and Hub;
  `#/local/engine` redirects.
- Faster interface: discussion lists (polled every 30 s by the sidebar and
  `/api/workspace`) carry turn metadata only (14 MB → 15 KB with one large
  imported thread), and opening a discussion no longer refreshes the sidebar
  once per replayed turn.
- Pi: its startup notice and provider retry messages are no longer shown as
  the answer; a failed turn now shows the model's real error (read from Pi's
  own session journal, since pi-acp does not forward it), e.g. "engine
  unavailable". Loom refreshes Pi's provider file before starting Pi, so cloud
  providers added since are listed.

### Changed

- Navigation grouped by domain: **Models** (Local, Cloud), **Agents**,
  **Machines** (Machines, Terminals, Environment), **Brain** and **Activity**
  (Usage, Bench), with tabs inside each group. Machines is now a page of its
  own instead of a Settings section; old `#/settings/machines` links redirect.
- Agents page: each agent card shows where it is installed (this machine and
  every connected machine, with the version read there); an agent driven on
  another machine joins its family's card. Capability lists moved out of the
  overview, and the state reads "Connected" / "Not connected".
- Settings only hold Loom itself (General, Internet, Security and data,
  About). The engine moved to **Models › Engine**; workspaces and startup moved
  to **Machines** tabs. Old `#/settings/engine|startup|workspaces` links
  redirect.

## [0.2.10] - 2026-10-07

### Added

- Update channels: Settings › About › Updates can follow **Development**, an
  `edge` pre-release rebuilt after every merged change, instead of waiting for
  the next stable release. Works with the Linux system updater too.

## [0.2.9] - 2026-10-07

### Fixed

- Remote machines whose shell prints a banner on every SSH connection (for
  example `fastfetch` or a message of the day in `.bashrc`) now work: importing
  their harness discussions and running their harnesses no longer fails with
  "the agent does not respond to the ACP protocol", and harness versions in
  Settings › Machines no longer show the banner text.

## [0.2.8] - 2026-10-07

### Fixed

- A tool can no longer stay "running" forever: when a turn ends, unfinished
  tools are closed as not run. Antigravity actions refused in Cautious mode
  (it cannot ask for approval headlessly) now end with an explanation instead
  of a silent, endless "in progress".
- Settings spacing: notes inside cards have consistent padding and separators,
  notes below a card align with it, and grouped sections keep the same gap.
- The llama.cpp install recommendation is translated.
- Importing a harness session that is still open in the harness's own app (for
  example a Codex desktop conversation) now says so and asks to close it there,
  instead of a vague "could not reopen" error.
- Reading native usage no longer leaves one empty Claude Code project per
  reading in `~/.claude/projects`; it runs in a single stable directory.

## [0.2.7] - 2026-10-07

### Fixed

- Sending no longer fails with "the model, thread or its context changed" when
  automatic project context moves between preparation and sending: the draft is
  always prepared with its real text and resent once after a context change.
- Long discussions stay usable: past 128 KiB or 200 messages, the oldest whole
  messages are left out of what the model receives (with a notice) instead of
  refusing every new message. Stored history is unchanged.

### Added

- Pasting more than 2,500 characters turns the text into a `pasted-text-N.txt`
  attachment that can be removed, downloaded and expanded in the thread; the
  model receives the full text. Messages may now hold up to 64 KiB.

## [0.2.6] - 2026-10-05

### Added

- Select DuckDuckGo, self-hosted SearXNG, Brave Search or Tavily in Settings →
  Internet, with an explicit search test and encrypted remembered API keys.
  Search is independent of the Go/Crawl4AI page reader.
- Opt-in web search for cloud Chat Completions models that support function calls.
  This connector permits only `web_search`, with bounded calls/results and visible
  tool events; it does not expose host files, shell or private memory.

- `make dev` rebuilds uncommitted source into a foreground instance with separate
  development data. An explicit LAN listener allows phone testing before a
  release, with the same access authentication and no saved binding changes.
- Native harness discussion transfer between detected sources and connected
  destination harnesses, including linked machines. Portable transcripts retain
  source provenance and start a fresh destination session on the next user turn;
  native credentials, permissions, tool state and files are never copied.
- Linux systemd startup settings for installed Loom UI, engine and engine-node
  services, locally or over SSH. Linked nodes expose a dedicated engine startup
  policy; vLLM requires a cached model and starts offline. User services report
  whether lingering is available for startup before login.

### Fixed

- Synchronize compiled Windows version resources with the source version and
  reject stale resources in CI and release builds. The 0.2.5 draft was not published.

- Limit the model selector to 70% of the visible viewport and at most 520px,
  with scrolling inside the list and a persistent close button.
- Keep the last Usage quotas, native activity and cloud balances visible across
  page navigation and failed refreshes, with a visible quota refresh indicator.
- Place remote discussion import under each detected harness in machine settings,
  independently of whether that source harness is linked to Loom.

- Bound complete JSON responses and authentication requests, release suspended
  requests, reconnect idle event streams and refresh workspace state on resume.
  UI cancellation now settles independently of transport abort, and store
  notifications recover from throttled animation frames.
- Executor/workspace preparation and native configuration no longer hold the
  shared discussion lock during SSH/native calls. Concurrent changes are checked
  before saving, and sends/deletion cannot race a reserved configuration.
- Remembered cloud API keys survive restarts on headless hosts using a private
  encrypted credential store when the OS keychain is unavailable. Forgetting
  reports a removal failure rather than falsely claiming success.

## [0.2.4] - 2026-10-04

### Added

- Connected second brains from local folders, Obsidian or mounted stores and
  managed HTTPS/SSH Git checkouts. One source can be the writable primary with
  bounded Markdown write/edit tools for direct models and authenticated Brain MCP.
- Link a skills directory directly from a connected second brain.
- Compatibility for existing shared preference pages and stored project
  continuity metadata across executor changes.
- Project-scoped conversation retrieval over HTTP/MCP, including semantic search
  and direct passage reads derived from current project membership.
- Review/edit/keep/reject for newly distilled memory candidates; existing memory
  remains accepted. Source freshness states, optional multilingual embeddings
  and explicit incremental semantic auto-indexing.
- Native-import project attachment, immutable runtime/machine/session provenance
  and deduplication after route changes; fresh native execution by default in UI.

### Changed

- Brain navigation now presents second brains, Skills and MCP. Loom's own
  conversation, retrieval and reviewed-memory layers remain automatic instead
  of becoming project setup fields.
- Project setup now contains title, machine, workspace and default
  executor/model. Project conversations define their memory scope automatically;
  connected second brains are inherited with a bounded default retrieval budget.
- Project retrieval uses the current draft from the first turn; native local
  context is prepared once and blocks unreadable selected preferences.

### Fixed

- Confine second-brain file creation and edits with directory-root operations,
  validate managed checkout IDs before cloning and retain checkouts on rejected
  source configuration. Capture the owning indexer for background write refreshes.
- Serialize visible status, hardware and activity observations, bound stalled
  JSON reads and avoid rerendering unchanged UI selections.
- Keep anchored menus outside transformed parents, coalesce repositioning and
  dismiss on completed clicks so touch actions can finish.
- Cancel stale parameter/capability/estimate reads and show a retry on failure.
- Prepare discussion context and remote workdirs outside the global session
  lock; recheck edits, credentials, duplicate requests and run limits before send.
- Bound project semantic retrieval with lexical fallback and remote engine
  observation reads without limiting long engine mutations.

## [0.2.3] - 2026-10-03

### Fixed

- Switch existing harness discussions to direct local execution using the active
  engine's catalog, including remote nodes and linked servers. Reject missing or
  ambiguous choices instead of silently keeping the harness route; distinguish
  runtime/provider identity and keep the execution label visible on phones.
- Keep historical cloud rows in Usage eligible for API pricing and estimates;
  do not infer a subscription from the presence of a runtime ID.
- Give the mobile navigation drawer and page headers opaque, theme-aware surfaces.
- Keep model selection and anchored menus inside the visible viewport as screen
  size, content or the mobile keyboard changes.
- Open Settings on a mobile section index, with a back link from each section;
  retain the desktop navigation and avoid mounting hidden settings forms.
- Reflow Bench and Usage results with metric labels on narrow screens, preserving
  all native usage values. Wrap page actions, account quotas and long form controls;
  keep mobile dialogs scrollable and support screen safe areas.

## [0.2.2] - 2026-10-03

### Fixed

- Keep mixed benchmark queues, saved tests and history on the control plane when
  an engine node is selected; only local rows are dispatched to that node.

- Prevent duplicate native authorization-code submission even if the CLI repeats
  its prompt, and clear queued codes when login ends or is cancelled.
- Show the control plane's own hostname/version in the sidebar, independently
  of a linked engine's status/version. A main UI update no longer looks unapplied
  merely because its engine runs an older release.
- Distinguish missing native harness CLIs from missing ACP launchers, and use
  the same user-installed executable lookup for Antigravity reads and Codex quotas.
- Reject Antigravity bridge sessions when its native model catalog is unavailable
  instead of exposing an empty, apparently connected adapter.

### Added

- Bench model catalog independent of chat visibility, native Claude-account
  model-only runs with tool/MCP/customization suppression and account preflight,
  named prompt/output-budget tests, full responses and run history. Unsupported
  native adapters remain visible and non-executable. Cancellation is scoped to
  the owned node job on updated nodes; old nodes require a manual stop.

- Browser-account buttons for ChatGPT, Claude and Google in the harness-page
  dialog. Native CLIs retain OAuth and credentials; Loom handles only expiring
  links/codes, cancellation and a terminal fallback for other onboarding.
- Engine-node maintenance access and update controls for each SSH machine,
  independent of the active inference engine; credentials stay server-side.

### Changed

- Gemini CLI is removed from built-in harness, installation and machine choices.
  Saved discussions/configuration and user-installed CLIs remain untouched.
  Gemini cloud providers/models are independent and remain available.
- Usage refreshes retained Loom metrics every four seconds and connected native
  quotas/usage/provider balances every 30 seconds while visible, including
  immediate refresh on resume. Duplicate reads are bounded; failures keep stale
  observations and unsupported metrics stay unknown.
- Native quota I/O no longer holds the global cache mutex. Missing CLIs use the
  installation state without a redundant alarming probe banner.

## [0.2.1] - 2026-10-03

### Control plane audit

- Restrict anonymous control access to loopback Host and peer; close DNS-rebinding
  and credential-removal exposure paths. Require POST for remaining legacy
  actions, preserve dual read/write endpoints and reject malformed/oversized JSON.
- Revoke pending terminal tickets and established terminal/SSE/preview WebSocket
  access with its owner session or credential; bound tickets and ACP requests.
- Confine workspace downloads with one `os.Root` file handle; bound shell output
  during execution and crawler responses; refuse linked-engine redirects.
- Require patched Go 1.26.8+, update x/crypto, and add vulnerability/race/doc gates
  plus weekly dependency monitoring. Record scope and limits in
  `docs/control-plane-audit.md`.
- Reduce Brain ranking/quoted-search allocations and avoid whole-history copies
  for streamed runtime deltas while preserving ranking, privacy and persistence.

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

- Track asynchronous skill distribution so test fixtures wait for background writes before removing their data directory. The unpublished 0.2.0 candidate exposed a cleanup race in CI and is superseded by this release.

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
