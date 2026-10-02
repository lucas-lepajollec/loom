# Local and linked engines

Loom controls external inference engines; it does not implement inference.
llama.cpp remains the common local engine on Linux, macOS and Windows. A
router-capable llama-server starts once and loads models through its API.
The existing llama.cpp parameter/preset and automatic-update workflows are
unchanged.

Settings › Engines exposes vLLM installation, its Hugging Face model library,
per-model parameters, version and updates. The authenticated APIs below back
those controls. **Local vLLM requires Linux and a
supported NVIDIA GPU with CUDA or AMD GPU with ROCm.** This Loom workflow does
not install native vLLM on macOS or Windows. Those Loom installations can use
llama.cpp or link a Linux inference server by address (or another Loom).
Merely listing parameters/cache entries does not launch inference.

## Installation and lifecycle

`GET /api/engines/vllm` reports `installed`, `version` (empty if unknown),
`missing`, `running`, `model`, `job`, `error`, `dir`, the bounded `log` and
`auto_update` state. `POST` accepts `{"action":"install"}`, `update`, `stop`,
or a start request:

```json
{"action":"start","model":"Qwen/Qwen3-8B","params":{"max-model-len":"8192","gpu-memory-utilization":"0.85"}}
```

Loom installs into `LOOM_HOME/engines/vllm`, preferring uv with Python 3.12 and
otherwise using a system Python venv/pip. Updates use `uv pip install --upgrade`
or the environment's `python -m pip install --upgrade`. AMD installation and
updates require uv and use the official ROCm wheel index. A supported ROCm
driver/runtime and compatible GPU must already be installed; the backend does
not install drivers or promise that every AMD GPU works. See the upstream
[GPU installation requirements](https://docs.vllm.ai/en/stable/getting_started/installation/gpu/).

Install/update refuse with HTTP 409 while serving, loading, stopping or another
action is in progress, before changing logs or the Python environment. Start
applies saved model settings, then temporary `params` overrides; it does not
save overrides. Legacy `gpu_memory_utilization` and `max_model_len` numeric
fields remain accepted, with validation. Stop can cancel a model still loading.
The process serves on loopback and retains the existing direct-engine link,
health wait and log/state workflow. Usage telemetry remains disabled.

`GET/POST /api/engines/vllm/auto-update` returns `{ok,state}` or accepts
`{"auto":true}`. This per-engine option is off by default and persists across
restarts. Checks run every 30 minutes, with upstream version checks at most
every six hours; updates reserve the same lifecycle action as manual upgrades
and run only while stopped. The state records check/result timestamps, source
and actual installed versions, pending version and errors. Checking/updating
never starts generation. llama.cpp's separate auto-update option is preserved.

## Parameters saved per model

`GET /api/engines/vllm/params?model=Qwen%2FQwen3-8B` returns
`{ok,engine_id,model,params,values,gpu_count}`. Omit `model` for the catalog alone.
`GET /api/engine/params?engine=vllm` also returns the shared `ParamSpec` catalog;
the existing route without this query still describes llama.cpp.

`POST /api/engines/vllm/params` replaces that model's remembered settings:

```json
{"model":"Qwen/Qwen3-8B","values":{"max-model-len":"8192","dtype":"auto","reasoning-parser":"auto","enable-auto-tool-choice":"on","tool-call-parser":"auto","trust-remote-code":"off"}}
```

Keys match the catalog `id`/`key`. Values are strings, as in the llama.cpp
editor. Empty/missing values defer to native defaults; `{}` resets remembered
settings. Booleans accept `on`/`off` or `true`/`false`. Unknown keys, nonfinite
numbers, invalid bounds/fractional integers, invalid enum values and malformed
parser names return HTTP 400 without changing saved settings. Tensor parallel
size appears only for more than one detected GPU and cannot exceed that count.

Controls cover context length, GPU memory fraction, tensor parallelism, weight
dtype/quantization, KV dtype, prefix caching, simultaneous sequences, eager
execution, CPU offload, swap space, reasoning/tool parsers, automatic tool
choice and remote code. The launch uses argument arrays without a shell.
All changes require a new start; saving alone does not restart a live model.

`reasoning-parser` defaults to `auto` using the existing Qwen/QwQ, DeepSeek R1,
GPT OSS and GLM family mapping. `none` disables it; an explicit parser name
overrides it. Tool choice is off by default. When enabled, `tool-call-parser`
defaults to family selection (Qwen/Hermes, Qwen3 Coder, Llama 3.1–3.3,
Mistral/Mixtral, GPT OSS, GLM 4.5–4.6, DeepSeek V3.1). An unknown family must
specify a parser. These mappings are hints, not architecture/chat-template
compatibility guarantees; the installed vLLM validates supported parsers and
flags. Some families require a suitable model-provided chat template; see
[upstream tool calling](https://docs.vllm.ai/en/stable/features/tool_calling/).

**`trust-remote-code` is off by default**, with `dangerous:true` and an explicit
catalog tip: enabling it executes repository Python code with Loom's rights.
Downloading `.py` files into a snapshot does not execute them.

## Hugging Face model library

All these endpoints require the Loom control key and return `Cache-Control:
no-store`. They are forwarded when the engine is another linked Loom. A direct
inference-server link does not provide remote cache/package management.

| Endpoint | Behavior |
| --- | --- |
| `GET /api/engines/vllm/models` | `{ok,cache,models:[{id,size,servable,snapshots}]}` |
| `GET /api/engines/vllm/hub/search?q=Qwen&quantization=awq` | Text-generation repositories with root safetensors and `config.json`; optional `awq`, `gptq` or `fp8` filter. Returns models and machine GPUs. |
| `POST /api/engines/vllm/download` | `{"model":"org/model"}` starts an asynchronous prefetch (202); one at a time (409 if busy). |
| `GET /api/engines/vllm/download` | `{ok,download}` with model/revision/file, `done`/`total` bytes, completion/cancellation/error and start time; null before any download. |
| `POST /api/engines/vllm/download/cancel` | `{"model":"org/model"}` cancels the matching transfer. Partial files remain resumable. |
| `POST /api/engines/vllm/models/delete` | `{"model":"org/model"}` removes that repository's cache; refuses while it is serving/loading or downloading. |

The cache location honors `HF_HUB_CACHE`, then `HF_HOME/hub`, then
`XDG_CACHE_HOME/huggingface/hub`, otherwise `~/.cache/huggingface/hub`.
Repository folders use HF's `models--org--model`, `snapshots/<commit>` and
`refs/main` layout. Existing snapshot-to-blob symlinks are supported within
the repository. Sizes count regular files once (including partial files);
missing caches return an empty list. `servable` is a heuristic, requiring a
complete-looking snapshot with config and weights, all shards listed by a
safetensors index, and no pending Loom prefetch marker. It does not prove vLLM
supports the architecture. A cache entry can be present with `servable:false`.
Deleting a repository also removes its cached revisions used by other HF
applications; external processes are not tracked or killed.

Prefetch pins the Hub's commit SHA and downloads root safetensors plus config,
tokenizer, vocabulary, chat-template and Python files; it excludes alternative
binary/GGUF weights and nested exports. Snapshot files are ordinary files,
which HF accepts without symlink support. `refs/main` is published only after
successful completion. Repeating the request skips completed files and resumes
`.loom-incomplete` files with HTTP Range, including after Loom restarts; a
source ignoring Range triggers a fresh transfer. Transient failures retry up
to four attempts. Paths and immutable revisions are validated and filesystem
operations remain confined to the repository. No user-supplied download URL
or shell command is accepted.

Authentication reuses the existing `HF_TOKEN` environment setting, or the
existing HF token file (`HF_TOKEN_PATH` or `HF_HOME/token`); tokens never enter
progress responses, saved settings or logs. Gated repositories require a token
and HF's access approval. This follows
[HF environment/cache conventions](https://huggingface.co/docs/huggingface_hub/package_reference/environment_variables).

Search returns nullable `size` and `estimated_vram_mb`, `quantization`,
`estimate_only:true`, and `verdict` (`fits`, `tensor_parallel`, `no`, `unknown`).
Sizing uses reported weight bytes when available, otherwise reported parameter
counts or a name-based estimate, with a rough runtime/KV allowance. It compares
against GPU total memory at a default 90% budget, with conservative equal
partitions for multiple GPUs. Unknown weight/VRAM data stays unknown. Actual
requirements depend on context, concurrency, hardware, dtype and quantization;
the estimate is advisory and does not apply tensor parallelism automatically.
