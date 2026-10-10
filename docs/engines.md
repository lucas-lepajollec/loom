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

## Installation permissions

Engine installations stay in user-owned Loom directories; vLLM uses a private Python environment, never global pip. A read-only linked llama.cpp source/build or managed engine directory is rejected before fetching/building. Repair ownership with an administrator or install in a new user-owned location; do not run the interface as root. Missing system build dependencies may require administrator installation. In a noninteractive service, package-manager sudo uses `-n` and fails instead of waiting for a password.

For a GPU machine without a second full control plane, use [Loom node](engine-node.md). Direct servers and legacy full-Loom engine links remain compatible.

## Engine as a service

Loom's llama.cpp front accepts external OpenAI-compatible clients and Loom's
native chat, Brain and Bench requests through the same inference endpoint.
A request keeps its native model section for its entire response, including
streaming. Model selection and temporary API parameters are resolved per request;
router requests do not replace the discussion's saved model selection.
llama-server continues to own slots, batching, sampling and token generation.

When loading would evict a resident model, Loom waits for its in-flight requests
to finish. Swap targets are FIFO: new requests for the old model wait behind a
queued swap, while later requests for the next target join that target's batch.
With `MODELS_MAX > 1`, loads into free capacity need no drain; possible evictions
wait for all resident requests to finish because the native router chooses its
victim. `MODELS_MAX=0` retains the native unlimited setting. A queued request waits
at most `ENGINE_SWAP_WAIT` seconds (default **300**), then receives HTTP **503**
with `{"error":{"code":"model_busy",...}}` and `Retry-After: 1`. Cancelling a
waiting request removes it from the queue. Running streams are never stopped to
make room for a queued request.

Clients can send `X-Loom-Priority: interactive` or `background`; the default is
interactive. A named key configured as background cannot promote itself through
the header. Brain continuity, consolidation/distillation and semantic requests
use background priority. Existing CLI, one-shot and queue Bench entry points are
explicit user launches and use interactive priority; Bench sends each row's model
in its inference request rather than preloading it outside the drain policy.
Background requests use an already resident model, or load only when nothing is
resident and there has been no interactive use for `ENGINE_INTERACTIVE_GRACE`
minutes (default **15**). Otherwise they immediately receive `503 model_busy`.
Continuity records `skipped_reason: "engine model_busy"`; consolidation and
distillation return that skip reason without retrying a busy engine as an error.

`ENGINE_IDLE_UNLOAD` defaults to **30 minutes**; **0** disables it. After that
interval with no inference activity, no in-flight request and no pending swap,
Loom releases VRAM. Router mode unloads all resident sections through the native
API. Historical single-model mode stops only Loom's owned llama-server child,
keeping the front and saved selection alive. The next inference loads on demand,
including native chat and Bench after an idle unload. Native generation leases
also protect pauses between tool calls across the web/serve processes; leases
expire after a crashed caller. The idle check runs every 15 seconds. Observations,
model listings and key management do not renew inference activity.

The control-plane endpoints below require the same browser session/control key
as other engine routes, return `Cache-Control: no-store`, and forward to a linked
Loom engine. A direct inference-server link has no Loom service-management API.
The inference process owns live queue/count observations even when the web or node
control plane runs in a separate process.

| Endpoint | Request / response |
| --- | --- |
| `GET /api/engine/service` | `{idle_unload_minutes,swap_wait_seconds,interactive_grace_minutes,models_max,resident:[{model,in_flight,since,last_used}],queue:[{model,waiting,since}],last_activity,idle_unload_at}` |
| `POST /api/engine/service` | Partial `{idle_unload_minutes?,swap_wait_seconds?,interactive_grace_minutes?,models_max?}`; returns the service snapshot. |
| `GET /api/engine/keys` | `{keys:[{id,name,created_at,last_used_at,allowed_models,max_concurrency,requests_per_minute,priority,usage:{requests,prompt_tokens,completion_tokens,last_error}}],endpoint}` |
| `POST /api/engine/keys` | `{name,allowed_models?,max_concurrency?,requests_per_minute?,priority?}` → `{key:{…},secret:"sk-loom-…"}` |
| `POST /api/engine/keys/update` | `{id,…changed fields}` → `{key:{…}}` |
| `POST /api/engine/keys/delete` | `{id}` → `{ok:true}` |
| `POST /api/engine/keys/rotate` | `{id}` → `{secret:"sk-loom-…"}` |

Service settings accept integer idle/grace intervals from **0 to 10080 minutes**,
swap waits from **1 to 3600 seconds**, and model limits from **0 to 64**. Validation
is complete before saving any field. Settings persist across preset changes.
`models_max` writes `MODELS_MAX`. When an owned router is running, changing this
launch parameter queues a control barrier, drains inference and restarts only
that child with the new capacity, then restores resident sections through the
native API (the new limit may evict older sections). Model switches themselves
never restart a router. If no engine is running, the setting applies on its next
start. While the change is pending, admission uses the smaller finite limit of
the configured value and running capacity. A timed-out control barrier leaves the
saved setting for a later start; an already started reconfiguration may finish
after its caller's timeout. The snapshot reports the configured limit. Resident `model` values are native
router section IDs; timestamps are RFC 3339 UTC strings. `since` is the oldest
in-flight request's start (zero time when none); `idle_unload_at` is null when
unloading is disabled or no model is resident. The initial activity time is the
service's startup baseline; later observations report inference start/end times.
The projected idle deadline is deferred while inference/native generation runs.

Named client secrets are random, shown once on creation/rotation and stored only
as SHA-256 hashes using the control-key hash helper. Names are required (up to
128 bytes). `allowed_models: []` permits all models; otherwise inference outside
the permitted selection receives **403 `model_not_allowed`**, including attempts
through the current-model alias. `max_concurrency: 0` and
`requests_per_minute: 0` mean unlimited; finite limits count queued and active
inference requests and use a rolling one-minute window. Exceeding a limit returns
**429** with `Retry-After`. Limits are local to the running inference process and
reset on restart; usage counters persist. Revocation/rotation affects subsequent
requests and leaves already admitted streams intact. Deleting the last named
key does not reopen a previously keyless loopback front; create another key or
configure the legacy secret through the control plane.

The legacy inference secret remains identity **`default`**, with editable limits
and usage. Rotating it updates the legacy secret; removing it still follows the
existing protection against keyless LAN exposure. Loom's local internal calls
use identity **`loom`**, outside the named-key list and its quotas, authenticated
with a private installation capability shared by the web/serve processes. That
capability is never forwarded to an external provider or linked engine. Linked
engines receive their explicitly configured inference credential and priority.

`usage.requests` counts admitted inference attempts, including engine-busy and
backend failures. Token counters add only values reported in a non-stream JSON
`usage` object, an SSE usage chunk (request `stream_options.include_usage`), or
llama.cpp `timings` when no usage is reported. Usage takes precedence over timings;
unreported tokens are not estimated. `last_error` is a bounded error code from
the latest completed request, cleared by success. Key lists never return hashes
or secrets. `endpoint` is the OpenAI base URL clients should use, including `/v1`
(and the node's public origin for a Loom node).

## Repairing llama.cpp acceleration

**Engine → Acceleration** reports devices observed from the configured
`llama-server --list-devices`, or **none** when enumeration succeeds without
GPU devices. A failed or missing binary is **unknown**, with the failure shown
alongside it. **Planned build backend** describes the compiler plan separately.
For an unhealthy source installation, **Rebuild** requests a clean build through
`POST /api/llamacpp/update` with `{"clean":true}`. Official binaries use the
release download/update action instead of the source update route.

An ordinary source update checks the binary and CMake cache even when Git has
no new commits. A missing binary, missing planned CUDA/ROCm/Metal/Vulkan device,
failed device probe, or mismatched/relocated CMake cache triggers a clean build.
Successful GPU repairs report **rebuilt to restore GPU support**. A healthy
current checkout stays **already up to date**. Completed jobs refresh the
acceleration display. Builds are verified before Loom reports success; an
incremental build failing verification gets one clean retry.

These checks apply to the web update job, CLI `loom llamacpp update`, existing
CLI/web source installs and custom forks in `backends/<name>`. CLI
`--dir`/`--no-switch` and custom per-model engine selection retain their scope.
For official releases, a same-version install verifies the selected release
variant and reinstalls an unhealthy binary. Opt-in automatic source/release
checks also recognize repairs, retaining their existing idle-only application
policy. Device observations bypass saved lists so replacement of a binary at
the same path cannot preserve an old acceleration claim.

`GET /api/llamacpp`, source `POST /api/llamacpp/check`, and
`POST /api/llamacpp/prebuilt/check` include an additive `health` object:
`{healthy,observed,devices,error?}`. `observed:false` means the device command
could not complete, rather than a proven absence of acceleration. Check routes
include unhealthy binaries in `needs_rebuild`/`update` advice. Sampling/preset,
model, discussion and inference API contracts are unchanged.

vLLM installation/update verifies that its environment can import vLLM and
PyTorch and enumerate a GPU using the expected CUDA or ROCm build. Same-version
automatic checks also report a broken environment in `auto_update.last_error`;
the Engine page displays it. These probes load no model and generate no tokens.
vLLM reports environment failures for explicit repair; it does not silently
reinstall a multi-gigabyte environment. A directly linked inference server
continues to own its installation; Loom can observe endpoint health but cannot
inspect its remote binary, CMake cache or Python environment.
