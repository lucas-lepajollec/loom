# Workspace architecture

## One discussion, several models

Loom owns the portable transcript, project and shared context. A turn uses Local
or Cloud. Loading an engine model changes engine state; choosing it in a
conversation changes that discussion's route. Never confuse an old native agent
using a model with direct inference through the same model.

Historical agent discussions remain readable. Their native event/state fields
are compatibility data only; choose Local/Cloud before continuing. See
[retirement](harness-retirement.md).

## Discussion and execution state

`discussion` owns persisted transcripts, turn records, context fingerprints and
frozen snapshots. `workspace_sessions.go` composes preparation, validation,
policy, persistence and execution. The registry registers only llama.cpp and
Chat Completions adapters. Generic local/cloud tool approvals use the shared
request broker and display journal; no native protocol is started.

A send is bound to its current discussion/model/context revision. Changing the
route during preparation requires a fresh preview. Request IDs prevent duplicate
accepted turns. Server-side work survives browser disconnect; explicit stop,
shutdown or the model timeout cancels it. Old agent data cannot bypass these
checks or register an executor.

## Context limits and compaction

The first accepted turn freezes static context. Reuse it until configuration
changes, explicit context refresh or compaction. Retrieval is attached only to
the outgoing request. Compaction preserves display history and produces a
portable context snapshot; it cannot transfer native private state. Context
counts are estimates unless the engine/provider reports actual values.

## Cloud destinations and credentials

Cloud uses saved provider/model choices and explicit consent for external
transcript/context sharing. Keys remain in server memory unless remembered in
the OS keychain or encrypted server store. They are never browser storage,
provider-record plaintext or response diagnostics. A locked vault blocks access.

## Context ownership and portability

Projects keep a title, workspace, machine, default model and their discussions.
Brain source files are authoritative; indexes only select bounded context.
Skills and MCP are shared Loom resources, not agent configuration distribution.
External MCP clients configure the gateway manually with a dedicated token.

Only portable text and selected context follow model switches. Historical
reasoning, approvals, tool events and native private memory remain display data,
never inferred or replayed as instructions. Source permissions still apply.

## Events and usage

One discussion event stream supports replay, sequence/cursor recovery and
current turn state. Tokens, costs, latency and provenance remain attached to
individual turns. Legacy agent events are read-only compatibility records.
Usage shows retained model observations and supported cloud balances, with
unknown metrics preserved. No native subscription quota is polled.

## API and persistence boundaries

Authenticated control APIs manage `/api/workspace`, `/api/runtime/sessions`,
projects, workspaces, tools, Brain and providers. Generic pending requests use
`/api/workspace/sessions/{id}/requests/{request_id}`. Native integration endpoints
return410 behind the existing auth/origin boundary. Engine nodes reuse focused
management handlers without initializing discussion, Brain or provider state.

## UI and PWA boundary

One composer and inspector adapt to Local/Cloud. Retired discussions show an
Archive state requiring a model selection. The selector is bounded and scrollable.
Settings uses a section list/back navigation on phones. Valid shared observations
survive failed refresh; resume restarts observations/streams without reloading.
PWA caching contains public fallback/assets only, never private API or SSE data.

## Contributor verification

Run Go and UI tests without real generation; browser tests use isolated data and
read-only fixtures. Include Desktop, narrow Chromium and WebKit views. Actual
physical-device/network/model acceptance remains separate from fixture checks.
