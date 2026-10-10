# Implementation guide for coding agents

Read [AGENTS.md](../../AGENTS.md), the
[architecture principles](../architecture-principles.md),
[architecture](../architecture.md), [workspace contracts](../workspace-architecture.md)
and [roadmap](../ROADMAP.md). Inspect the branch, working tree, code and
configuration before editing; preserve unrelated work.

## Boundaries

1. Reuse the existing UI components, CSS and tokens. The visual design belongs
   to the design owner. Keep the common discussion/composer and complete local
   parameter editor; do not introduce a second chat or redundant header.
2. Keep changes focused. The leaf package split is delivered; application
   state, lifecycle policy and HTTP domain handlers deliberately remain in Loom.
   Do not recreate completed migrations described in architecture §4.
3. Engines own inference. Keep llama.cpp router mode, native slots and one
   argument builder; do not add slot/token scheduling or restart a router for model changes.
   Engine-service admission may queue residency changes until responses drain.
   Temporary API overrides never alter saved model settings.
4. Descriptors must match implemented capabilities. Native runtime state,
   approvals and hidden reasoning remain private. Unknown metrics stay unknown.
5. Keep credentials out of logs and public records. Provider keys may be
   explicitly remembered in the OS keychain or private encrypted server store. Preserve vault checks, bounded
   input, model-output escaping and consent before external context handoff.
6. Add UI copy in both English and French dictionaries. English is the default;
   use short sentence-case labels and existing Tip components for explanations.
7. Keep one Brain/context engine. Project retrieval is opt-in, personal sources
   require explicit selection, and cloud embeddings require stored consent.
8. Terminals and harness processes must retain their ownership, authentication,
   native permissions and shutdown rules. Never kill unrelated processes.

## Validation

```sh
make build
go test -short ./...
go vet ./...
make check-ui # includes make check-guards
python3 tools/check-doc-links.py
```

`make build` directly embeds `ui/next`; Node is needed for UI checks, not an
asset build. Use the Makefile’s module-aware syntax check. Go HTTP tests need
loopback sockets. Native accounts, GPUs, keychains and ConPTY require separate
real-platform checks.

Use an isolated development data directory and port:

```sh
LOOM_HOME="$PWD/.project-local/runtime" LOOM_SERVICE=loom-dev-engine \
LOOM_UI_SERVICE=loom-dev-ui ./bin/loom web 2594
```

Keep local notes in ignored `AGENTS.override.md` and `.project-local/`. Never
publish private paths, infrastructure, logs, keys, databases or model files.

## Current integration references

- [ACP execution and development agent](acp-implementation.md).
- [ACP design reference](harness-acp-brief.md).
- [Engines and vLLM](../engines.md).
- [Brain sources, semantic indexing and distillation](../brain.md).
- [MCP files](../mcp-files.md), [terminals](../terminals.md) and
  [native usage](../harness-usage.md).

Report the concrete change, checks and results, unverified behavior and any
remaining decision. Update public workflow documentation and the changelog when
behavior, API endpoints or CLI flags change.

## Transversal inventory

Before shared-contract changes, check these producers/consumers. This inventory was traced for the capability evidence stage; a later mission must recheck affected code and report its changed subset.

| Surface | Checked implementations / consumers | Evidence-stage changes (historical) |
| --- | --- | --- |
| Families | Codex app-server/ACP, Claude ACP/extensions, Pi RPC/ACP, OpenCode HTTP/ACP, Antigravity native/bridge, Hermes, OpenClaw, DeepSeek developer preview, custom/registry ACP (`acp_registry.go`, launch/inspect JSON, `harness_features.go`) | Four compatibility constructors through canonical construction, shared ACP projection and native catalog checks |
| Transports | ACP stdio, SSH POSIX/Windows recipes (`remote_machines.go`, lifecycle remote), authenticated Node ACP (`node_harness.go`, `node_bridge.go`), `agentstdio`, Codex/Pi, owned OpenCode HTTP/SSE, AGY CLI stream | Common probe/session boundary; no transport implementation replacement |
| Features and use | `acp_session.go`, permissions/files/elicitation, native session collectors, `harness_history.go`, `agent_history.go`, resume, providers/model sinks, skills/MCP/Brain sinks, quota/usage | Shared adapter Run boundary records successful session enums; history launcher call sites pass explicit check-only selection without behavior changes; unsupported features remain unsupported |
| Health and stores | `capability/state.go`, `workspace_degraded.go`, runtime canonical/contracts/compat aliases, `store`, `mem_store.go`, cached probe/compatibility records, installations, Doctor and redacted diagnostic bundle | Targeted recovery and bounded evidence in existing private capabilities bucket |
| Lifecycle | Local lifecycle, remote SSH lifecycle, controller/Node lifecycle, install transactions, automatic clock/action tests | Automatic activity becomes check-only; explicit update retains the existing installer and performs free checks |
| Other runtimes/engines | `workspace_runtime.go`, `workspace_native.go`, runtime local/OpenAI adapters; llama.cpp/router/service, vLLM, direct endpoints, Node engines | No change: engine/cloud capabilities do not consume harness compatibility records |
| UI | Core state, harness page/account/history/options/lifecycle/machines, chat engine/composer/picker/requests, inspector/config/selection, Usage, Settings Doctor/policy/updates/machines, local/cloud/engine pages | Agent page uses existing key/value rows; lifecycle labels in English/French |
| Monitoring | `tools/agents-watch` probes/normalization/schema/version publication, embedded `tested_versions.json` and capability snapshots, scheduled workflow | No watcher or public snapshot changes; legacy version input remains available |


The native answer and catalog extraction additionally checked `model_sinks.go`, `skills_sink.go`, `mcp_gateway_harness.go`, `brain_agents.go`, `harness_resume.go`, `harness_providers.go`, all six SSH/Node recipe families, and the existing local/OpenAI adapters. Runtime-private journals, store schemas, account readers and UI styles remain independently owned. See [contract coverage decisions](acp-implementation.md#contract-coverage-decisions).

## Capability evidence ownership

This is the first evidence stage, not SDK adoption, TCK certification, automatic
repair or a claim of live installed compatibility. Production changes are:

- `workspace_degraded.go`: checked-capability recovery; failure wins conflicts.
- `runtime/canonical.go`, `agent_acp_compat.go`, native/OpenCode/AGY constructors:
  shared construction and additive observation fields. Accepted-version input
  and warnings remain unchanged.
- `agent_evidence.go`, shared `acpAdapter.Run`, probe/cache readers: bounded
  aggregate private evidence with independent observations and health. Native
  tables never become installation observations; model checks verify models.
- `harness_lifecycle.go`, ACP/native probe launch: automation is check-only;
  explicit mutations retain existing installers and free checks skip model-sink
  writes/key projection. Obsolete automatic-update reservation and send gating
  are removed. Native history call sites retain their previous source behavior.
- Harness agent detail and fallback detail: shared existing key/value rows show
  each capability's level and separate degradation. Existing components/classes
  are reused with English/French copy; no CSS, tokens or existing layout styling
  changes. Inspector, chat, Usage and machine consumers retain their contracts.
- Synthetic evidence/constructor fixtures, recovery/clock/sink/vault/identity
  tests, and existing ACP interaction/lifecycle regressions are updated.

All other inventoried family transports, engine implementations, Brain/resource
sinks, account/quota readers, machine IO, store internals, Doctor handlers,
watcher/public capability snapshots and dependency files are unchanged.
`tested_versions.json` remains an input throughout this transition.


## Guard maintenance

`make check-guards` runs offline source checks and their fault fixtures, then the
existing Go feature/family matrix and contract coverage tests. `make check-ui`
depends on it, so CI uses the same entry point. No paid model, installed harness,
account, network, updater or real native HOME is needed by these guards.

- `TestHarnessContractCoverage` reads catalog IDs, independent feature-path cases,
  live inspect/model sinks, synthetic lifecycle command construction and the
  [canonical decisions](acp-implementation.md#contract-coverage-decisions).
  Its negative cases omit every dimension, add a family, duplicate a family and
  leave an obsolete family. Existing transport fixtures cover native/ACP, SSH
  POSIX/Windows and Node; this is coverage, not installed compatibility.
- `tools/guards/structure.json` explicitly grandfathers current root `.go` files,
  including tests. New root files need a specific ownership reason; leaf files
  need no entry. Deleted entries fail until removed. Authored JS modules under
  `ui/next/js` have a 24 KiB UTF-8 byte ceiling. The five larger modules (Local,
  Settings page/Machines, en/fr dictionaries) have frozen individual ceilings;
  vendored JS outside that directory is outside this authored-code budget.
  A budget increase needs a reviewed rationale, not automatic regeneration.
- Existing i18n tests check literal `t()` keys, parity, plural forms and variables.
  The same dependency-free lexer detects letter-bearing literals in both
  languages in HTML text/attributes/expressions, notification calls and labeled
  data fields. `ui-copy.json` preserves pre-existing values by file, exact literal
  and count; copying an existing literal to a new file or adding an occurrence
  fails. Stale entries fail. This intentionally conservative baseline also
  includes technical values; explain any added technical exception in review.
  Translate user copy rather than adding an exception. Source analysis cannot
  understand every computed string; route/render review remains mandatory.

A temporary filesystem fixture verifies the real entry point reports each class
of violation. Guard tests prove rejection and a passing counterpart for new root files,
new/grandfathered JS growth (including multibyte text), removed keys, missing
variables/plurals, English/French nested HTML/attributes/expressions/toasts, and
stale or duplicated copy exceptions. They operate on synthetic snapshots; no
checkout mutation or generated acceptance baseline is required.
