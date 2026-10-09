# Local voice engine and Jarvis API (6.1–6.3)

Loom manages **sherpa-onnx 1.13.8** separately from the LLM engine. Voice runs on
this machine or a paired Loom Node advertising `voice`. It does not change the
selected LLM, create a discussion, record a microphone, select audio devices,
run wake-word detection, or call a speech provider. Browser voice mode is 6.2.
The Voice page belongs in Models › Voix; this slice supplies its backend contract.

## Installation and trust

Engine installations live under `voice/sherpa-onnx/<version>/<cpu|cuda>/` in
Loom's data directory. Models live under `voice/models/<catalog-id>/`. Downloads
use the official k2-fsa GitHub releases. CPU is the default. CUDA requires both
an observed NVIDIA GPU (`nvidia-smi`) and an explicit `cuda:true` request on the
machine doing the install. Unsupported platforms/builds return an error.

The embedded [catalog](../internal/loom/voice/catalog.json) pins model asset
names, URLs, compressed byte sizes and SHA-256 digests. Digests come from
GitHub's published asset digests, cross-checked against a local download; the
three older assets without a published digest (English streaming zipformer,
Silero VAD v5, the KWS zipformer) were downloaded and hashed. A missing digest
(`TODO_SHA256`) makes an artifact uninstallable: Loom refuses before downloading
it, and a pack preflights all its digests before starting. There is
no trust-on-first-use, unverified install, hash override API or silent fallback.

Other engine platform entries pin the platform/provider hash. Once populated,
Loom obtains the asset name, URL and byte size from the official **fixed
v1.13.8** release metadata, accepting exactly one matching shared-library build.
Universal macOS builds can serve either native architecture. An ambiguous or
absent build fails explicitly. Release sizes check transport
integrity; the embedded digest is the content authority. No `latest` lookup is
used. Engine platform rows cover Linux amd64/arm64, macOS amd64/arm64, and
Windows amd64/arm64; actual CUDA asset availability is checked against the
release. macOS uses CPU.

Each artifact downloads into a private temporary directory. Loom checks HTTP
status, exact byte count, digest, safe extraction (tar.bz2/zip), required model
files, the shared C API library and the native `sherpa-onnx-version` result before publishing it. Missing
or ambiguous model file patterns fail. Symlinks must remain inside the staged
installation. Cancellation removes staging and leaves the installed copy in
place. The previous installation is retained for rollback; changing CPU/CUDA
retains both installations and the previous selection. No old version directories
are pruned. Only verified, fully published models enter the library.

A pack installs its models, installs/selects the engine, then saves its STT/TTS/
VAD configuration. A failed pack retains already verified individual downloads
but does not save an incomplete pack configuration. Installations and deletion
require the service to be stopped; downloads, configuration and service starts
cannot overlap. Deleting a selected model requires deselecting it first.

## Curated packs

Sizes below are compressed downloads from the supplied official release
catalogs, excluding the engine. Installed usage is measured separately.

| Pack | STT | TTS | Why |
| --- | --- | --- | --- |
| `fr-cpu-small` | Kroko streaming Zipformer French, 57.2 MB | Piper Siwis French int8, 20.9 MB | Small streaming French recognizer and modest CPU synthesis. |
| `en-cpu-small` | Streaming Zipformer English 2023-06-26, 310.4 MB archive | Piper Lessac English int8, 21.0 MB | Streaming recognition with int8 encoder/joiner; the upstream archive also contains other variants. |
| `multi-cpu` | NeMo multilingual fast Conformer CTC int8, 102.3 MB | Kokoro multi-language v1.1 int8, 147.0 MB | Smaller multilingual recognizer; VAD supplies utterance boundaries. Kokoro offers a quality alternative to Piper. |
| `fr-gpu` | Parakeet TDT 0.6B v3 int8, 487.2 MB | Kokoro multi-language v1.1 int8, 147.0 MB | Larger multilingual STT for an opted-in NVIDIA machine. |

Every pack includes **Silero VAD v5**, 2.31 MB. The library also offers an
optional English Zipformer KWS model, 17.63 MB; installation does not enable
wake-word execution in 6.1. NeMo CTC supports be/de/en/es/fr/hr/it/pl/ru/uk.
Parakeet is multilingual. Moonshine tiny English is smaller but does not cover
French; the streaming English pack prioritizes incremental recognition. Qwen3
is not part of the default packs. Archive contents retain upstream model notices;
engine Apache-2.0 licensing does not replace individual model licenses.

## Supervision and inference

Voice additionally requires **Python 3**, with standard-library `ctypes` only.
No Python package, pip operation, Go dependency, compiler or model binding is
installed. Go supervises two long-running workers loading the installed sherpa
shared C API: STT and TTS. The ABI declarations are pinned to 1.13.8. The worker
runs Python in isolated mode and loads no user Python configuration.

This uses the prebuilt engine's actual inference implementation. The inspected
prebuilt STT WebSocket executables expose `--port` but no loopback bind option;
the offline TTS CLI reloads its model each invocation. Resident workers keep
models loaded and deliver sherpa's TTS callback chunks immediately. Workers
communicate over private stdin/stdout and **open no listener ports**. The only
voice WebSocket is Loom's existing authenticated HTTP listener, including the
paired node listener. There is no unauthenticated sherpa listener on a LAN.

`config.threads` reaches both workers' startup JSON and the native online/offline
recognizer `model_config.num_threads` and `OfflineTtsModelConfig.num_threads`.
There is no separate STT server or `--num-threads` argv in this implementation:
the C API field is its equivalent. Changing the configuration while idle replaces
previously resident workers; stopped workers remain stopped until requested.
TTS always uses CPU. Thread counts do not guarantee proportional speedups.

The pinned [Kokoro implementation](https://github.com/k2-fsa/sherpa-onnx/blob/v1.13.8/sherpa-onnx/csrc/offline-tts-kokoro-impl.h)
and [Piper/VITS implementation](https://github.com/k2-fsa/sherpa-onnx/blob/v1.13.8/sherpa-onnx/csrc/offline-tts-vits-impl.h)
invoke the generation callback **after a sentence batch has been synthesized**.
Loom sets `max_num_sentences:1`, converts each callback immediately to PCM16,
and flushes transport chunks of at most 3,200 samples before native generation
continues. A multi-sentence request can play the first batch before later batches
finish. One long sentence still waits for its entire native synthesis: these
models do not expose PCM during that ONNX run. Splitting transport chunks does
not reduce that wait. Loom does not split text artificially or claim sub-sentence
streaming; the native frontend chooses sentence boundaries.

Streaming Zipformer owns partial recognition and native endpointing. Non-streaming
models use the native Silero detector and offline recognizer; they emit final
transcripts at VAD boundaries, without invented partials. VAD threshold/minimum
speech controls apply to that offline path. Streaming endpoint silence maps to
native endpoint rules. TTS runs Piper/Kokoro int8 on CPU even with a CUDA STT
installation, keeping LLM GPU residency independent. `voice` is the native
numeric speaker ID; unavailable speaker IDs are rejected by the worker.

Idle unloading defaults to 30 minutes; zero disables it. An open voice stream or
active request protects residency. Observation never starts inference. A request
may load a stopped worker lazily. Stop/restart and engine configuration changes
refuse while requests are in flight. Barge-in cancels only TTS, suppresses stale audio,
and kills/reaps that owned worker; the next speech request reloads it. STT stays
resident. Failed workers recover on a subsequent request. Native stderr logs are
bounded to 32 KiB per worker; protocol text/audio is not added to the logs.
Processes and downloads are stopped when Loom exits.

Startup › **Voice engine at boot** saves an independent boot option, exposed
only when the engine and STT/TTS/VAD models are installed. It starts when the
selected machine's Loom web/node service starts, without recording or generation.
The existing Startup systemd management remains Linux-only. On other systems,
`config.boot` still applies when Loom starts. Nodes advertise voice by default;
`loom node init --no-voice` disables it and `--no-voice=false` restores it. Use the
same node home, restart the node and refresh/re-pair the machine's advertised
modules, as with the other node modules.

Doctor observes installation, native version, selected model presence and worker
reachability. A health probe skips workers already in use and never kills or
restarts them on timeout. A stopped/uninstalled voice engine is `skip`, not a failure; absent
selected models are `warn`. Remote checks run on the selected voice node.
`voice.install` authorizes engine/model/pack installs and rollback on that machine,
using the existing policy broker. Explicit local actions default to allow;
user rules can confirm or deny them. `data.send_provider` is unchanged.

## HTTP contract for the Voice page

All routes require Loom control authentication (browser session or control key),
use `Cache-Control: no-store`, and return `{ok:false,error}` on errors. Nodes
require their paired machine bearer token and reject browser origins; cookies
do not authorize requests.
No credentials appear in voice status. GET is inert; mutations require POST.

`GET /api/voice/node` returns
`{ok:true,machine:"local",machines:[{id:"local",name:"Local"},{id,name}]}`.
`POST /api/voice/node {machine:"local"|"saved-machine-id"}` persists the voice
execution target. Only paired machines advertising `voice` qualify. Append
`?machine=local|saved-machine-id` to other routes to manage a machine without
changing that selection. These choices do not affect the LLM engine link.

`GET /api/voice`:

```json
{
  "ok": true,
  "engine": {
    "id": "sherpa-onnx", "version": "1.13.8", "installed": false,
    "provider": "cpu", "cpu_installable": true, "nvidia": false,
    "selection": {"version":"1.13.8","provider":"cpu"}
  },
  "config": {
    "stt":"", "tts":"", "vad":"", "voice":0, "speed":1,
    "language":"en", "threads":2, "vad_threshold":0.5,
    "endpoint_silence":0.7, "min_speech":0.25,
    "idle_unload_minutes":30, "boot":false
  },
  "service": {
    "running":false, "stt_running":false, "tts_running":false,
    "in_flight":0, "last_used":"0001-01-01T00:00:00Z",
    "error":"", "log":[]
  },
  "download": {
    "running":false, "phase":"", "artifact":"",
    "received":0, "total":0, "error":""
  },
  "disk_bytes":0,
  "capabilities":["stt","tts","vad","stream","cancel","bench"],
  "runtime_requires":"python3"
}
```

`POST /api/voice` sends the **complete config object** above. Send empty model
IDs to deselect. It validates installed models and parameter ranges and refuses
changes during requests or downloads. An idle service restarts its resident
workers with changed parameters, including `threads`. If reloading fails, the
saved config remains and the service is stopped; POST reports the error.
`boot:true` requires all
three model selections. `last_used` is a timestamp, with the zero timestamp
meaning never used; status `version` is the curated release, while Doctor executes
the installed version binary. `selection` may also contain `previous_version`,
`previous_provider`, and `previous_copy` (same release/provider retained copy).

| Route | Request → response |
| --- | --- |
| `GET /api/voice/models?kind=stt&language=fr&max_size=100000000` | `{ok,models:[{id,kind,languages,size,streaming,family,installed,disk_bytes,installable,sha256,url}],disk_bytes}`. `kind` is stt/tts/vad/kws; sizes are bytes. The curated library includes supported inference families and optional KWS. |
| `POST /api/voice/models/download` | `{id}` → HTTP 202 `{ok,download}`. |
| `POST /api/voice/models/delete` | `{id}` → `{ok}`. Catalog IDs only. |
| `POST /api/voice/install` | `{cuda:false}` → HTTP 202 `{ok,download}`. OS/arch are detected on the destination. |
| `POST /api/voice/install/rollback` | `{}` → `{ok}`. Switches to the previous verified installation. |
| `GET /api/voice/packs` | `{ok,packs:[{pack:{id,language,hardware,stt,tts,vad,kws,reason},size,installable,requires_nvidia}]}`. Pack size excludes the engine. |
| `POST /api/voice/packs` | `{id:"fr-cpu-small",cuda:false}` → HTTP 202 `{ok,download}`. CUDA packs require `cuda:true`. |
| `GET /api/voice/download/status` | `{ok,download:{running,phase,artifact,received,total,error}}`. Progress counts the current artifact's compressed bytes; phases: queued/downloading/extracting/complete/failed/cancelled. |
| `POST /api/voice/download/cancel` | `{}` → `{ok}`; poll until `running:false`. |
| `GET /api/voice/service` | `{ok,service}` using the status shape above. |
| `POST /api/voice/service` | `{action:"start"|"stop"|"restart"}` → `{ok,service}`. |
| `POST /api/voice/test/tts` | `{text}` → `audio/wav`, PCM16 mono at the model's sample rate. Text: 1–4096 bytes. |
| `POST /api/voice/test/stt` | Raw `audio/wav` body, PCM16 mono 16 kHz, at most 60 seconds → `{ok,text}`. This is a raw upload, not multipart/JSON. |
| `POST /api/voice/bench` | `{}` → `{ok,sample_text,sample_source,tts_audio_seconds,stt_audio_seconds,tts:{latency_ms,first_audio_ms,real_time_factor},stt:{latency_ms,real_time_factor,text}}`. |
| `GET /api/voice/doctor` | `{ok,checks:{installed:{status,detail,fix?},version:{…},models:{…},service:{…}}}`. |
| `GET/POST /api/startup` | Existing shape plus `voice:{boot,installed,ready}`; POST `{voice_boot:true}` toggles only that machine's voice boot option. Existing `/api/machines/{id}/node/startup` proxies it. |

The benchmark uses a fixed sentence in English/French for TTS and a fixed
embedded English/French WAV for STT (generated with eSpeak NG at 155 words/minute,
converted to PCM16 16 kHz mono). `sample_source` is
`embedded_espeak_ng_fixture`. RTF is elapsed seconds / audio seconds, not the
LLM's token throughput. TTS reports `first_audio_ms` from request start to the
first nonempty PCM callback received by Go, including conversion/worker transport;
the sample-rate announcement (`audio_start` on the WebSocket) is not first audio.
`latency_ms` measures total completion time. The fixed single-sentence bench can
therefore report nearly equal first-audio and total times; it does not prove
sub-sentence streaming or measure browser playback/network latency. These are
local diagnostic timings; they include a cold load when the worker is stopped.
No microphone or external provider is used.

## Streaming contract for 6.2

Open authenticated WebSocket `/api/voice/stream`, optionally with `?machine=…`.
The server announces:

```json
{"type":"ready","input":{"format":"pcm_s16le","sample_rate":16000,"channels":1},"stt_streaming":true}
```

- Send binary PCM16 little-endian frames (16 kHz mono, at most 64 KiB/frame).
  Results are JSON `{type:"partial"|"final",text}`. Only streaming models emit
  partials. Finish each utterance within 60 seconds or a native final boundary.
- Send `{"type":"finish"}` to flush STT. The server emits remaining finals and
  `{"type":"stt_done"}`; another utterance can follow on the same connection.
- Send `{"type":"speak","id":"utterance-1","text":"Bonjour"}`. The server
  emits `{type:"audio_start",id,format:"pcm_s16le",sample_rate,channels:1}`,
  binary PCM chunks, then `{type:"audio_end",id}`. Output rate is model-native.
  Speech IDs are at most 80 bytes. Only one speech generation may be active.
  Jarvis speech can attach `"voice":{"tts_model":"piper-fr","voice_id":0,"speed":1.2}`
  from the session response (below). Omitted/empty `tts_model` and omitted/null
  `voice_id`/`speed` inherit the selected machine's engine config. Overrides apply
  only to this request and are forwarded unchanged to a paired voice node.
  The TTS model must be installed there; an unavailable model returns an error.
  Model changes replace only an idle TTS worker, retain STT, and refuse while TTS
  is in use. Ordinary `speak`, WAV tests and benchmarks use engine defaults.
- Send `{"type":"cancel","id":"utterance-1"}` or `barge_in`. The server
  cancels the current speech and emits `{type:"cancelled",id}`. No old audio
  follows that acknowledgement. STT continues; cancel before submitting new speech.
- `{"type":"ping"}` → `{"type":"pong"}`. Errors are `{type:"error",error,id?}`.

This protocol contains no discussion/model-provider context. PCM and TTS text
are processed on the selected machine only. Browser origin checks and session
revocation use the same control security as existing streams. Slow writes have
five-second deadlines; inference requests and PCM frames are bounded. STT
streams have an eight-session native limit. Disconnects release native streams.

## Validation and remaining acceptance

Fake binaries/downloads test hashes, size, staging, rollback, unsafe extraction,
missing model files, cancellation, model filters, WAV uploads, benchmark shapes,
policy denial, boot settings, Doctor, supervision, idle unload and restart.
In-memory HTTP/WebSocket tests exercise PCM, partial/final transcripts, TTS chunks,
barge-in and paired-node credentials without opening network sockets. The worker
C API symbols, all 46 struct sizes and field offsets were also checked against the supplied 1.13.8 Linux shared library.
Real model inference, CUDA and macOS/Windows acceptance remain pending the reviewed
model/platform hashes and installed runtime on those machines.

## Jarvis 6.2: isolated spoken conversations

This backend supplies APIs for the design owner's full-screen voice view. The
browser connects the existing `/api/voice/stream` STT/TTS WebSocket to the Jarvis
text endpoints below: final STT text starts a Jarvis turn, streamed answer text
can be synthesized with the WebSocket's `speak` command. Playback, microphone
selection, animation, push-to-talk, VAD interaction and closing the view belong
to that UI. These APIs do not record audio or start a harness.

A voice conversation lives only in RAM and belongs to its authenticated browser
session or control credential generation. Logout, credential rotation and vault
locking revoke access. A session accepts at most **200 completed user/assistant
exchanges**, expires after **two idle hours**, and allows one turn at a time.
There are at most 64 sessions per Loom process. Restart discards them. User text
is 1–4096 UTF-8 bytes without NUL; answers are limited to 16 KiB and requests to
three minutes. Failed/cancelled turns do not enter session history. No automatic
retry or discussion write occurs during a turn.

All routes use the main Loom control authentication and `Cache-Control: no-store`.
They stay on the main host even when speech is routed to a paired voice node.
POST requires `Content-Type: application/json`; unknown fields are rejected.

### Jarvis profile (6.3)

`GET /api/voice/jarvis` returns:

```json
{
  "ok": true,
  "name": "Jarvis",
  "language": "auto",
  "personality": "",
  "length": "short",
  "formality": "tu",
  "model": "discussion",
  "fallback": "",
  "context": "discussion+memory",
  "voice": {"tts_model":"", "voice_id":null, "speed":null},
  "routes": [
    {
      "id": "local:example.gguf", "name": "example.gguf", "kind": "local",
      "provider_name": "llama.cpp", "model": "example.gguf",
      "enabled": true, "ready": true
    }
  ]
}
```

`routes` contains the existing model picker's `ModelChoice` objects, restricted
to local/cloud routes. Cloud IDs are opaque `cloud:…` values; use the returned
IDs, not a provider/model string invented by the browser. Optional picker fields
include `engine_value`, `provider_id`, `endpoint` and `via`. Hidden routes remain
listed with `enabled:false`; this flag controls picker visibility. `ready` is
an observation, not a guarantee of successful inference.

`POST /api/voice/jarvis {"model":"discussion","fallback":"local:example.gguf"}`
saves both settings and returns the same shape as GET. All profile fields can
be sent together or partially; omitted fields retain their saved values, so older
model/fallback clients preserve the profile. Empty defaulted strings restore
their defaults; `personality:""` clears the persona. Unknown fields and invalid
values return HTTP 400 without saving. `model` accepts
`discussion` or a returned local/cloud route; `fallback` accepts a returned route
or `""`. They persist as `voice.jarvis_model` and `voice.jarvis_fallback`.
Selecting settings does not grant permission to send data.

| Profile field | Default and meaning |
| --- | --- |
| `name` | `Jarvis`; at most 80 Unicode characters. Used as the spoken identity; retained for future wake-word work, with no wake detector started here. |
| `language` | `auto` follows the user; otherwise a language code such as `fr`, `en`, `fr-FR` or `zh-Hant` (at most 16 bytes). This controls the answer prompt; STT/TTS language and supported voices remain engine/model choices per machine. |
| `personality` | Empty; free user text up to 1,000 Unicode characters. Control/format characters are removed (newline/tab retained as data), then JSON-quoted under `Personality:` in the speech prompt. Explicit prompt rules treat it as untrusted style text and reject role/tool/permission/rule changes; no tools are granted by persona text. |
| `length` | `short`: 1–2 sentences unless asked, `max_tokens:160`; `normal`: concise explanation, cap 400; `detailed`: fuller spoken explanation, cap 900. Provider enforcement/tokenization remains native. |
| `formality` | `tu` or `vous` when speaking French, neutral register in other languages. Default `tu`. |
| `context` | `discussion+memory`: bounded recent discussion and existing Brain memory; `memory`: Brain memory only; `none`: neither. The voice session's completed exchanges always remain. Discussion metadata still resolves the model and project scope. |
| `voice` | `tts_model:""`, `voice_id:null`, `speed:null` inherit the selected machine's `/api/voice` config at speech time. A TTS catalog ID, numeric speaker ID 0–1024, or speed 0.25–4 overrides only Jarvis speech. Model installation and actual speaker range are checked on the speech machine; no automatic install/fallback occurs. |

The profile persists through the existing settings store under `voice.jarvis_*`;
the original model/fallback keys are unchanged and GET fills missing defaults.
Reset voice inheritance with `{"voice":{"tts_model":"","voice_id":null,"speed":null}}`.
The profile page is built separately. Its voice flow sends the returned session
`voice` object with each Jarvis `speak` command, including on paired nodes.

With `discussion`, a Loom-held local/cloud discussion supplies its own model
route. A harness discussion or absent discussion uses `fallback`. An empty or
missing fallback returns an actionable error; a missing discussion model also
returns an error. Explicit routes always use their selected model. Jarvis never
uses harnesses, including harnesses with a Loom model association. Local requests
use the existing engine's Chat Completions endpoint and residency/loading policy;
cloud requests use the existing provider completion client, with no tools.

### Create and stream

`POST /api/voice/jarvis/session` accepts:

```json
{"discussion_id":"discussion-id"}
```

Omit `discussion_id` to start without a discussion. Response:

```json
{"ok":true,"session_id":"opaque-session-id","model":{"route":"local:example.gguf","label":"example.gguf"},"voice":{"tts_model":"","voice_id":null,"speed":null}}
```

The model route and profile are pinned for that session; reopen voice mode to
apply profile edits. With default `context`, Jarvis reads Brain's same bounded
memory index, user profile and relevant topic block used by Loom-held chats,
scoped to the originating project. It reads recent visible discussion text with
a 6 KiB budget allocated newest first, then presented chronologically, plus its
own completed voice exchanges. Tool state and harness-private context are not
included; reading does not capture or change a discussion's frozen snapshot.
The speech system prompt follows profile language, length, French formality and
personality, with natural numbers and no Markdown, lists, code blocks or emojis. Jarvis tells the
user to use an agent in the discussion for tasks requiring tools.

`POST /api/voice/jarvis/turn` accepts:

```json
{"session_id":"opaque-session-id","text":"Bonjour Jarvis"}
```

It returns `Content-Type: text/event-stream`. Each SSE frame is a JSON `data`
frame with no named SSE event:

```text
data: {"type":"delta","text":"Bonjour. "}

data: {"type":"delta","text":"Comment puis-je vous aider ?"}

data: {"type":"done","text":"Bonjour. Comment puis-je vous aider ?"}

```

On an execution/policy error the terminal frame is instead:

```text
data: {"type":"error","error":"confirmation required for capability: data.send_provider; authorize this destination in policy settings"}

```

Closing/aborting the request cancels inference. Validation, missing/foreign
sessions and busy/limit errors happen before streaming and return JSON
`{"ok":false,"error":"…"}` with HTTP 400/404/409 respectively. Authentication
and vault errors retain the existing 401/403/423/503 contracts. No metrics or
hidden reasoning are synthesized. Clients must not resubmit a completed turn.

`data.send_provider` is evaluated before any context leaves Loom. A cloud
route already selected for the originating cloud discussion retains that
existing exact-destination consent; another cloud route needs an existing
policy allow grant. An explicit confirm or deny rule always applies. Linked
engine destinations also need consent. A confirm decision returns immediately
as an SSE error and does not create an approval task or grant. Provider spending
policy also applies. Redirects are rejected, and upstream error bodies are not
returned. Only the selected destination's credential is sent.

### Close and optional injection

`POST /api/voice/jarvis/end` accepts:

```json
{"session_id":"opaque-session-id","inject":true}
```

Response is `{"ok":true}`. With `inject:true` and an originating discussion,
the entire completed exchange is appended as ordinary user/assistant messages,
each with `source:"voice"` and an opaque `source_id` for retry deduplication.
Both native journals/archives and workspace sessions are persisted and published
through their existing discussion subscriptions. Their Markdown transcripts and
Brain consolidation see this text like other messages. Display provenance is
removed from inference requests.

No model turn, agent session, tool action or model selection is triggered.
`inject:false`, or a session with no discussion, writes no messages. Successful
end deletes the in-memory session. Repeating end for an ended/expired/unknown ID
returns `{"ok":true}` without writing anything. A foreign live session returns
404. Ending while a voice turn or destination discussion is busy returns 409;
abort the turn or wait, then retry. A persistence error keeps the voice session
available for retry, with durable source IDs preventing duplicate messages.

### HTTPS for browser microphones

Browsers expose microphones in secure contexts: HTTPS, or recognized loopback
hosts such as `localhost`. Plain LAN HTTP remains a preview. Loom can serve the
same handlers, browser login, cookies, control authentication and origin checks
on an additional HTTPS listener. It uses the running HTTP listener's bind host;
it does not independently expose loopback Loom to the LAN. Existing network
exposure rules still require a password/control key. `/api/network/web` controls
the web bind address; `/api/network` remains the engine API's network control.
Restart Loom after changing the web host.

`GET /api/https` is inert and returns:

```json
{
  "ok": true, "enabled": false, "port": 2543, "mode": "self-signed",
  "urls": ["https://127.0.0.1:2543"],
  "fingerprint_sha256": "", "running": false
}
```

`urls` reflects the listener bind host (LAN names/addresses for an all-interface
bind). `fingerprint_sha256` is the lowercase hexadecimal SHA-256 of the leaf
certificate DER, or empty if no readable certificate exists. A startup failure
adds `error`; `enabled` is saved intent while `running` is observed listener
state. GET never generates a certificate or starts a listener.

`POST /api/https` sends the full listener configuration:

```json
{"enabled":true,"port":2543,"mode":"self-signed"}
```

It returns the same status shape. Settings persist as `https.enabled`,
`https.port`, `https.mode`, `https.cert_path`, `https.key_path`. Defaults are
HTTPS off, port 2543 and `self-signed`. Enabling/disabling, changing ports and
regenerating certificates apply live in `loom web` and the desktop app; bind/certificate failures
return 409 and preserve the previous listener settings. Tunnel-only serving
cannot start this local listener.

Self-signed mode generates an ECDSA P-256 certificate once, valid for 825 days,
with SANs for the hostnames, localhost, loopback addresses and all observed
private LAN IP addresses. Certificate and private key are stored together in
Loom's private sealed secret store, never in API JSON. New addresses require an
explicit regeneration:

```json
{"enabled":true,"port":2543,"mode":"self-signed","regenerate":true}
```

This changes the fingerprint and persists the new identity. It updates the
certificate used by new TLS connections without dropping the request carrying
the update. Corruption does not silently regenerate an identity. The UI can
guide users: **Open the returned https://… URL and accept the certificate once.**
Self-signed trust depends on the browser/device; it is not public-CA trust.

Files mode loads an existing matching PEM certificate/key, for example output
from `tailscale cert`, using absolute paths on the Loom host:

```json
{"enabled":true,"port":2543,"mode":"files","cert_paths":{"cert":"/path/to/host.crt","key":"/path/to/host.key"}}
```

The response includes `cert_paths`. Loom does not issue, renew or rewrite these
files; POST again or restart after replacing them. `regenerate` is restricted to
self-signed mode. Port zero/omitted selects 2543; other valid ports are 1–65535.
No new firewall rule or public tunnel is created.

Doctor's read-only `security.https` check is `ok` for a running HTTPS listener or
running loopback HTTP origin, `warn` when voice is installed/selected and neither
exists, and `skip` when voice is absent. Real mobile microphone, certificate
trust and speech/model performance acceptance remains separate from synthetic
backend tests.
