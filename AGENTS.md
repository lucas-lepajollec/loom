# Loom

Workstation control plane and test bench for llama.cpp.

Location: `projets/loom`. Private Forgejo authoritative origin, private GitHub mirror.

Working base: Ajean v0.12.8 copied in-tree (`github.com/nathaninline/ajean`). Go package names stay `ajean`; the product is Loom.

## Product boundary

- Pilot `llama-server` as an **external process**. One owned process at a time.
- Do not fork, vendor, or reimplement llama.cpp.
- **First-class path:** point `BIN=` at a llama-server already on the machine (Réglages → Moteur → Lier). Loom classifies it as **llama-server only** or **full llama.cpp** (git/sources above the binary). Updates: official binary vs `git pull` + rebuild. A server-only install can later **Ajouter llama.cpp**. Creating a full tree clones+compiles into Loom data or a chosen folder — still a standalone llama.cpp. Official zip (llama-server only) stays available. Do not advertise per-preset forks.
- Chat talks to that owned server. Threads persist locally. **Serveur** (sidebar, under Modèles) is the live OpenAI `/v1` dashboard: URL, loaded-model card, LAN expose, slots configuration, stats, in-flight requests with live tokens/tok/s from `/slots`, recent completions. **GET /v1/models** lists every local GGUF and preset (not only the loaded one). A client that then POSTs `/v1/chat/completions` with another `model` loads that GGUF/preset at request time — listing or picking in the other app does not start a load. Any parameter the app sends on a completion is honored for that call: sampling stays in the request body; launch flags (`-np` / parallel slots, context, GPU layers, batch, and any other `llama-server --help` flag) apply as a runtime overlay on the owned process only. llama-server starts with 4 parallel slots by default (`-np 4`, continuous batching natif). Concurrent clients (e.g. TraDoc sending multiple requests simultaneously) execute in parallel across active slots without serialization, and subsequent requests are held natively in llama-server's batching queue. Parallel slots (`-np`) can be adjusted directly from the Serveur dashboard or Réglages → Serveur API (survives preset switch). Preset files and remembered GGUF params are not rewritten; keys the app did not send stay as the loaded engine. Header is title + status sentence + API-key action (no status pill). Réglages → Serveur API keeps LAN, optional-vs-required API key, and parallel slots. Full llama.cpp sources are not required to serve.
- **Bench** (sidebar, under Serveur) compares GGUFs **and presets** in a queue: built-in raw prefill/decode plus saved prompt tests. One owned engine at a time; each entry is loaded, measured, then the next. Results stay on the page (prefill/decode tok/s, duration, reply excerpt for prompt tests).
- Model picker at the top of chat loads a GGUF or a named preset. Unload on the live row (circled minus, aligned with the title) stops the owned engine and clears MODEL / active preset. The GGUF in use keeps a green pill even when a preset is what was loaded. A left params panel (open by default) edits the live model or the active preset. Naked GGUFs start from the file’s native context (GGUF `context_length`); panel tweaks are live-only unless **Se souvenir pour le prochain chargement**. Presets keep their own params. The params panel and the preset popup share the same three-level form: **Essentiels** (context slider with an editable value on the label row that can exceed the native max, GPU layers, temperature, **prompt système**, plus **vision / reasoning / effort only when the GGUF actually has them**), collapsible **avancé** (KV, parallel slots, speculative/MTP, batch, flash, load-mode, sampling…), and **Expert** (every remaining `llama-server --help` flag, auto-typed as toggle / slider / select as new flags appear). Info tips sit next to labels. **Prompt système** on the model/preset wins over Réglages → Paramètres; empty panel uses that global prompt; both empty = no system message. Thinking comes from the GGUF chat template (`enable_thinking`, `<think>`, …), not a hardcoded on/off. Effort levels are parsed from that same template (Qwen: `low`/`medium`/`xhigh`; Gemma 4: think toggle only). Vision appears only if a `mmproj*.gguf` sits next to the model (or MMPROJ is already set) — never a dump of every projector on disk. A thinking GGUF shows reasoning on by default; turning it off writes `REASONING=off`. The panel and preset editor show a live VRAM estimate (Unsloth-style KV/compute). Loading a naked GGUF that overflows VRAM warns with GPU / RAM offload before start; **Ajuster les paramètres** opens **Modèles** (local library) with the params panel, without loading — subtitle says **pas chargé**, never “preview”. **Charger** remembers then starts. **Remettre les défauts du modèle** restores GGUF-native params in the panel.
- **Modèles** (sidebar, under Nouveau chat) lands on the **local library**: downloaded GGUFs (full path + folder), uninstall, and presets (edit/create/delete). Select a GGUF or preset **without loading** to edit it in the params panel (that unload-edit surface is library-only; chat still has the panel for the live model). Header switch **Hub** opens the Hugging Face GGUF catalog: search (short field), sort/size/VRAM filters, per-repo page, VRAM-fit estimate against **total GPU VRAM** (not currently used — a load replaces the resident model) from `/api/vram`, download via existing `/api/models/download` into the hub folder set in Réglages → Moteur (default `$LOOM_HOME/models`). Flag existence still comes from `llama-server --help`.
- Agent mode (shell / write / tasks), ajean.link, and remote machines/postes are **out of the product**.
- Memory is off or on-demand — never auto-search/write. Compaction stays: at ~75% of the context window, the middle turns are summarized.
- Internet and MCP stay, **opt-in per chat** (composer tools icon). Composer card: `+` (file attach) / tools / context / mic / send, field below, same on phone and desktop. Internet will later accept any web server; MCP must keep working.
- Flag existence comes from `llama-server --help`, never from a hardcoded list alone.

## Isolation (must not touch a live Ajean)

- Data: `$LOOM_HOME` → `$AJEAN_HOME` if set → `$XDG_DATA_HOME/loom` or `~/.local/share/loom`. Never `/etc/ajean`. Never read `/etc/default/ajean`.
- Services: `loom-engine` / `loom-ui`. Default engine bind `127.0.0.1:8081`.
- Installed binary: `/usr/local/bin/loom`. Do not link `/usr/local/bin/ajean`.
- Do **not** run `sudo … install` against this tree until you mean to install Loom, and never against a machine’s Ajean.

## Commands

```bash
make test
make assemble-ui          # rebuild internal/ajean/ui/index.html from ui/src/
make api                  # go run ./cmd/ajean web  (UI on 127.0.0.1:8091)
make build                # assemble-ui + bin/loom
```

Point at llama.cpp with `BIN=` in config (UI Moteur → Lier, or `loom edit`). Model dirs in Réglages → Moteur (and the preset editor). Pointing at a `llama-server` adopts the nearest useful `models/` folder. Declared model folders and the `llama.cpp` tree next to `BIN` are scanned recursively for `.gguf` files. Never hard-code a single user’s home path as a default.

## Forbidden

- Killing a llama-server process Loom does not own (Ajean, Unsloth, …)
- Inventing llama.cpp flags
- Embedding personal user-specific paths in defaults (detection probes common locations only)

Private notes: ignored `.project-local/` and `AGENTS.override.md` (must re-read this file first).
