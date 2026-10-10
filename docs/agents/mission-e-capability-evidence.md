# Capability evidence implementation record — 2026-10-10

Checks and outcomes below belong to the original implementation run. See [Mission J verification](mission-j-cleanup.md) for the consolidation run.

Durable fields, privacy bounds, identity partitions and acceptance limits live in [agent compatibility](../agents-compat.md#evidence-and-check-only-automation). The [implementation guide](implementation-brief.md#transversal-inventory) owns the consolidated inventory. This records the first evidence stage on `codex/mission-e`, which started clean; it is not SDK/TCK certification or live installed compatibility.

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
