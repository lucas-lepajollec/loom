# Loom

Workstation control plane, OpenAI-compatible proxy, and dashboard for [llama.cpp](https://github.com/ggml-org/llama.cpp).

Loom operates **one** owned `llama-server` instance and provides an elegant local web interface, automated continuous batching across parallel slots, performance benchmarking, local model library management, and strict local-only data isolation.

## Key Features

- **Local-first & Private**: 100% offline, zero telemetry, zero analytics. All conversations, settings, presets, and downloaded GGUFs reside strictly on your machine (`$LOOM_HOME` or `~/.local/share/loom`).
- **Use Your Existing llama.cpp**: Point Loom at any pre-existing `llama-server` binary, or build/install upstream llama.cpp directly through Loom.
- **OpenAI `/v1` Endpoint & Multi-Slot Dashboard**: Full OpenAI-compatible chat/completions proxy (`/v1`) with live real-time token/throughput monitoring, parallel slots visualization, and automatic model switching.
- **Model Library & Hugging Face Hub**: Local GGUF catalog, preset configuration (context, GPU layers, reasoning effort, KV cache settings), and direct Hugging Face GGUF downloads.
- **Test Bench**: Built-in prompt and raw prefill/decode benchmarking to measure real hardware throughput.
- **Clean Responsive UI**: Fast, responsive web UI with collapsible sidebar, settings, dark/light themes, MCP tool integration, and chat export.

## Quickstart

### 1. Build Loom

```bash
make build    # compiles UI and outputs bin/loom
```

### 2. Point to your llama.cpp engine

```bash
# In the Web UI (Settings → Engine) or via CLI:
./bin/loom edit
# Set BIN=/path/to/llama-server
# Set MODEL=/path/to/model.gguf
```

### 3. Run

```bash
# Start the web interface directly (default port 8091):
./bin/loom web 8091
```

Access the UI at [http://127.0.0.1:8091](http://127.0.0.1:8091).

## Architecture & Ports

- **Web UI & API Proxy**: `http://127.0.0.1:8091`
- **llama-server Engine**: `http://127.0.0.1:8081` (default)
- **Data directory**: `$LOOM_HOME` (defaults to `~/.local/share/loom`)

## CLI Commands

```bash
loom start | stop | restart | status | logs | edit
loom ui start | ui stop
loom web [PORT]                      # Run UI server in foreground
loom llamacpp install | update | status # Optional upstream llama.cpp manager
loom bench                           # Benchmark model throughput
loom chat                            # CLI chat session
```

## License

MIT License. Upstream base derived from Ajean.
