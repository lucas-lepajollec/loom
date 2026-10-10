# Capability evidence, first stage

## Inventory before the contract change

Branch `codex/mission-e` started clean. The following producers and consumers
were traced before adding fields to `runtime.CompatibilityRecord`:

| Surface | Checked implementations / consumers | Changed subset |
| --- | --- | --- |
| Families | Codex app-server/ACP, Claude ACP/extensions, Pi RPC/ACP, OpenCode HTTP/ACP, Antigravity native/bridge, Hermes, OpenClaw, DeepSeek developer preview, custom/registry ACP (`acp_registry.go`, launch/inspect JSON, `harness_features.go`) | Four compatibility constructors through canonical construction, shared ACP projection and native catalog checks |
| Transports | ACP stdio, SSH POSIX/Windows recipes (`remote_machines.go`, lifecycle remote), authenticated Node ACP (`node_harness.go`, `node_bridge.go`), `agentstdio`, Codex/Pi, owned OpenCode HTTP/SSE, AGY CLI stream | Common probe/session boundary; no transport implementation replacement |
| Features and use | `acp_session.go`, permissions/files/elicitation, native session collectors, `harness_history.go`, `agent_history.go`, resume, providers/model sinks, skills/MCP/Brain sinks, quota/usage | Shared adapter Run boundary records successful session enums; history launcher call sites pass explicit check-only selection without behavior changes; unsupported features remain unsupported |
| Health and stores | `capability/state.go`, `workspace_degraded.go`, runtime canonical/contracts/compat aliases, `store`, `mem_store.go`, cached probe/compatibility records, installations, Doctor and redacted diagnostic bundle | Targeted recovery and bounded evidence in existing private capabilities bucket |
| Lifecycle | Local lifecycle, remote SSH lifecycle, controller/Node lifecycle, install transactions, automatic clock/action tests | Automatic activity becomes check-only; explicit update retains the existing installer and performs free checks |
| Other runtimes/engines | `workspace_runtime.go`, `workspace_native.go`, runtime local/OpenAI adapters; llama.cpp/router/service, vLLM, direct endpoints, Node engines | No change: engine/cloud capabilities do not consume harness compatibility records |
| UI | Core state, harness page/account/history/options/lifecycle/machines, chat engine/composer/picker/requests, inspector/config/selection, Usage, Settings Doctor/policy/updates/machines, local/cloud/engine pages | Agent page uses existing key/value rows; lifecycle labels in English/French |
| Monitoring | `tools/agents-watch` probes/normalization/schema/version publication, embedded `tested_versions.json` and capability snapshots, scheduled workflow | No watcher or public snapshot changes; legacy version input remains available |

## Reuse decision

ACP v1 initialization and session setup are the protocol references:
[session setup](https://github.com/agentclientprotocol/agent-client-protocol/blob/main/docs/protocol/v1/session-setup.mdx).
An advertised `loadSession` is discovery, not successful resume. The
[coder ACP Go SDK](https://github.com/coder/acp-go-sdk) supplies protocol types
and connections, not an installation evidence ledger or Loom's negative overlay.
The audit's SDK/TCK trial is a separate stage. Reuse Loom's existing probes,
canonical records, bounded health overlay, private store and deterministic
fixtures here; adding another transport library would not remove this logic.
No dependency, watcher, transcript store or generic runtime layer is added.

## Regression reproduction

Before implementation, `TestAgentProbeRecoveryIsCapabilitySpecific` failed for
all eleven family/transport cases with `models-only probe restored unchecked
approvals`. `TestHarnessAutomaticChecksNeverUpdateWorkingInstallation` failed
with `updates=1 version=codex-cli 1.1.0`. Both use synthetic checks/injected
lifecycle actions and temporary homes; no model is called.

The post-update sink regression was also demonstrated by temporarily removing
only the new check-only guard, then restoring it: the deterministic
`TestHarnessUpdateChecksLeaveModelSinksUntouched` failed because the Pi fixture's
`models.json` acquired a Loom provider. The fixed run preserves its bytes.
Replaying the original `observeAgentProbe` function also makes
`TestAgentProbeConflictsFailClosed` fail; the fixed function passes.
The original constructor output was captured before refactoring in
`testdata/agents/compatibility-constructors.json`; the equivalence test preserves
legacy versions, source labels, pins, capabilities and warnings.

## Implemented boundary

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

Remaining evidence limits: only models get free protocol-operation verification
in this stage; ACP list/load announcements are discovery. Normal sessions observe
chat, actual assistant streaming and successful used approval/question/form
interactions. Cancellation, resume, history, tools, resource sinks and quota
remain unknown without their own observations. Local file metadata and cached
remote identity/version are fingerprints, not full binary/package attestation;
unchanged metadata or untracked native config changes need a later stage.

## Validation and honest acceptance boundary

All execution used `GOTOOLCHAIN=go1.26.9`, temporary HOME/XDG/Loom data and a
temporary Go build cache with the existing module cache supplied read-only.
No user's native agent profile, IA directory or Loom Node data was used, and no
paid model, login, real updater or live account acceptance was invoked.

| Check | Observed result |
| --- | --- |
| Failing tests first | Recovery failed across eleven family/transport IDs; automatic cycle performed one unwanted update. Both subsequently pass. Removing only the check-only guard reproduced the Pi sink write; fixed guard preserves it |
| Constructor equivalence | Pre-refactor JSON fixture passes for all four constructors, nine families/registry and unknown/tested/drift versions |
| Focused application tests | Passed recovery/conflicts, synthetic evidence, identity/transport partition isolation, bounded history/privacy, vault/locked behavior, actual fake normal sessions and answered approval/questions versus failures, update sink guard, lifecycle clock/HTTP/SSH/Windows/Node policy, feature matrix and native AGY/ACP fixtures |
| `go vet ./...` | Passed |
| `go test ./...` | Attempted on final code; root stops at `TestHandleHubSearch` (`backend_hub_test.go:111`) and platform at `TestFetchReleaseChannels` (`platform/sys_update_channel_test.go:11`), both `listen tcp6 [::1]:0: socket: operation not permitted`. Full root/platform completion is unverified |
| Remaining Go packages | Passed (some cached), including all runtime/ACP/native protocol children, harness, capability, store, engines leaf, Brain, diagnostics, discussion, policy, resources, web and agents-watch |
| `GOOS=windows go build -o <temporary-output> ./cmd/loom` | Passed; compile-only |
| `make build` | Passed; final embedded-UI Linux binary |
| `make check-ui` | Passed syntax checks and 21 Node test-file subtests, including evidence/translation checks; not a count of assertions |
| `npm ci --prefix tools/tests/ui-routes --no-audit --no-fund` | Attempted with bounded fetch retry/timeout; failed `EAI_AGAIN` resolving npm registry for Playwright |
| `node tools/tests/ui-routes/crawl.mjs bin/loom` | Attempted after final build; blocked `ERR_MODULE_NOT_FOUND` for Playwright. No desktop/phone browser routes were exercised |
| `gofmt`, `git diff --check`, documentation checker | Passed; 192 local documentation targets, none missing |

Real installed compatibility, native account/approval behavior, real SSH/Node
execution, Windows runtime and desktop/phone rendering remain unverified.
Fixtures prove Loom's implementation; they do not certify installed harnesses.
The patch is uncommitted; `COMMIT_MSG.txt` records the proposed change and checks.
