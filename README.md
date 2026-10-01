<div align="center">
  <img src="cmd/loom/icon.png" alt="Loom logo" width="96" />
  <h1>Loom</h1>
  <p><strong>A local-first AI workspace, powered by llama.cpp.</strong></p>

  <p>
    <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-6d7cff" alt="MIT license" /></a>
    <img src="https://img.shields.io/badge/self--hosted-111827" alt="Self-hosted" />
    <img src="https://img.shields.io/badge/OpenAI--compatible-/v1-111827" alt="OpenAI /v1 compatible" />
    <img src="https://img.shields.io/badge/continuous--batching-parallel--slots-111827" alt="Continuous batching" />
    <img src="https://img.shields.io/badge/inference-local--first-111827" alt="Local-first inference" />
  </p>

  <img src="docs/ui.png" alt="Loom workstation interface and llama.cpp dashboard" width="1200" />
</div>

## Overview

Loom is a conversation-first AI workspace: the discussion and its explicit project context stay together when you switch between local, cloud and the experimental Antigravity text bridge. Models manages local GGUFs, the download Hub, cloud model lists and providers. Harnesses manages native configurations; Antigravity uses its own discovered account catalog, not exported provider API keys. Other harness execution and interactive terminals are not connected yet.

Loom operates **one** owned `llama-server` instance and preserves its existing model library, settings, continuous batching and benchmarking. Local inference stays on the workstation. Explicitly selecting a cloud destination allows the discussion text and selected project instructions/skills to be sent there; Hub, downloads and updates also use the network when requested.

Instead of forking or vendoring llama.cpp, Loom pilots `llama-server` as an external process. You can point Loom at an existing binary on your machine or let Loom clone and build upstream llama.cpp directly.

The built-in OpenAI-compatible `/v1` proxy exposes `/v1/chat/completions` and `/v1/models` with automatic request-time model switching. It starts `llama-server` with 4 parallel slots by default (`-np 4`), providing native continuous batching so multiple concurrent applications (such as TraDoc, IDE extensions, or agent workflows) can execute in parallel without queue serialization or manual server restarts.

## Interface

The sidebar groups Discussions, Models, Harnesses, Projects, Skills, Connections, Services and Settings. A common model selector controls the next reply in a discussion. The side panel shows model-specific controls and shared context. Models has Local, Cloud and Providers tabs, with per-model visibility in the selector. Harness profiles associate models without claiming unsupported execution. MCP configuration remains in Connections; the server is under Services and Bench remains in the existing local library.

Discussions use Loom's original chat, composer and model/preset picker, without an extra workspace header. The original side panel has three states: complete native local parameters, cloud connection/context, and a preparatory harness configuration. Local replies use the existing rich conversation pipeline (files, tools, reasoning, presets and compaction), not a second simplified chat. Cloud transfers carry **text only**; native attachments, tool state and private reasoning stay local. Returning a common thread to local appends its portable turns to a native archive while retaining the original source archive. Cloud credentials live only in server memory and must be supplied again after restart. See the [context and adapter boundaries](docs/workspace-architecture.md).

The common discussion's context section lets you rename it, attach or detach a project and add instructions without rewriting exchanged messages. **View prepared text** previews its portable instructions/history and draft without contacting a model or saving the draft. Cloud sends and common-context edits reject stale revisions. Native local replies retain their existing prompt, tool and compaction processing; the portable preview is not a full native wire dump. Byte limits are not model token-window estimates.

### Cloud connections

In **Models → Providers**, create a connection using the OpenAI, OpenRouter or Mistral URL preset, or a custom **Chat Completions-compatible** endpoint. Enter its API key and explicitly request **Verify and retrieve models**: this sends the key only to that destination's `/models` endpoint, without sending a conversation or generating a reply. Choose up to 32 model IDs to save; manual IDs remain available when catalog discovery is unsupported. A returned catalog does not guarantee chat compatibility, account access or quota. The usage-reporting option can be disabled for providers that reject `stream_options`.

Use **Models → Cloud** to choose which saved models appear in discussions. Selecting one in the original model picker confirms the destination before sharing portable text/context. The same discussion continues, with model attribution and token usage when reported. After a server restart or disconnect, reconnect its key in Providers; sending is blocked until then. Keys are not saved in the database or browser storage. These presets do not implement Anthropic's native API, Responses-only models or arbitrary provider protocols.

### Mobile and installed app

The interface retains its original chat and parameters, with responsive workspace pages, scrollable dialogs, larger touch targets and safe-area handling. **Settings → Appearance → Application** shows installation instructions or an install action when the browser offers it. Installation/service workers require HTTPS, except for browser-recognized loopback development origins; plain LAN HTTP can show the interface but is not an installable-PWA guarantee.

The service worker caches only a public offline fallback and listed icons. It does **not** cache conversations, credentials, API traffic or the application document. Offline sends preserve the draft and are not queued or retried automatically. An already-open interface may remain visible, but offline chat or durable offline drafts are not provided. Existing push behavior is retained. See [service-worker security requirements](https://developer.mozilla.org/en-US/docs/Web/API/Service_Worker_API).

The screenshots below document the existing inference controls; the workspace shell has since evolved. See the [architecture audit and first implementation](docs/workspace-architecture.md).

| Chat & Parameters | OpenAI `/v1` Server Dashboard |
| --- | --- |
| Responsive chat with thinking/reasoning inspection and a three-tier parameter tuning panel (Essentials, Advanced, Expert). | Real-time slots monitoring, live token throughput (tok/s), active/queued request states, and completion history. |
| <img src="docs/ui.png" alt="Loom chat interface and parameter panel" width="600" /> | <img src="docs/loom-server.png" alt="Loom OpenAI /v1 server dashboard" width="600" /> |

| Model Library & Hub | Hardware Test Bench |
| --- | --- |
| Local GGUF catalog, preset editor, and direct Hugging Face repository catalog with VRAM sizing estimates. | Standardized prompt tests and raw prefill/decode throughput benchmarking to measure hardware speed. |
| <img src="docs/loom-models.png" alt="Loom model library and hub" width="600" /> | <img src="docs/loom-bench.png" alt="Loom hardware test bench" width="600" /> |

## Highlights

- **External process orchestration**: Pilots `llama-server` as a clean external subprocess without modifying upstream source code.
- **Antigravity text bridge**: Opt-in `agy` catalog and streaming CLI turns in the original discussion, with shared text context, cancellation, native session IDs and reported tokens. Each turn starts fresh; native permissions/configuration remain in force, without Loom adding auto-approval. This is not a full tool/approval or private-memory integration.
- **Usage and quotas**: A closable composer sheet reads Antigravity/Codex account limits on request, shows available reset counts when reported, and summarizes retained Loom turns. API costs use manually entered indicative prices, not account billing; unknown values never become zero. No reset credit is redeemed.
- **Reasoning and response provenance**: Antigravity reasoning variants share one picker entry; the composer selects a discovered native level without sending a message. Past responses retain their runtime/model, reported tokens and duration; native tool status and bounded write targets are collapsible. Local responses retain llama.cpp decode metrics and show “No API cost”, not free electricity or hardware. Hidden reasoning text is not reconstructed.
- **Project context**: Group discussions with shared instructions, a working-directory reference and explicitly selected reusable skills. No directory contents are read automatically.
- **Continuous batching**: Native multi-slot parallel inference (`-np 4` default), allowing multiple client tools to query the engine concurrently without serialization.
- **OpenAI `/v1` proxy**: Standard `/v1/chat/completions` and `/v1/models` endpoints with on-demand model loading and runtime parameter overlays.
- **Three-tier parameter control**: Customize context windows, GPU layers, reasoning effort and KV cache. Curated controls come from a versioned JSON catalogue; Expert follows the installed `llama-server --help`. The authenticated, read-only `/api/engine/params` endpoint merges both without changing saved configuration.
- **VRAM estimation**: Calculates expected KV and compute VRAM requirements against total GPU memory before loading.
- **Model Library & Hugging Face Hub**: Search, inspect, and download GGUF models directly to local disk with download resume and progress tracking.
- **Hardware test bench**: Benchmark prompt completion times and raw token generation speed (prefill tok/s, decode tok/s) across models and presets.
- **Local-first inference**: llama.cpp prompts and completions stay on the workstation. Explicit cloud/harness selection shares portable text with that destination; optional Hub searches/downloads use the network. Loom adds no telemetry.

## Quick start

### Install a release

Review the installer before running it. It selects the binary for your platform and verifies it against the release's SHA-256 manifest. On Linux and macOS, it also configures system services with elevated privileges. See [RELEASING.md](RELEASING.md) for the maintainer's release process.

On Linux and macOS:

```bash
curl -fsSL https://raw.githubusercontent.com/lucas-lepajollec/loom/main/install.sh -o loom-install.sh
# Review loom-install.sh before continuing.
sh loom-install.sh
```

On Windows (PowerShell):

```powershell
Invoke-WebRequest https://raw.githubusercontent.com/lucas-lepajollec/loom/main/install.ps1 -OutFile loom-install.ps1
# Review loom-install.ps1 before continuing.
.\loom-install.ps1
```

The installers stop if the matching release binary or checksum is missing; they do not silently build different source code instead.

---

### Building from source

If you prefer to build from source:

- Go 1.25 or later
- GNU Make

```bash
git clone https://github.com/lucas-lepajollec/loom.git
cd loom
make build
./bin/loom web 8091
```

### Run the web interface

```bash
./bin/loom web 8091
```

Open [http://127.0.0.1:8091](http://127.0.0.1:8091) in your browser.

### 3. Link your llama-server engine

In the Web UI, open **Settings → Engine** (or run `./bin/loom edit` via CLI):
- Set `BIN=/path/to/llama-server`
- Configure your local model directories
- Select a model in the library to start serving immediately

## Configuration and persistence

- **Data directory**: All configuration, presets, chats, and downloads are persisted in `$LOOM_HOME` (defaults to `~/.local/share/loom` or `$XDG_DATA_HOME/loom`).
- **Configuration**: Persisted in `$LOOM_HOME/loom.db` (bbolt), alongside chats and preferences. Change settings through the UI or `./bin/loom edit`; legacy `config.env` files may be imported during migration.
- **Development environment**: Loom automatically loads `.env.local` or `.env` from the project root if present, allowing local overrides without editing tracked files.
- **Model discovery**: Recursively indexes all `.gguf` files within declared model directories and the directory containing the `llama-server` binary.

## Security, privacy, and limitations

> [!WARNING]
> Do not expose Loom or its OpenAI `/v1` endpoint directly to the public internet without an authenticated TLS reverse proxy.

- **Loopback binding**: Loom binds to `127.0.0.1` by default.
- **LAN access**: The OpenAI front can be exposed intentionally; Loom requires an API key before enabling that mode. The web UI remains on loopback by default and uses a separate web key.
- **Single owned process**: Loom manages exactly one running `llama-server` process at a time; loading a new model safely unloads and replaces the resident instance.
- **Explicit destination**: local inference stays local. Selecting a cloud model requires confirmation before the conversation and selected context are sent to that provider. No telemetry. Never put credentials in project instructions or skills.

## Architecture

| Component | Implementation |
| --- | --- |
| Core daemon & proxy | Go 1.25+, bbolt embedded database |
| Web dashboard | Single-page UI (HTML5, modern CSS, ordered plain JavaScript sources) assembled into Go binary |
| Workspace layer | Portable conversation records, per-turn runtime attribution, local/cloud adapters, model/provider catalog, explicit project context and skills |
| Inference backend | Standalone external `llama-server` subprocess |
| Web UI / control port | `8091` (`http://127.0.0.1:8091`) |
| OpenAI-compatible front | `8081` (`http://127.0.0.1:8081/v1`) |
| Managed llama-server backend | `18081` by default, loopback-only and derived from the front port |

```text
cmd/loom/          # Main application entry point and Windows resource metadata
internal/loom/     # Daemon, OpenAI /v1 proxy, process orchestration, and embedded UI
tools/             # Build scripts and single-page asset assembler
docs/              # Visual assets and architecture documentation
```

## Development and quality

| Command | Purpose |
| --- | --- |
| `make build` | Assemble web UI assets and compile `bin/loom` |
| `make test` | Run Go test suite (`go test ./...`) |
| `make web` | Run the web server in foreground on port 8091 |
| `make assemble-ui` | Reassemble `internal/loom/ui/index.html` from `internal/loom/ui/src/` |

## Interactive demo

The [browser-only demo](https://demo.loom.lucas-homelab.fr) simulates Loom with fictional data. It needs no GPU, model, daemon, or account, and does **not** perform inference. The actual workstation product runs locally.

## Documentation and community

- [User documentation](https://docs.loom.lucas-homelab.fr)
- [Contributing guide](CONTRIBUTING.md)
- [Changelog](CHANGELOG.md)
- [Security policy](SECURITY.md)
- [Support](SUPPORT.md)
- [Code of Conduct](CODE_OF_CONDUCT.md)
- [MIT License](LICENSE)

Third-party dependencies and llama.cpp retain their respective licenses and terms.

## Runtime actions

The workspace runtime catalog comes from one ordered adapter registry. Descriptors
include their description, native CLI, connection consent text and implemented
capabilities. Codex, Claude Code, Pi and Gemini now have ACP adapters, available
when their registry launcher and native detection binaries are installed. Hermes
remains a planned entry without executable capabilities.

Explicit native catalog connection uses `POST /api/runtimes/{id}/connect` with
`{"consent":true}`. Explicit quota reading uses `POST /api/runtimes/{id}/quota`
with `{}`. Both retain the control API's authentication, private-cache and vault
boundaries. Quota reads share the existing 30-second throttle/cache and never
start generation or consume reset credits. The Antigravity connect route and
`/api/usage/refresh` remain aliases. Codex app-server now reads quotas only;
its old catalog-connect alias reports that connection discovery is unsupported. See
[workspace contracts](docs/workspace-architecture.md#runtime-registry-and-optional-actions).


ACP discussions require an explicitly selected existing absolute workdir before
sending. Session configuration, approval routing, confined fs writes/diffs and
ordered display events are available over the control API; the chat store retains
those events for the design owner's rendering. `LOOM_DEV_FAKE_ACP=1` enables a
scripted agent that exercises this path without model calls. Native session
resumption is negotiated and requires compatible portable context. Skills sinks
remain deferred. See the [ACP API and lifecycle notes](docs/agents/acp-implementation.md).


## Settings and classic feature migration

Settings → Security and data supports masked password/recovery-key unlock,
locking, confirmed decryption, and adding/replacing an additional vault key.
Encryption displays its recovery key once; keep it offline. Local snapshots
restore only memory files and the vault, with a safety snapshot first; they do
not restore discussions, presets or settings. Snapshot sizes remain unknown.
General settings enables Web Push only in supported secure browser contexts.
Engine settings selects GPUs for the active model using its binary's native
identifiers and saves `EXTRA_ARGS --device` in the preset or remembered model
before confirmed application. Unknown/single GPU lists are read-only. Local
Library's up/down preset buttons save the complete order, including hidden
filtered entries. Automatic classic vault unlock/legacy-secret migration,
remote relay backups and GPU tensor-split controls remain outside this slice.

## Inspecting models and automatic local settings

Click a model name in Local or Cloud, or a harness/name in Harnesses, to open
its contextual drawer. Local models and presets expose their parameter editor;
cloud/harness selections show declared connection, model and capability data.
Inspection alone does not load a model, connect an account or change a chat.

Bare local loads leave context/GPU layers automatic and use `--fit on` when
advertised by the installed engine. Explicit settings and remembered values
remain effective; older builds retain the historical defaults. The pre-load
VRAM estimate is advisory. After router loading, the inspector shows observed
context per slot when available, otherwise “—”; this is distinct from the native
model limit. See [native fitting documentation](https://github.com/ggml-org/llama.cpp/blob/master/tools/server/README.md).
