# Loom workspace: conversations, models and context

## Accepted product contract — clarified 2026-09-26, continued 2026-09-27

A discussion belongs to Loom, not to a provider. Its portable transcript and
explicit project context survive a change of local model, cloud model or future
harness. Local/cloud discussion silos from the intermediate prototype are
superseded. No parallel memory engine is introduced.

- **Discussions:** one history and one model/execution selector; each answer
  records the runtime and model that produced it.
- **Models:** local GGUF library and Hub, cloud models and provider connections.
  Users choose which models appear in the discussion selector.
- **Harnesses:** configurations and model associations from the same catalog.
  A harness is an executor, not a provider or a second discussion app.
- **Projects / Skills:** explicit shared instructions, selected instruction skills
  and an optional directory reference.
- **Connections:** external tools/MCP, separate from model providers.
- **Services / Settings:** existing engine, hardware and settings.

Lucas's latest correction restores the original discussion UI, composer and
model/preset picker. The original side panel keeps its full local parameter
editor; cloud and preparatory harness selections change its contents, not its
layout. The redundant workspace header and second simplified chat are removed.

## Existing architecture preserved

Native Go binary, bbolt state, plain ordered JavaScript, generated embedded HTML.
llama.cpp remains an external owned process. Library, presets, Hub, server,
proxy, benchmarking, MCP and the original local chat remain in place.

The historical local conversation is a singleton with richer tool/attachment
events. The common discussion store is additive, not a destructive migration.
Opening a historical discussion restores its native archive directly, as before.
Selecting cloud projects user/assistant **text** into the common record and keeps
the source archive. System prompts, attachments, tool protocol messages and
private reasoning are not claimed as portable.

## Implemented common discussion path

`RuntimeSession` stores the Loom transcript, project reference, current route,
per-turn model attribution, usage if reported, state and request IDs. Changing
the selected model updates the route **on that same record**; it neither clears
nor forks messages. Route changes are rejected while a reply is active.

Each discussion also owns an optional explicit title and additional instructions.
Its project can be changed or detached without moving, deleting or rewriting
messages. Changing cloud-bound instructions/project requires confirmation.
Detaching a project removes its instructions and skills from future prompts, not
text already exchanged in the history. A missing/locked project blocks sending
until the user chooses an accessible project or explicitly detaches the thread.

The original local Conversation pipeline remains the primary local executor,
including tools, attachments, model/global prompts, presets and compaction.
`workspace_native.go` binds a common discussion to a separate native archive,
copies a compatible source journal without deleting it, and appends intervening
cloud text without replaying tools. Completed native history projects its visible
text back into the same common record. The display journal, not the compacted
model view, supplies this text. Diverged archives are retained instead of overwritten.
Running native turns block related route/context changes. Imported portable text
is rendered inert even after native replay/coalescing. The underlying text-only
local adapter remains available for unbound API discussions and delegates to
`runChat`. No llama.cpp execution layer is reimplemented.
The engine remains shared: loading a different GGUF changes that one process.

The cloud adapter implements a bounded Chat Completions SSE client:
explicit base URL and model; no automatic provider discovery, fallback,
local tools or hidden retry; no redirects; HTTPS required except explicit
loopback HTTP for development. Upstream errors are sanitized rather than
echoing provider bodies. UI progress uses authenticated polling of the live
session, so navigation/reconnection does not resubmit a paid request.

Provider records contain multiple model IDs and no credential. Credentials
exist only in server memory until disconnect/restart. Destination URLs cannot
be edited in place; new connections are required. A discussion's current
route is changed only through an explicit user selection, with confirmation
before a cloud transfer. `ready` means an in-memory key is present, not that the
account, model, quota or protocol has been validated. No real provider or paid
model has been tested here.

Models → Providers supplies URL presets for OpenAI, OpenRouter and Mistral,
plus a custom endpoint. An explicit catalog probe sends only a Bearer key to
that fixed destination's GET `/models`; it sends no discussion and performs no
generation. It does not save the key, connection or catalog. Users select IDs
to save (32 maximum) or enter them manually. Catalogs can include unsupported
non-chat models and may be publicly readable; discovery does not establish
authentication, account entitlements or Chat Completions compatibility.
`usage_mode: "none"` omits `stream_options` for endpoints that reject usage
requests. Other native provider protocols are not silently converted.

Shared model visibility is persistent and independent of model files/history.
Generic harness profiles persist selected model associations, but remain
**preparatory**: no compatibility/provider credential mapping is inferred from
a model's presence. Antigravity has a separate opt-in native catalog and an
experimental text bridge, described below; generic profiles do not execute it.

### Antigravity and usage slice — 2026-09-27

`workspace_antigravity.go` launches installed `agy` with fixed argument arrays,
bounded NDJSON stdin/stdout, cancellation and redacted diagnostics. Model IDs
come from explicit `agy models` discovery. Authentication remains with the CLI;
Loom never reads its credential files or injects cloud-provider keys. Each turn
uses a freshly created temporary working directory, not the project directory.
This is **not a security sandbox**: global CLI settings, permissions, hooks and
native resources may still apply. No global settings are changed and Loom never
adds `--dangerously-skip-permissions`. The bridge requests text-only responses;
that instruction is not an enforcement boundary or full tool-disabling flag.
Headless approval requests follow native policy; there is no Loom approval RPC.

The prepared shared messages (instructions/selected instruction skills/history/
draft) are serialized as role/content JSON inside one native user prompt. Each
send creates a fresh native session rather than resuming private state or
injecting fake tool results. This sacrifices native cache/session reuse and may
increase prompt tokens. CLI reasoning/checkpoints/compaction remain upstream;
Loom adds no summarizer/embedding/AI management call. Native session IDs, reported
per-turn tokens and bounded tool name/state metadata are retained under the Loom
turn. Arguments/results are not exported except for a bounded `TargetFile` on
recognized native write/edit calls; observing a tool is not granting
permission or replaying it. Current acceptance is text-only, not agent tooling.

#### Reasoning selection and response observability

The UI groups only discovered Antigravity siblings ending in `-low`, `-medium`
or `-high`. Actual requests and persistence keep the exact native ID. A composer
select changes that ID in the same discussion without generation or history
mutation. Once the destination is already Antigravity, selecting another level
does not add another transfer-confirmation dialog. First external handoff still
requires explicit consent. Singletons such as `-thinking` are not guessed into
new model families. Existing per-variant visibility remains stored; explicitly
toggling a family affects all its variants. The harness catalog is collapsed.
Local hot effort reuses `/api/reasoning` and existing configured model levels;
unsupported cloud/provider effort controls are not fabricated.

Native per-step token reports update live display; terminal usage supersedes those
reports. Since this bridge starts fresh, terminal `duration_seconds` is one turn.
Average throughput is reported output tokens / total native duration, including
reasoning/startup/tool waits, **not** pure decode. Thinking tokens are a subset
of output; this CLI stream does not supply visible reasoning text, and Loom never
reconstructs it. Tool errors/refusals remain distinct from completed CLI steps.
A write target is metadata, not evidence of a verified on-disk diff. No tool
arguments, command text, file contents, raw output or private error text are shown.

`RuntimeTurnRecord` keeps usage/duration/start time and optional native stats.
Native `turn_done.runtime_turn` carries provenance solely in the display journal;
portable role/content projection is unchanged. Imported answers retain their
original runtime/model and metrics when switching back to llama.cpp. Startup
recovers already-known attribution for older workspace-bound journals without
guessing from the current model. Missing legacy fields remain unknown. Native
llama.cpp response metrics and parameter controls are retained; “Sans coût API”
does not estimate electricity/hardware cost.

Regression coverage includes stream metric/tool-error redaction fixtures, native
metadata replay/context separation, discovered model grouping and exact same-fil
effort selection. Browser QA uses disposable data for phone 320/390 px, landscape
844×390, grouped picker/harness catalog, native replay and keyboard confirmations.
No additional real-account generation or actual tool execution was needed for
this slice; those native approval/resource capabilities remain unvalidated.

`workspace_usage.go` separates account quota snapshots from retained Loom usage.
GET `/api/usage` reads cached data only, with `no-store` and vault-lock checks.
Explicit POST `/api/usage/refresh` invokes only `agy -p /usage` and `/credits`, or
a separate Codex app-server process with initialize/initialized and
`account/rateLimits/read`. No generation, login/logout, account nudges or reset
consumption. Reads are bounded and throttled to one attempt per runtime per
30 seconds. Cached snapshots are timestamped; failed refreshes mark old readings
as potentially stale. Missing percentages/reset dates/credits stay null.
Antigravity AI credits are not earned-reset credits. Codex multi-bucket data takes
precedence over its legacy single bucket; availableCount is authoritative, and
optional reset detail expiry dates are read-only. Claude Code/Pi/Hermes quota
readers are not integrated; no private billing endpoints are scraped.

Per-turn usage now survives subsequent sends. Older records can contribute only
their last reported usage; unknown turns are counted separately. Totals cover
retained Loom discussions, not the whole account or an immutable billing ledger.
Deleting a discussion removes its contribution. POST `/api/usage/price` saves
explicit manual USD/EUR input/output-per-million prices for cloud choices only.
The sheet shows a simple estimate on reported tokens, potentially incomplete,
excluding cache/volume discounts, tiers, taxes and other charges. It is **not** a
provider invoice. Opening/closing the modal neither polls accounts nor sends
messages. The original local controls/llama.cpp adapter remain untouched.

Real workstation acceptance: discovered 14 native models; two short synthetic
Loom sends on Gemini 3.8 Flash High in the same thread produced distinct native
session IDs and correctly retained a control word through the shared text.
Reported usage and real Antigravity/Codex quota reads succeeded. No actual agent
tool execution, native resumption, arbitrary provider binding, exported SKILL.md
or real cloud billing was validated.

Protocol references: [Antigravity headless](https://antigravity.google/docs/cli/headless/),
[permissions](https://antigravity.google/docs/permissions?tab=cli),
[Codex app-server](https://learn.chatgpt.com/docs/app-server).

## Context ownership and portability

| Owner | State | Shared across execution choices? |
| --- | --- | --- |
| Loom discussion | User/assistant text, route provenance | Yes |
| Loom project | Explicit instructions and selected instruction skills | Yes, assembled once for each turn |
| Runtime | KV cache, hidden reasoning, compaction internals | No |
| Native harness | Session ID, tool journal, approvals, native memory | Only through that harness's supported protocol |
| Project folder | Files on disk | Never implicitly read or transmitted |

The context section uses a fresh server snapshot, not stale project metadata from
the navigation. Its prepared-text dialog lists the initial system message,
portable history and optional draft, in order. Previewing does not contact a
runtime, persist the draft or consume model tokens. Native runtime templates and
internal processing (including the existing local retry/compaction behavior) are
not represented as portable state. It is an initial input preview, not a claim
that runtime internals never transform inputs.

`prepareDiscussion` assembles the portable preview and cloud/text-adapter
execution from the same rules. The original native local pipeline still adds its
model/global prompt and tool capabilities and owns its compaction; the portable
preview is not a native request dump. Native shared project/skills/discussion
instructions are reread when generating.
The server reads the instructions again at send time, checks a revision hash of
the route/history/context against the client's latest read, and records that hash,
instruction byte count and total input text bytes on the turn. A stale revision
rejects a new request before execution or history persistence. Retrying an already
accepted request ID still returns idempotently before revision checks. Editors
retain their opening revision to prevent overwriting another tab's changes.
No revision hash is a permission or credential. Stored history is not rewritten
with repeated system prompts. A folder reference grants no
filesystem permission. Switching to cloud can send **the entire portable
text history**, not just the next user message, so the UI explicitly confirms it.

Instruction skills are not tools or permissions. They are not yet exported as
filesystem SKILL.md resources. Project/skill/session records participate in the
existing optional encrypted-store migration; provider keys never enter it.

A future harness adapter needs a capability negotiation contract:

1. Can it start from a supplied text transcript, or only a seed prompt?
2. Can it resume a native session, and what Loom turn has that session consumed?
3. Can intervening Loom turns be appended without replaying tools?
4. Which instructions/skills and model bindings are supported?
5. Which approvals, cancellation, usage and native events must remain native?

Store native bindings **under** the Loom conversation, keyed by harness and
route, with a consumed-turn cursor. When a native session cannot accept changed
history, offer an explicit fresh-session handoff with a disclosed text/context
payload. Never pretend that importing text restores native memory, duplicate
tool calls by replay, silently summarize with a paid model, or promise zero
token/cost overhead. This native binding is a next step, not implemented today.

### Future native harness integration contract (not implemented)

- **Model binding:** distinguish harness-native account authentication from
  explicit API-compatible/local endpoints. An association in Loom's model
  catalog is only a preference until the adapter proves the harness accepts that
  endpoint, model ID and authentication mode. Negotiate capabilities per adapter;
  do not export all provider keys, change global harness profiles or claim every
  subscription model can use an arbitrary provider. Unsupported combinations
  stay visibly unavailable. Native authentication remains native unless a
  supported, explicitly approved credential handoff exists.
- **Skills:** current central skills are instruction text, not executable
  `SKILL.md` packages. Filesystem projection must account for each harness's
  discovery roots, format/version, bundled scripts and tool permissions. Prefer
  an opt-in isolated resource root or a supported extra-root API; never overwrite
  the user's global skills/configuration. A shared instruction is not a tool
  grant. Report precisely which resources the harness actually loaded.
- **Context:** keep the native session binding, consumed Loom-turn cursor and
  context revision under the conversation, specific to runtime/profile/model.
  Resume only when the native session and shared input agree. After intervening
  local/cloud turns or changed instructions, append through a documented native
  mechanism or offer a fresh-session handoff with the exact disclosed text.
  Do not replay tool calls, copy hidden state or auto-run paid summarization.
- **Events:** adapters should preserve native item IDs and normalize message/tool
  start, delta and completion, pending approvals, errors, cancellation and usage
  into one visible journal. Keep a redacted native metadata envelope when an
  event has no common representation. Approvals must still reach the native
  protocol; showing a tool card is not permission to execute it. Cancellation
  should interrupt the actual turn, not merely close Loom's stream.

This is the next adapter design boundary, not a universal harness schema or a
promise of identical memory, latency, token use or billing.

## UI and PWA boundary

The polish layer reuses the original typography, colors and control language.
It harmonizes workspace pages and light/dark surfaces without replacing the
chat/composer or the full three-tier local parameter editor. Mobile pages use
single-column cards, safe-area offsets, touch-sized actions and scrollable dialogs
with a reachable footer; landscape and tablet retain the original overlay panel.

The manifest and canonical brand PNGs provide an installed-app shell. Settings
shows browser-specific installation guidance and an action only when offered.
Service workers require a secure context (HTTPS, or recognized loopback for
development). Plain LAN HTTP is usable for preview but is not PWA installation.
No notification permission is automatically requested.

`sw.js` retains push handlers and caches only the public offline document and
the two listed manifest icons. Root navigation is network-first with that public
fallback on network failure. Conversation/app HTML, API JSON, SSE, POST and URLs
with query parameters are never intercepted for caching. No send queue or
background retry exists. Offline send guards preserve the live draft; this is
not durable offline storage or an offline conversation editor.

## APIs and limits

All new APIs use the existing web-auth boundary; POST requires JSON, rejects
unknown/trailing fields and caps requests at 128 KiB.

- GET /api/workspace: projects, skills, runtimes, models, providers, sessions,
  harness profiles.
- POST /api/projects/context; /api/capabilities/save; /api/capabilities/delete.
- GET /api/providers; POST /api/providers/save; /api/providers/disconnect.
- POST /api/providers/models: explicit consent, fixed saved destination when an
  ID is supplied, memory-only or submitted key, GET catalog with no redirects;
  15-second timeout, 3 MiB response and 2,048 raw entries maximum. Returned IDs
  are trimmed, deduplicated and sorted; malformed/oversized responses fail without
  echoing upstream bodies or credentials. No connection/model persistence.
- POST /api/workspace/models/visibility; /api/workspace/harnesses/save.
- GET /api/runtime/sessions (optional id): list or transcript/live state and
  current shared-context snapshot/revision.
- POST /api/runtime/sessions/create, select, import, send, stop, delete.
- POST /api/runtime/sessions/local: bind/restore the rich native local conversation;
  this does not load a model or call inference. The original local send/stop APIs
  and SSE subscription continue to own native execution.
- POST /api/runtime/sessions/configure: title, project and additional instructions;
  requires context_revision and cloud consent when changing shared context.
- GET /api/runtime/sessions/preview?id=… or POST with id/text for an optional draft:
  read-only prepared messages, byte/count limits and any validation problem.
  These responses use no-store. The send endpoint requires context_revision.

Limits: 32 models/provider or harness profile, 24,000 bytes/input, 200 outgoing
messages and 128 KiB total text **including instructions and draft**, bounded cloud response, four simultaneous
runtime replies and one common-path local reply. Projects retain the original
12,000-byte instruction and eight 8,000-byte skill limits. Discussion-specific
instructions have a separate 12,000-byte limit. Preview counts bytes, not tokens
or model context capacity. Oversized input is rejected, never silently cut. Context consumes
tokens; no identical behavior, price or latency across providers is claimed.

A restart marks unfinished turns interrupted; it never resubmits them.
Cancellation retains received text. Storage failures retain an unsaved result
in memory and expose a retry action. A crash can still lose non-checkpointed
streaming text; the accepted input is persisted before execution.

## Verification and next work

Tests cover local → cloud → local continuity in the same discussion, explicit
consent, route attribution, model visibility, non-destructive imports, invalid
harness associations, credential non-persistence, provider streaming errors,
redirect rejection, cancellation, replay protection and authenticated APIs.
All model executions in these tests use loopback fixtures. Additional tests check
preview/wire equality for local and cloud, changed skill/history/route rejection,
non-destructive project reassignment/detachment, deleted-project recovery, explicit
titles, nonportable input rejection, combined limits, preview non-persistence,
authenticated APIs and active-run edit rejection.

Native-bridge tests additionally cover original-archive preservation, retained
tools/reasoning/compaction, intervening cloud text, native text projection, no
cloud-route contamination and inert imported-text replay. Provider probe tests
check consent, fixed destinations, bounds, redirect rejection, sanitized failures
and non-persistence. PWA tests check valid canonical icons and public fallback.
Thirteen dependency-free Node UI/worker tests cover original DOM, three panel states,
send routing, key readiness, offline guards and the public-only cache boundary.
Run them with `node --test internal/loom/ui/tests/*.test.mjs`.

The Antigravity/usage slice also covers bounded CLI parsing, malformed/interrupted
streams, redacted errors, subprocess stdin/cancellation, explicit model consent,
nullable/multi-bucket account quotas, real zero vs unknown credits, incomplete
token totals and manual-price validation. The native local regression suite passes.
Browser acceptance covers AGY connect/model pick/send/panel/context retention and
quota refresh/closure/focus at desktop 1360×900, phone 390×844 and landscape 844×390.
No horizontal overflow; temporary viewport settings restored. The synthetic Loom
test discussion was removed after verification; native AGY history remains native.

Browser QA uses disposable data and a loopback synthetic provider: explicit
catalog selection, reconnecting a key after restart, switching models in the same
fil, streaming/usage, light/dark pages, phone 390×844, tablet 820×1180 and landscape
844×390. Stopping the preview server shows the public offline fallback; restarting
preserves the discussion and requires the key again. Viewport emulation is not
real-device keyboard, OS installation or second-device LAN acceptance.

Next: native harness binding/capability negotiation, rich content and opt-in
tool portability, native session resumption, project PTYs, a durable OS-backed
credential vault, and real-provider/real-model acceptance testing. These are
not delivered by a preparatory configuration screen.

Protocol reference: [Chat Completions](https://developers.openai.com/api/reference/resources/chat)
and [model catalog](https://developers.openai.com/api/reference/resources/models/methods/list),
[OpenRouter](https://openrouter.ai/docs/quickstart),
[Mistral migration guide](https://docs.mistral.ai/resources/migration-guides),
[service workers](https://developer.mozilla.org/en-US/docs/Web/API/Service_Worker_API).

## Runtime registry and optional actions

The local engine migration now also provides `Engine`/`llamaCppEngine` in
`engine.go` and a merged `ParamSpec` catalogue at authenticated GET
`/api/engine/params`. The local inspector reads row metadata/order/choices from
`params/llamacpp.json` plus installed help, retaining model-dependent controls
and the same visual components. Existing load/unload/estimate paths delegate to
the wrapper; router/service ownership, saved configuration, and native execution
remain in their original files. This does not implement a second
engine, new scheduling or the future package split. See architecture §2.1.

`workspace_runtime.go` registers the existing llama.cpp, Chat Completions,
Antigravity and Codex adapters in deterministic insertion order. Claude Code,
Pi and Hermes are descriptor-only adapters with empty capabilities. The registry
rejects duplicate IDs and capabilities claimed without the corresponding action
interface. Returned capability lists are detached snapshots.

The runtime descriptor carries `description`, `cli` and `consent` alongside the
existing fields. Harnesses renders that metadata from `/api/workspace`; Usage
uses the same declared quota capability instead of a separate runtime allowlist.
No native model, account or generation is contacted merely to list descriptors.

Optional `Connectable.Connect(ctx, consent)` discovers and saves a native model
catalog; its result retains each existing adapter's native `models` response for
legacy consumers. Optional `QuotaReader.Quota(ctx)` reads a native account without
generation. Generic endpoints are authenticated POST-only, no-store, strict JSON
and vault-checked:

- `/api/runtimes/{id}/connect` accepts `{"consent":true}`; missing consent stops
  before invoking the adapter. Unknown ID is 404; an unimplemented or
  non-connectable runtime is 501.
- `/api/runtimes/{id}/quota` accepts `{}` and uses the existing shared quota cache
  and 30-second throttle. Missing percentages remain null; a failed refresh
  preserves the earlier snapshot with an error/stale indication.
- `/api/workspace/antigravity/connect`, `/api/workspace/codex/connect` and
  `/api/usage/refresh` remain aliases with their existing catalog wire formats
  and body-based quota request. Unsupported legacy refresh IDs remain 400.

The already-present Codex text adapter uses its native app-server catalog and
an ephemeral read-only thread, disables native MCP for that process and declines
approval requests. Its protocol/stream/effort tests use synthetic fixtures here;
no new real-account generation is acceptance evidence for the registry change.

This first migration step leaves per-turn adapter configuration, existing native
execution, text portability and journal/polling behavior unchanged. Registry-based
connect/quota dispatch does not imply native resumption, transferable approvals,
universal model/provider bindings or any implementation of the planned adapters.


## Catalog inspection and automatic fitting

Clicking a local model or preset opens the shared selection drawer with its
parameters. Loaded selections edit the live configuration; other selections
remain drafts until an explicit load/apply. Cloud models show provider/model
identity and connection state; harnesses/models show only declared adapter
capabilities, native catalog and reported reasoning levels. Merely inspecting
never changes the discussion executor, connects a CLI or contacts a cloud API.
The drawer reuses existing CSS and supports keyboard opening, Escape, focus
containment and return to the trigger. Visibility/load/delete controls remain
separate. Preset loading uses the complete catalog index even after filtering.

Bare defaults use `FIT=on`; CTX/NGL stay unset for installed engines supporting
`--fit`. Explicit/remembered values win; unsupported binaries retain historical
arguments. UI estimates do not become saved automatic settings, and loading an
untouched draft does not create a remembered configuration. The context field
separates requested/native limits from `ctx_effective`, a nullable native
allocation per slot. Router observation requires the active section to be
loaded, uses `autoload=false`, short timeouts and selection checks; it does not
load a model or rewrite saved settings. Single-mode engines and temporary API
variants currently report unknown observed context.
