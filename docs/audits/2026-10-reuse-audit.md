# Mission C — Loom reuse and architecture audit

**Résumé français.** Audit documentaire et de code du 10 octobre 2026, sans modification du produit. La racine `internal/loom` concentre **76,1 % des lignes Go hors tests** ; plusieurs extractions utiles existent déjà. Les priorités sont de remplacer les listes de versions « testées » par des preuves par capacité, de réduire trois collecteurs de réponses natives et plusieurs catalogues de harnesses, puis de mesurer la qualité de récupération de Brain gratuitement. Le SDK ACP mérite un essai limité ; llama-swap, Letta, Mem0 et Graphiti apportent surtout des idées et des scénarios de test, sans justifier un second superviseur ou moteur de mémoire. SSH reste nécessaire : Environment et certaines lectures Usage ne passent pas encore par Loom Node. Builds Linux/Windows, vet et contrôles UI réussis ; suite Go complète bloquée par les sockets du sandbox, crawl bloqué par Playwright non installable. Aucun appel de modèle, aucune installation de harness, aucun commit.

## 1. Scope, method and confidence

Snapshot: branch `codex/mission-c`, commit `68c5bc4` (`Merge pull request #103 from lucas-lepajollec/fix/flaky-opencode-cancel`). The working tree was clean before this audit. Only this report and `COMMIT_MSG.txt` are deliverables; no contracts, code, configuration, dependencies or runtime behaviour were changed. Consequently no behavioural CHANGELOG/README update or bug-fix regression test applies to this change.

Read repository guidance and architecture documents before analysis: `AGENTS.md`, `CONTRIBUTING.md`, `docs/architecture-principles.md`, `docs/architecture.md`, `docs/workspace-architecture.md`, `docs/agents/implementation-brief.md`, `go.mod`, `Makefile`, CI and agents-watch workflows. The checked-in `AGENTS.md` has no section named “Working method”; the mission's explicit transversal inventory, reuse-first and fail-before-test requirements were nevertheless applied to the recommendations.

**Verified** below means measured from this checkout, read in the named implementation, or exercised by the named deterministic check. Static findings are distinguished from reproduced failures. **Proposed** files, commands, schemas and PRs do not exist as a result of this audit. External facts were checked through web search and primary repository/documentation pages on 2026-10-10; release observations establish activity, not reliability or a maintenance SLA. External SDK/TCK/services were not installed or executed. No live harness, GPU, remote SSH machine, paired Node or account was exercised. No production memory/account directories were opened by the validation commands; execution used a temporary HOME and Loom data directory.

The inventory in §8 covers every current agent family and transport, engine path, relevant store and UI consumer of the audited contracts. It is a contract and reuse audit, not a claim that every line in 966 files was independently security-reviewed.

## 2. Measured structure and extraction opportunities

### 2.1 Concentration

Measurements use tracked files from `git ls-files internal/loom`. LOC means physical `bytes.splitlines()` lines, including comments/blanks, generated sources and both OS alternatives, not executable SLOC. Production means `.go` excluding `_test.go`; vendored UI assets are outside the authored JS figures. There are **966 tracked files**, **672 Go files**, and **26 Go source directories** under `internal/loom`.

| Location | Production Go files | Production LOC | Test Go files | Test LOC |
| --- | ---: | ---: | ---: | ---: |
| `internal/loom` directly | 281 | 67,960 | 188 | 32,767 |
| All subpackages | 132 | 21,373 | 71 | 7,645 |
| Total | 413 | 89,333 | 259 | 40,412 |

The root contains **68.0% of production Go files and 76.1% of production Go LOC**. Moving files alone would improve this percentage without reducing maintenance; the recommendations below require fewer edit sites or a testable ownership boundary.

| Root filename prefix | Production files | LOC | Existing leaf boundary to preserve |
| --- | ---: | ---: | --- |
| `web_` | 29 | 7,861 | `web` helpers; root HTTP/application dispatch |
| `backend_` | 22 | 6,719 | `engine/llamacpp` |
| `engine_` | 20 | 5,550 | llama.cpp leaf; vLLM/direct/Node integration remains root |
| `harness_` | 22 | 5,369 | `harness`, transport packages in `runtime` |
| `workspace_` | 23 | 5,064 | `discussion`, `runtime`, `events`, `policy` |
| `brain_` | 13 | 4,407 | `brain` |
| `sys_` | 26 | 3,712 | `platform` |
| `chat_` | 16 | 3,616 | original Conversation pipeline plus `discussion` |
| `acp_` | 14 | 3,248 | `runtime/acp` |
| `llm_` | 8 | 3,201 | `runtime/openai`; original client/pipeline integration |
| `voice_` | 8 | 2,922 | root voice/application lifecycle |

Subpackage production totals: `runtime` and descendants 34 files/6,096 LOC; `brain` 15/3,770; `engine/llamacpp` 14/3,050; `tools` 9/2,278; `platform` 25/2,109; `resources` 7/893; `discussion` 5/814; `store` 4/571; `notify` 4/460; `web` 6/381; `policy` 1/298; `diagnostics` 1/252; `events` 1/111; `capability` 1/89; `doctor` 3/85; `project` 1/71; `harness` 1/45. These are established boundaries, not missing architecture.

### 2.2 Largest implementation files

Paths below are relative to `internal/loom/`; tests are excluded.

| File | LOC | Bytes |
| --- | ---: | ---: |
| `web_api.go` | 1,456 | 49,639 |
| `llm_client.go` | 1,283 | 53,648 |
| `harness_lifecycle.go` | 1,014 | 32,068 |
| `chat_conversation.go` | 1,008 | 40,458 |
| `backend_build.go` | 991 | 33,384 |
| `brain_semantic.go` | 856 | 24,060 |
| `backend_models.go` | 831 | 25,639 |
| `workspace_sessions.go` | 763 | 26,982 |
| `backend_prebuilt.go` | 759 | 23,998 |
| `web_llamacpp.go` | 750 | 23,271 |
| `brain/engine.go` | 749 | 19,682 |
| `acp_session.go` | 737 | 23,447 |
| `brain/markdown_memory.go` | 688 | 19,554 |
| `brain_consolidation.go` | 677 | 19,921 |

Authored `ui/next/js`: **91 files, 16,190 LOC, 1,214,040 bytes**. The English/French dictionaries account for 436,180 bytes; excluding those gives 89 files/777,860 bytes. `ui/next/js/features/harnesses/page.js` is **615 LOC, 62,894 bytes** (62.894 decimal KB; 61.42 KiB), **8.09%** of non-dictionary JS bytes. Other large modules are `settings/page.js` 570 LOC/47,552 bytes, `settings/machines.js` 348/29,448, `local/page.js` 289/28,102, `resources/brain.js` 205/24,457 and `chat/engine.js` 431/22,609. Long template lines explain why byte concentration is more informative than LOC alone for these pages.

Reproduce the Go totals from the repository root without extra dependencies:

```sh
python3 - <<'PY'
from pathlib import Path
from collections import defaultdict
import subprocess
paths = subprocess.check_output(['git', 'ls-files', '-z', 'internal/loom']).decode().split('\0')
counts = defaultdict(lambda: [0, 0])
for name in filter(None, paths):
    p = Path(name)
    if p.suffix != '.go':
        continue
    key = ('root' if p.parent == Path('internal/loom') else 'packages',
           'test' if p.name.endswith('_test.go') else 'production')
    counts[key][0] += 1
    counts[key][1] += len(p.read_bytes().splitlines())
for key, value in sorted(counts.items()):
    print(*key, *value)
PY
```

### 2.3 Concrete duplication and change amplification

| Concern | Checked paths | Measured edit sites / consequence | Recommended first reduction |
| --- | --- | --- | --- |
| Native answer/event accumulation | `agent_native.go`, `agent_opencode.go`, `agent_antigravity.go` | **3 collectors for 4 native families**: Codex/Pi share one; OpenCode and AGY each repeat answer collection, projection, item closing and final-state plumbing | One pure accumulator in existing `runtime`, preserving each protocol's replacement/prefix semantics; leave lifecycle in adapters |
| Compatibility record/warnings | `agent_acp_compat.go`, `agent_native.go`, `agent_opencode.go`, `agent_antigravity.go` | **4 constructors** assemble substantially the same compatibility policy | One constructor in `runtime/canonical.go` with family-specific facts as inputs |
| Harness identity/launch metadata | `harness/acp_agents.json`, `harness/inspect.json`, `remote_machines.go` (`remoteHarnessDefs`) | **3 core catalogs** for a new local+remote ACP launcher; remote npx pins already reuse builtins | Extend existing `harness` package's declarative metadata; do not duplicate argv/version pins |
| Optional family integrations | `model_sinks.go`, `skills_sink.go`, `mcp_gateway_harness.go`, `brain_agents.go`, `harness_resume.go`, `harness_providers.go`, `harness_usage.go` | Separate family tables/switches; changes to supported resources often require several edits. Not every family implements every resource | Audit a new-family checklist and expose existing supported hooks; retain genuinely different file/protocol schemas |
| ACP/native stdio mechanics | `runtime/acp/protocol.go`, `runtime/agentstdio/client.go` | **2 implementations** of bounded framing, pending requests, write locking and process/reader lifecycle | ACP SDK differential trial before inventing a third generic transport |
| Memory lexical scoring | `brain/search.go`, `brain/markdown_context.go` | **2 BM25 implementations** with the same k1=1.2/b=0.75 but different corpora and selection | Shared pure scoring function/constants after quality baseline; do not merge memory policies |
| Machine harness display names | `ui/next/js/features/harnesses/machines.js`, `ui/next/js/features/settings/machines.js` | **2 name maps**, with different member sets | Use existing runtime/install descriptors, with one fallback map if needed |
| Native session rows | `ui/next/js/features/harnesses/page.js` (`NativeSessions`) | **2 render branches** repeat session row markup | Extract one row component with explicit actions |

The first six identity/resource catalogs are concretely `acp_agents.json`, `inspect.json`, `remoteHarnessDefs`, `modelSinks`, `skillSinkDefs`, and `brainAgentSpecs`. This is a measured coordination surface, not a claim that every agent edit requires six changes. Gateway, native resume, provider injection and usage add conditional sites.

The stdio clients are **not interchangeable**: ACP allows bounded initial SSH banner noise, ordered notifications and JSON-RPC; `agentstdio` preserves stdout drainage, bounded diagnostic capture, and the Pi envelope differs from JSON-RPC. Root adapters also differ in text replacement: native item replacement/prefix clearing, OpenCode replacement, and AGY streaming text. A generic runner that erases these distinctions would increase debugging cost.

Static JS reference search found `Detail` in `harnesses/page.js` and exported `MachinesSection` in `harnesses/machines.js` without call/import sites. These are deletion candidates, not browser-proven dead code. `MachineDialog` in the latter file **is used** by Settings; deleting that entire module would break the canonical machine page. Harness history transfer and same-harness native-session import have different consent/workflow semantics and should remain distinct.

### 2.4 Ordered package/module boundaries

1. **Existing `runtime`: answer accumulator, then compatibility construction.** Inputs are canonical events and explicit family facts; outputs are answer/usage/projection data. Move no store, registry, launch or auth ownership. Risk: replacement and terminal-event order; use current fixture mapper tests plus interleaved/replace cases.
2. **Existing `harness`: declarative recipe identity.** Start with local/remote inspect/launch metadata. Root keeps native discovery, credential decisions, resource sinks, machine policy and mutation. Risk: unsupported planned profiles becoming executable through defaults; test all entries, including generic Hermes ACP and the unverified DeepSeek developer preview.
3. **UI feature modules, same directory.** Extract catalog/dialogs from `harnesses/page.js` to `harnesses/catalog.js`, then `native-sessions.js`, then `resources.js`. Reuse existing account/history/lifecycle/options/machines modules. Preserve markup, CSS, tokens and interaction order. Risk: import cycles and remounting state; route crawl and existing UI tests are required.
4. **Proposed `engine/residency`: only the pure state machine in `engine_service.go`.** Its clock, policy, observe and unload seams already exist. Root retains `engine_service_backend.go`, HTTP keys, launch arguments, engine selection and global state. Risk: fairness/drain/maintenance races; migrate `engine_service_test.go` intact before changes. This is residency admission, not inference or slot scheduling.
5. **Existing `capability`: pure evidence/recovery rules.** Keep persistence and event publication in root integration. Reuse `capability.State`, `capability.Probe`, existing feature names and event bus. Risk: permission/auth failures being mistaken for unsupported features; explicit states and operation-specific tests come first.
6. **Later, conditional leaves for vLLM configuration or archive helpers.** `engine_vllm_params.go`/library/download and `backend_build.go`/`backend_prebuilt.go` are candidates only after demonstrating duplicated pure operations. Keep owned-child lifecycle and platform policy where they are. No generic installer framework is justified by file size alone.

Do not repeat the completed extractions documented in `docs/architecture.md`: `discussion`, `runtime`, `brain`, `store`, `tools`, `platform`, `engine/llamacpp`, `resources`, `policy`, `events` and `web` already isolate useful logic. Root package import fan-in also shows integration is deliberate: `runtime` and `policy` are each imported from 18 root files, `brain` from 16, `discussion` from 12 and `web` from 10. Extract leaves toward these boundaries; do not create a package that imports root or a new workspace framework.

## 3. Agents: reuse, integration tiers and compatibility evidence

### 3.1 Current paths and proposed tiers

`workspace_runtime.go` and `acp_registry.go` already use one runtime registry and one ACP adapter type. `acpAdapter.Run` dispatches native AGY, native OpenCode HTTP, native Codex/Pi, or generic ACP. Native selection is currently for builtins on the **local** machine; custom and remote agents use ACP. An OpenCode Loom model sink can select its ACP path. AGY prefers discovered native stream support and has Loom's ACP bridge as fallback. Unsupported previews must not gain execution merely by sharing metadata.

| Family | Current implementation checked | Proposed product tier / supported route |
| --- | --- | --- |
| Codex | `agent_native.go`, `runtime/codexapp`, pinned Codex ACP recipe | Core: app-server locally; generic ACP where selected/remote |
| Claude Code | generic ACP, Claude question/failure extensions in `runtime/acp/interactions.go`, pinned Claude ACP recipe | Core: specialised support atop the same ACP adapter; a separate native integration needs measured benefit first |
| Pi | `agent_native.go`, `runtime/pirpc`, Pi ACP recipe, model sink | Core: RPC locally; generic ACP elsewhere |
| OpenCode | `agent_opencode.go`, `runtime/opencodehttp`, ACP alternative | Core: owned HTTP server locally where eligible; generic ACP alternative |
| Antigravity | `agent_antigravity.go`, `runtime/antigravity`, `acp_compat.go`, `runtime/acp/agy.go` | Core: native CLI/account catalog; existing bridge fallback; never inject arbitrary provider keys |
| Hermes | generic ACP, builtin installation/usage/features, `hermes acp` metadata | Standard ACP, including chat and quota support; no specialised native runner |
| OpenClaw | builtin `openclaw acp` | Standard ACP |
| DeepSeek Harness | builtin `@deepseek-ai/dsh@0.2.0-rc.2 --profile acp`, `dsh` discovery, preview/features | Standard ACP developer preview; executable launcher is declared, but real-harness compatibility remains unverified |
| Custom/registry ACP agents | generic registry/catalog, `acp_session.go`, `runtime/acp` | Standard ACP, declarative launch/config only; no new per-family runner |

This tiering is ownership and support prioritisation, **not a second adapter registry**. Keep `Run`, `Connectable`, `QuotaReader`, native session import/resume and model-source eligibility in current contracts. Claude being “core” does not claim that Loom already runs Claude through a native non-ACP protocol. Likewise, `localHarnessUsable` describes a narrower native capability; its false result for Hermes does not disable the generic ACP route. `TestHarnessQuotaCapabilities` explicitly requires Hermes chat and quotas. Gemini is not a builtin; catalog rejection tests exist and should remain part of inventory validation.

### 3.2 coder/acp-go-sdk comparison

The SDK is Apache-2.0 licensed, generates protocol types, and exposes agent/client connections. Its observed release is v0.13.5, including model selection moving to session config options; 0.x SDK/schema releases must not be confused with negotiated ACP wire versions. This is maintained upstream work, with API churn to account for. [SDK repository/license](https://github.com/coder/acp-go-sdk), [releases](https://github.com/coder/acp-go-sdk/releases).

The inspected `go.mod` declares Go 1.21 and no required dependencies, so dependency overhead looks small. That does not establish adaptation cost. [SDK go.mod](https://raw.githubusercontent.com/coder/acp-go-sdk/main/go.mod).

| Concern | Loom now | SDK source inspected / integration implication |
| --- | --- | --- |
| Protocol representation | explicit maps/raw extension handling in `runtime/acp/session.go`; canonical projection in root | generated types reduce schema handwork, but must preserve extensions and unknown native fields |
| Notification ordering | Loom notification processing is ordered | response-associated notification barriers exist; compare delayed-notification/replay behaviour |
| Cancellation | call context ends waiting; Loom has explicit session cancel paths | SDK also handles JSON-RPC cancel requests; preserve separate turn cancellation semantics |
| Bounds | 4 MiB frame, bounded 32 concurrent inbound requests | inspected SDK defaults include 10 MiB frames, 1,024 queued notifications; request handlers launch goroutines without Loom's semaphore |
| Noise/diagnostics | initial SSH banner tolerance; private stderr handling | raw protocol logging and malformed-message tolerance need Loom-specific configuration/policy |
| Process ownership | OS-specific owned-group termination in `runtime/acp/process_*.go` | retain Loom process ownership and SSH/Node bridge outside SDK |

These differences come from reading [SDK connection.go](https://raw.githubusercontent.com/coder/acp-go-sdk/main/connection.go); they are not runtime differential results. The SDK's [notification barrier tests](https://raw.githubusercontent.com/coder/acp-go-sdk/main/connection_notification_barrier_test.go) offer concrete scheduling scenarios to adapt, with Apache notices where copied.

**Recommendation:** a bounded experiment replacing only ACP framing/request machinery, using existing fake agents and replay/overload/cancel/policy fixtures. Keep the canonical `RequestBroker` already in `runtime/canonical.go`, filesystem consent, Claude forms, AGY bridge, unknown extensions and process policy. Compare total deleted bespoke LOC against bridge/configuration LOC, test coverage and allocations. Adopt only if the combined client gets smaller and preserves bounded concurrency and redaction. Do not add SDK plus the old transport indefinitely, and do not apply it to Pi RPC or Codex app-server merely because they use stdio. No SDK dependency was added here.

### 3.3 Official ACP TCK: useful evidence, bounded claims

The official organisation's Apache-2.0 [acp-tck](https://github.com/agentclientprotocol/acp-tck) is explicitly experimental. It launches an **agent** subprocess, defaults to v1, and has opt-in draft v2. It tests lifecycle/cancel/negative capability/transport behaviours; coverage remains incomplete. It does not directly certify Loom's **client**. Full runs can send prompts or authenticate, so they are not inherently free. JSON failure diagnostics contain wire transcripts and stderr; never persist those into the proposed compatibility ledger. Preserve PASS/FAIL/SKIPPED/NOT_TESTED rather than turning skips into successes.

Proposed first use: pin a TCK commit and run v1 against `bin/loom loom-fake-acp` in a temporary HOME/work directory; failures may reveal intentionally partial fixture behaviour, not a real-agent defect. Then turn selected upstream requirements into client fixtures exercising Loom. Example future command from the pinned TCK checkout:

```sh
uv run acp-tck --protocol-version 1 --agent-cwd "$AUDIT_TMP/work" \
  --report-json "$AUDIT_TMP/tck-private.json" -- "$LOOM_BINARY" loom-fake-acp
```

`AUDIT_TMP` and `LOOM_BINARY` must be set to temporary/absolute paths. This command was **not run**. Before installed-harness update checks, review and pin an explicit allowlist of test node IDs that perform only read-only protocol checks; no prompt, login/logout, file write or tool execution. A `-k` name match alone cannot guarantee that. The sanitizer stores only requirement/check IDs, outcomes, schema/adapter revision, duration and coarse error enums, and deletes raw reports. Draft v2 is a separate future contract audit, not a silent default upgrade.

### 3.4 Replace version lists with per-capability evidence

Current mapping:

| Existing surface | Verified contents/behaviour | Migration role |
| --- | --- | --- |
| `harness/tested_versions.json` | AGY 1.3.1; Claude ACP 0.88.0; Codex CLI 0.159.2/0.162.0; Pi 0.84.3/1.1.0; OpenCode 1.18.33/1.18.35; other entries empty | Historical maintainer observations only; do not inflate to all capabilities, transports or OSes |
| `agent_acp_compat.go` | adapter pin, installed/registry version, warnings | Availability and provenance; registry versions are not working-session evidence |
| `runtime/canonical.go` compatibility records | capability checks, protocol/adapter/source fields | Extend canonical representation; merge four constructors |
| `acp_probe.go`, native probes, agents-watch | discovery/initialisation/catalog/config/session surfaces without a generation prompt | Feed narrowly verified operations; explicitly identify checks that touch opted-in model sinks |
| `harness_features.go` | product feature projection and protocol-dependent eligibility | Separate “implementation exists” from evidence that the installed instance works |
| `capability/state.go`, `workspace_degraded.go` | bounded, in-memory negative capability overlay, events and descriptor filtering | Reuse mechanism; add positive evidence and precise recovery |
| `harness_lifecycle.go`, `harness_lifecycle_node.go` | update/install actions, idle checks, opt-in auto mode | Trigger free checks after explicit updates; no automatic update of a working harness |

**Proposed identity:** `(machine installation ID, family, transport, binary/package fingerprint, adapter/schema revision, non-secret launch-config revision)`. Include installed version as provenance, not the compatibility decision. Local, SSH ACP and Node ACP evidence never imply one another. Hashes must not expose credentials, home paths or native account identifiers.

**Proposed evidence per capability:** independent observations `discovered`, `protocol_verified`, `observed_working`, each with check ID/source, timestamp, matching fingerprint and count; health separately `unknown`, `ok`, `degraded`, `auth_required`, `unreachable`, `not_applicable`. These are not a single monotonically increasing score: a feature can retain historical session success while being currently degraded. A mock fixture validates Loom's implementation, not the user's installation. An advertised capability is discovery only; a successful relevant operation is verification; natural successful use establishes observed working.

Example schema sketch, not a new file or current API:

```json
{
  "installation": "opaque-machine-family-id",
  "transport": "acp-node",
  "fingerprint": "opaque-build-and-adapter-revision",
  "capability": "session-list",
  "discovered": {"check": "initialize.sessionCapabilities", "at": "2026-10-10T10:00:00Z"},
  "protocol_verified": {"check": "session.list.empty-or-valid-page", "at": "2026-10-10T10:00:01Z"},
  "observed_working": null,
  "health": "ok",
  "last_failure": null
}
```

Free update checks: fingerprint/version/help, initialise, eligible empty session/catalog/config discovery, bounded list/load-shape checks where read-only, schema checks and deterministic fixtures. No prompt, generation, tool invocation, account connection, model load, sink mutation or global permission change. Normal authorised sessions contribute only aggregate success/failure enums for streaming completion, cancellation, approval response, question/form response, resume and similar operations. Store **no transcript, prompt, answer, tool arguments/output, paths or raw wire logs** in this ledger; reuse the existing private store and bounded record eviction. Existing discussion journals are not copied into compatibility evidence.

Degrade the failing capability, not the family. A models probe failure must not disable chat; auth gates remain unknown/auth-required, not “unsupported”. A transport outage affects operations actually depending on that transport, while local account readings may remain valid. Restore a capability only after **that capability's check/use** succeeds for the current identity. A changed fingerprint starts an unknown evidence partition; it is not itself a failure, and old success is retained as historical provenance.

**Static recovery concern:** `observeAgentProbe` currently restores every degraded feature absent from the failed-check set after any error-free probe, including features not checked. This is verified by code inspection, not reproduced as a user bug here. First future fix PR must introduce a deterministic test: mark approvals degraded, submit only a successful models check, assert approvals remain degraded; demonstrate it fails before changing recovery logic. Add a corresponding successful approvals check to prove targeted restoration. Avoid changing generic probe errors to disable all features.

The opt-in auto loop currently runs every six hours and can update an installed, idle, otherwise working harness when a newer version exists. The mission's target requires a deliberate future behaviour change: convert working-harness automatic activity to **check-only**, surface an explicit update action, then probe the selected version. Never update merely because a version is absent from a list. Migration, UI wording, docs and CHANGELOG would belong to that PR, with an injected-clock/action test showing a working idle harness is not updated. No update policy was changed in this audit.

`tools/agents-watch` and `.github/workflows/agents-watch.yml` already provide scheduled schema/capability monitoring and pin-review candidates. Extend their normalised outputs rather than adding a watcher. Preserve their stripping of private paths/current state and `_meta` values. The scheduled install/publish mode is maintenance tooling, not a safe automatic end-user compatibility check; it was not invoked.

## 4. Engines: llama-swap comparison and ownership

### 4.1 Current supervision is already separated

`engine/llamacpp` owns argument construction, router configuration, model/cache observation and `Supervisor`. Root `backend_serve.go`, `backend_llama_owned.go`, `backend_engine.go`, `backend_router.go`, `engine.go` and service integration own persisted choice, application callbacks and admission. A reachable router uses load/activation APIs for model changes, rather than restarting the server. Owned-child exit cleanup is guarded against deliberate stop/replacement; it clears stale runtime state without killing an unrelated process. Observation uses `/props?model=...&autoload=false` where appropriate to avoid triggering loads.

`engine.go` currently has one local `Engine` implementation, llama.cpp; it is not a completed common abstraction for every backend. vLLM has its own install/library/parameters/update and owned process path in `engine_vllm*.go`. Linked direct endpoints in `engine_direct.go` are externally owned. `engine_node.go` proxies to a credentialed Node, with `bench_node.go` forwarding remote benchmarks. `runtime/openai` covers configured Chat Completions providers. `engine_service.go` handles residency admission, waits, drain, background grace and idle unload; it does not reimplement inference or engine slot scheduling.

### 4.2 External comparison and reuse decision

llama-swap is a Go MIT project, actively releasing: v263 was observed on 2026-10-10. This supports maintained-project status, not a conclusion that replacing Loom's process management is safer. [License](https://raw.githubusercontent.com/mostlygeek/llama-swap/main/LICENSE.md), [releases](https://github.com/mostlygeek/llama-swap/releases).

Its proxy/model-process switching, TTL/group policies and multi-upstream configuration solve a broader deployment problem. Its README documents that `/props` ignores `autoload`, which conflicts with Loom's read-only observation assumption. The inspected `go.mod` contains a substantial dependency list; single-binary deployment does not mean a dependency-free Go import. [README](https://github.com/mostlygeek/llama-swap), [go.mod](https://raw.githubusercontent.com/mostlygeek/llama-swap/main/go.mod).

| Aspect | Loom | Useful reuse / decision |
| --- | --- | --- |
| Native llama.cpp router | One owned router; API model loads | Keep ownership; no llama-swap supervising the same executable |
| Readiness | existing supervisor/router plus root admission | Borrow atomic readiness snapshot and “observe only” versus “ensure ready” distinction |
| Shutdown | owned process group, deliberate stop/exit guards; vLLM has separate stop path | Compare race and shutdown scenarios; do not copy global process killing |
| Capacity/idle | service admission/drain/idle unload | Borrow TTL/failed-start/concurrent readiness scenario ideas only if missing in current tests |
| Code reuse | existing small leaf and injected service seams | No demonstrated net deletion from importing an entire second supervisor |
| Tests | `engine/llamacpp/*_test.go`, `engine_service*_test.go`, backend/vLLM tests | Keep current suites; adapt a specific pure validation scenario before any code port |

The inspected [process interface](https://raw.githubusercontent.com/mostlygeek/llama-swap/main/internal/process/process.go) explicitly puts start decisions inside `EnsureReady`, while `WaitReady` only observes. Its [server implementation](https://raw.githubusercontent.com/mostlygeek/llama-swap/main/internal/server/server.go) and [bootstrap](https://raw.githubusercontent.com/mostlygeek/llama-swap/main/llama-swap.go) are coupled to internal process/router/store/hardware packages, not a drop-in public library. Go `internal` rules also prevent directly importing those packages from Loom.

The upstream [bootstrap tests](https://raw.githubusercontent.com/mostlygeek/llama-swap/main/llama-swap_test.go) include deterministic valid/broken config and unknown-macro rejection. Reuse that testing idea for Loom configuration validation if an uncovered case is demonstrated; these tests themselves depend on llama-swap configuration code and are not directly reusable. The wider upstream process-test suite was not executed or exhaustively reviewed, so no claimed coverage superiority or ready-to-copy supervisor test exists in this report.

**Recommendation:** retain Loom's supervisor/router; use llama-swap as a reference for readiness and lifecycle tests. Copy small isolated code only after a deletion/correctness proof and retain MIT notices. A user-managed llama-swap may be linked through Loom's existing direct/OpenAI-compatible endpoint path, with no claim of native `/props`, router control or process ownership. Embedding or launching both supervisors for one server is rejected. Router model switching must continue to start the engine once and load via its API.

## 5. Brain: existing reuse and a free retrieval benchmark

### 5.1 What Loom actually has

`brain/engine.go` indexes files and provider documents with deterministic chunks, scope filtering and a disposable cache. `brain/search.go` provides local BM25, phrase weighting, stable top-k selection and packing. Pack defaults to 1,500 approximate tokens, skips chunks that do not fit, deduplicates text and emits citations. `brain.Tokens` uses rune count/4 rounded up: it is a budget estimate, not the selected model's billed tokenizer.

`brain/semantic.go` provides vector checkpoints/model identity invalidation, cosine search and RRF fusion. Root `brain_semantic.go` manages opt-in local/cloud embeddings, consent, readiness and storage. `brain_http.go` implements SearchContext/PackContext; `brain_context.go` supplies project scope and a short automatic-context deadline with lexical fallback. This already provides lexical and optional hybrid retrieval; no separate memory search service is needed.

The active root `memoryStore()` in `brain_items.go` returns **`brain.MarkdownStore`**: writable primary vault or private fallback, YAML frontmatter, indexes, timestamps and file replacement. Legacy `brain/memory*.go` contains candidate/expired/superseded states and lineage, but migration filters those into the active Markdown model. Do not claim the active Markdown store already implements temporal fact validity just because the legacy store has supersession fields. `brain/markdown_context.go` separately builds index/profile/topic memory context. There are two BM25 scoring sites, not two independent new memory engines to build.

`brain_consolidation.go` already has an idle/paused-discussion background consolidation workflow; `brain_distill.go` has model-assisted extraction. Neither is free to benchmark by invoking its model path. `brain_agents.go` syncs opted-in primary-memory hints to supported native families. `brain_transcripts.go` retains the canonical verbatim display journal, independently of compacted model input.

Final context is a second boundary: `workspace_prompt.go`, `workspace_frozen_context.go`, `discussion/prompt.go`, and the native/local/cloud callers prepare the actual model-facing messages. Frozen memory context is selected from the first user query and reused; its revision does not automatically track every mutable memory change. Fresh project Brain passages and frozen memory can therefore behave differently. This is a verified architecture distinction, not a measured retrieval defect.

### 5.2 External projects: current facts and appropriate reuse

| Project | Licence and maintenance evidence | Relevant comparison | Recommendation for Loom |
| --- | --- | --- | --- |
| Letta Code | Apache-2.0 code; names/logos/artwork have separate exclusions. Observed v0.34.10 release on Oct 9; active pre-1.0 project | MemFS is an agent-oriented git memory filesystem with indexes/frontmatter and dreaming workflows | Reuse documentation/versioned-edit and review ideas; Loom already has Markdown memory and consolidation. Do not adopt a cloud/agent memory authority |
| Mem0 | Apache-2.0; observed Python SDK v2.2.1, with v2.2.0 released Sep 23; active releases | Extraction and semantic/BM25/entity retrieval patterns; current README describes ADD-only accumulation | Reuse benchmark formats, extraction evaluation ideas and stale-fact tests; no new Python memory service or default LLM ingestion |
| Graphiti | Apache-2.0; observed v0.30.2 on the releases page, active pre-1.0 development | Temporal graph facts, episodes/provenance and hybrid retrieval | Reuse temporal labels/as-of scenarios and lineage concepts; a graph database plus extraction pipeline is not justified by current measurements |

Sources for licence/release facts: [Letta licence](https://raw.githubusercontent.com/letta-ai/letta-code/main/LICENSE), [Letta releases](https://github.com/letta-ai/letta-code/releases); [Mem0 licence](https://raw.githubusercontent.com/mem0ai/mem0/main/LICENSE), [Mem0 releases](https://github.com/mem0ai/mem0/releases); [Graphiti licence](https://raw.githubusercontent.com/getzep/graphiti/main/LICENSE), [Graphiti releases](https://github.com/getzep/graphiti/releases). Release activity is the maturity proxy checked here; vendor performance/production claims were not independently verified.

Letta's [MemFS documentation](https://docs.letta.com/letta-code/memfs) describes per-agent git-backed memory, local checkouts, indexes and dreaming worktrees; semantic/vector retrieval is not the default MemFS mechanism. That differs from Loom's discussion-owned context and shared local memory. Borrow traceable edits and controlled background review rather than creating per-harness portable memory state.

Mem0's [current README](https://raw.githubusercontent.com/mem0ai/mem0/main/README.md) advertises extraction and combined semantic/BM25/entity retrieval. However, inspected OSS [memory/main.py](https://raw.githubusercontent.com/mem0ai/mem0/main/mem0/memory/main.py) rejects `add(timestamp=...)` and `search(reference_date=...)` as gated features. Do not promise that the advertised temporal surface is available in the free OSS path. Model ingestion, extra stores and telemetry dependencies also prevent treating its default benchmark ingestion as a free replacement for Loom Brain.

Graphiti's [README](https://raw.githubusercontent.com/getzep/graphiti/main/README.md) describes temporal knowledge graphs; [edges.py](https://raw.githubusercontent.com/getzep/graphiti/main/graphiti_core/edges.py) distinguishes `valid_at`, `invalid_at`, `created_at`, `expired_at` and episode provenance. That distinction is useful for tests of current versus historical facts. Adopting its graph runtime before demonstrating a retrieval gap would add storage/extraction maintenance without measured simplification.

### 5.3 What has been measured, and what has not

Executed existing `BenchmarkBrainSearch` with `-benchtime=100ms`, Limit=10, one synthetic frequent-term corpus; setup excluded. Results from this machine/run, rounded:

| Chunks | Query | ns/op | B/op | allocs/op |
| ---: | --- | ---: | ---: | ---: |
| 1,000 | `alpha` | 515,263 | 154,281 | 979 |
| 1,000 | quoted `alpha beta` | 716,272 | 154,312 | 984 |
| 20,000 | `alpha` | 814,023 | 308,047 | 979 |
| 20,000 | quoted `alpha beta` | 7,886,231 | 309,643 | 984 |
| 60,000 | `alpha` | 2,283,570 | 628,018 | 979 |
| 60,000 | quoted `alpha beta` | 27,127,178 | 627,082 | 984 |

This is **throughput/allocation evidence only**. It does not establish natural-query Recall@k, stale-fact accuracy, semantic quality, final-context evidence, indexing cost or p95 latency. No LongMemEval result is claimed. Existing Brain deterministic tests passed, including vector invalidation/checkpoint tests; they are not a substitute for retrieval-quality evaluation.

### 5.4 Benchmark dataset and fairness

Use pinned **LongMemEval cleaned S** as the initial natural-history source, alongside hand-reviewed synthetic temporal/scope cases. The original suite has 500 questions covering six memory abilities; haystacks supply session IDs/dates, answer-session IDs and turn-level `has_answer`. Use all haystack sessions for each selected case, not the oracle-only version. The upstream QA evaluation calls a model judge and is excluded. LongMemEval V2 exists; pinning the original cleaned suite is a comparability choice, not a claim it is the newest. [Official LongMemEval README](https://raw.githubusercontent.com/xiaowu0162/LongMemEval/main/README.md), [official cleaned dataset](https://huggingface.co/datasets/xiaowu0162/longmemeval-cleaned).

The Apache-2.0 [mem0ai/memory-benchmarks](https://github.com/mem0ai/memory-benchmarks) provides reusable evaluation organisation. Its [`--predict-only` documentation](https://raw.githubusercontent.com/mem0ai/memory-benchmarks/main/README.md) means skipping answer generation/judging, **not free ingestion**. Its [LongMemEval runner](https://raw.githubusercontent.com/mem0ai/memory-benchmarks/main/benchmarks/longmemeval/run.py) still uses model clients/ingestion and can download data; retrieval mode can also judge with a model. Do not run its default Mem0 path in Loom CI. Reuse input/prediction conventions while directly testing Loom retrieval in predict-only mode.

Dataset redistribution licence was **not verified** from the dataset card during this audit. Code licensing does not grant dataset redistribution rights. First PR must record the dataset's actual licence/terms and pinned content SHA before checking in a subset. If redistribution is unavailable, keep an approved offline test-data cache outside git and commit synthetic CI fixtures plus selected-ID/hash manifest; a synthetic-only run must be labelled as such. No dataset downloaded or copied here.

Proposed fixed natural subset: 24 IDs, selected once and reviewed: three non-abstention cases per six question types (18), three abstention cases, and three additional knowledge-update cases. Selection sorts SHA-256 of `loom-retrieval-v1 + question_id` within each bucket, chooses disjoint IDs, and fails if a bucket is insufficient. Commit the **actual selected IDs**, upstream immutable revision/file digest, category counts and converted hashes; do not reselect on every CI run. Actual IDs are intentionally not invented in this report. Add eight small synthetic cases for file replacement, as-of history, missing evidence, budget drop, duplicate text, source scope, frozen-context refresh and vector invalidation.

Per case, construct a fresh Brain engine and ingest the entire selected history through the normal provider/folder chunking path. Keep session/turn-to-chunk mappings and gold annotations in an evaluation sidecar. Never put answers, `has_answer`, gold session IDs or oracle ranking hints into indexed text or queries. Preserve chronological dates as normal source content/metadata, and identify evidence at both session and full supporting-turn span granularity. Do not silently trim distractors to satisfy limits: record corpus bytes/chunks, reject oversized cases or deliberately choose another reviewed case. “Small subset” limits case count, not necessarily tokens per history.

Natural knowledge-update labels need a reviewed sidecar identifying superseded/current support spans and query time. `has_answer` alone does not label stale facts. Synthetic updates replace a real Markdown file, run Refresh and query again; do not simulate relevance using an already-extracted oracle memory alone. Historical as-of questions can legitimately retrieve old facts.

### 5.5 Metrics and output contract

All metrics are deterministic evidence checks, with no answer generator or judge:

| Metric | Exact definition / stage |
| --- | --- |
| Evidence Recall@k | For k=1,5,10,20, unique gold supporting units covered by retrieved chunks / total gold units; report coarse session recall and strict supporting-span recall separately |
| Complete evidence@k | Boolean: every required supporting span covered; prevents multi-session partial recall being presented as full success |
| Evidence in packed context | Same span coverage after `PackHits`, budgets 512/1,500/3,000 approximate tokens; citation IDs alone never count |
| Evidence in final context | Span coverage in actual prepared model-facing messages after assembly/window fit/frozen-context handling; record all-evidence-present plus missing-stage reason |
| Updated/stale facts | Current-support presence and stale-support contamination for current-state queries; separately as-of correctness. Historical transcript presence is reported separately from stale memory injected as authoritative context |
| Abstention | No-evidence cases have no Recall denominator: report retrieved/packed unsupported units, scope leakage and context tokens; do not force a zero/one Recall score |
| Tokens used | `brain.Tokens` for retrieval injection and full prepared messages separately; estimated tokens, not billing or actual model token counts |
| Latency | Monotonic warm search, packing and final assembly p50/p95 over 30 repetitions; index/refresh time separate; allocations via Go benchmark |

Report macro averages and per-question-type values, with lexical/hybrid mode, scope, budget, corpus size, source/model hashes, fixture revision and commit. Freeze a reviewed initial baseline; gate deterministic evidence/scope invariants and per-case/per-type quality regressions against that baseline. Do not invent a target Recall value before measuring it. Timing stays informational until stable runner-specific tolerances exist.

The final-context test must call the production path through `discussionContextFor`/assembly and `PrepareDiscussion`, then inspect the input passed to a **fake capturing adapter**, without invoking a model. Exercise local/common Conversation preparation, cloud Chat Completions preparation and harness frozen-context preparation. Use the same evidence sidecar at retrieved → packed → assembled → sent stages. A context citation whose text was dropped by budget must fail the strict presence metric. Repeat a memory update in an existing session both before and after explicit context refresh, recording the current freeze contract rather than silently changing it.

Optional semantic mode may use **only an already-installed local embedding model already configured for Brain**, with consent and recorded model/content digest; no automatic weights download or cloud embeddings. Default CI is lexical with semantic settings disabled. Small precomputed vector fixtures can test fusion/invalidation deterministically, but cannot be advertised as an embedding model's retrieval quality. Do not include extraction, consolidation, dreaming, query rewriting, reranking, completion or LLM grading in the free benchmark.

### 5.6 Exact proposed implementation files and commands

These are a future implementation specification, not files created by Mission C:

| Proposed file | Responsibility / dependency |
| --- | --- |
| `tools/brain-retrieval/import_longmemeval.py` | Python stdlib, offline input only; verify input SHA from manifest; deterministic subset/conversion/evidence sidecar; no network/model client |
| `internal/loom/brain/testdata/retrieval/manifest.json` | Selected IDs, upstream revision/licence/hash, selection version, converter revision and converted hashes |
| `internal/loom/brain/testdata/retrieval/cases.jsonl` | Approved full-history subset, or locally supplied licensed cache selected by env; gold kept separate |
| `internal/loom/brain/testdata/retrieval/evidence.json` | Gold session/span mapping, reviewed update/as-of annotations |
| `internal/loom/brain/testdata/retrieval/temporal.jsonl` | Eight synthetic cases, safe to commit independently of dataset permission |
| `internal/loom/brain/testdata/retrieval/baseline.json` | Reviewed deterministic predictions/quality counts, no noisy timing gate |
| `internal/loom/brain/retrieval_eval_test.go` | `TestRetrievalEvidence`, `BenchmarkRetrievalEvidence`; normal New/Refresh/Search/PackHits; JSON result output to caller-specified temp path |
| `internal/loom/brain_retrieval_context_test.go` | `TestBrainRetrievalFinalContext`; isolated stores, fake capturing adapters, frozen/update/budget/local/cloud/harness coverage |
| `docs/brain-retrieval-benchmark.md` | Dataset permissions, reproduction, metric meanings and baseline acceptance |
| `.github/workflows/ci.yml` (existing) | Add isolated offline evidence run alongside existing Go validation; no new runtime dependency |

Define `LOOM_BRAIN_RETRIEVAL_MODE=lexical` as predict-only test mode, `LOOM_BRAIN_RETRIEVAL_CASES` as optional offline corpus override, and `LOOM_BRAIN_RETRIEVAL_OUT` as JSON output path. Tests must clear relevant provider credentials, disable semantic settings, and fail if a network/model path is attempted. Use existing injected adapters/provider seams instead of adding a benchmark runtime layer.

Proposed one-time import after licence and source-hash review:

```sh
python3 tools/brain-retrieval/import_longmemeval.py \
  --input "$LONGMEMEVAL_LOCAL_JSON" \
  --manifest internal/loom/brain/testdata/retrieval/manifest.json \
  --output-dir internal/loom/brain/testdata/retrieval
```

Proposed offline CI/reproduction, with the corpus already present (no downloads):

```sh
RETRIEVAL_TMP=$(mktemp -d)
mkdir -p "$RETRIEVAL_TMP/home" "$RETRIEVAL_TMP/data"
env HOME="$RETRIEVAL_TMP/home" XDG_CONFIG_HOME="$RETRIEVAL_TMP/home/.config" \
  XDG_DATA_HOME="$RETRIEVAL_TMP/home/.local/share" LOOM_HOME="$RETRIEVAL_TMP/data" \
  GOTOOLCHAIN=go1.26.9 LOOM_BRAIN_RETRIEVAL_MODE=lexical \
  LOOM_BRAIN_RETRIEVAL_OUT="$RETRIEVAL_TMP/evidence.json" \
  go test ./internal/loom/brain -run '^TestRetrievalEvidence$' -count=1
env HOME="$RETRIEVAL_TMP/home" XDG_CONFIG_HOME="$RETRIEVAL_TMP/home/.config" \
  XDG_DATA_HOME="$RETRIEVAL_TMP/home/.local/share" LOOM_HOME="$RETRIEVAL_TMP/data" \
  GOTOOLCHAIN=go1.26.9 LOOM_BRAIN_RETRIEVAL_MODE=lexical \
  LOOM_BRAIN_RETRIEVAL_OUT="$RETRIEVAL_TMP/final-context.json" \
  go test ./internal/loom -run '^TestBrainRetrievalFinalContext$' -count=1
GOTOOLCHAIN=go1.26.9 go test ./internal/loom/brain -run '^$' \
  -bench '^BenchmarkRetrievalEvidence$' -benchmem -benchtime=1s
```

The test implementation must use `t.TempDir`, reset global stores/settings with cleanup, and supply a fixed clock. CI supplies its Go cache/module paths explicitly if changing HOME affects availability. The benchmark's own lexical fixtures must be isolated too. These exact future test names/env flags are not existing public Loom flags. Initial PR: importer/manifest/licence decision plus synthetic lexical cases; second PR: final-context capture and reviewed natural baseline. No product retrieval changes until these show a concrete gap.

## 6. Machines: SSH legacy and Loom Node

### 6.1 Existing dispatch and gaps

`RemoteMachine` retains SSH host/user/port plus NodeID/modules/handshake. There is no authoritative `kind` field separating two products. `remote_machines.go` provides SSH discovery, quoting and six remote harness recipes (Hermes, Claude, Codex, Pi, OpenCode, OpenClaw); npx recipes already reuse builtin pins. `usesNodeHarness` selects Node when NodeID exists and either the harness module exists or there is no SSH user. A pure paired Node without the module should report unavailable, not silently fall back to unauthorised SSH.

| Operation | SSH path checked | Node path checked | Decision |
| --- | --- | --- | --- |
| Agent transport | `remote_machines.go`, root ACP launch/probe | `node_bridge.go`, `node_harness.go`, authenticated ACP WebSocket and bounded owned child | Keep transport-specific IO; unify declarative recipes and capability decisions |
| Install/update | `harness_lifecycle.go`, Windows remote lifecycle helpers | `harness_lifecycle_node.go`, Node lifecycle service | Existing lifecycle service is shared; no replacement updater |
| Terminals | `terminal.go`, `terminal_pty_unix.go`, `terminal_pty_windows.go`, SSH command | `terminal_node.go`, `node_terminal.go` | Existing process seam covers PTY/ConPTY/SSH/Node; do not claim Node terminals are missing |
| Folders/workspace | `machine_folders.go`, `workspace_folders.go` | Node folder/workspace endpoints in `node_bridge.go` | Preserve one workspace UI and normalise operation results, not a universal shell API |
| Metrics | `machine_metrics_remote.go` script, `machine_metrics.go` | Node observe/module path | Keep unknown values unknown; share result projection |
| Environment/Docker checks | `env_docker.go` `remoteCommand`, `env_check.go` | No equivalent Node dispatch in these paths | SSH removal would break current remote Environment support; add narrow read-only Node operations first |
| Native usage | `harness_usage.go` remote OpenCode/Hermes commands use SSH; remote Claude/Codex/Pi log readings unavailable | No Node branch in the inspected command reader | Another concrete SSH dependency; represent unsupported readings explicitly before migration |
| Engine | direct linked SSH-era/external endpoint observation | `engine_node.go`, `machine_node.go` | Machine maintenance link already independent of selected inference engine; reuse it |

The fallback error text in `terminalCommand` referring to SSH is not evidence that Node terminal support is absent: `startTerminalProcess` dispatches Node earlier. Preserve hybrid saved machines and module-based eligibility when simplifying.

`machine_migration.go` provides an explicit Linux SSH-to-Node install/pair workflow preserving machine identity. Pairing/key/module/handshake logic exists in `machine_pairing.go`, `node_pairing.go`, `node_discovery.go`, `node_listener.go` and `machine_node.go`. Do not force migration, replace a working SSH link before pairing succeeds, delete user keys, or tie maintenance access to the currently selected engine. Selected credentials in Node launch records remain scoped/consumed; never mirror the whole environment.

### 6.2 Unify/remove in this order

1. Put reusable **harness recipe metadata** in the existing harness boundary; retain SSH shell quoting and Node argv/working-directory validation separately. Test the same six-family recipe matrix plus unsupported AGY/DeepSeek and pure Node missing-module cases.
2. Use existing machine/module projection for explicit per-operation availability. Standardise inventory/folder/metrics/lifecycle result fields only where actual duplicate consumers are shown; reuse current dispatch functions. Do not introduce a generic “run anything remotely” layer.
3. Add only missing read-only Node Environment/usage operations with bounded output and explicit permission/module checks, if product demand warrants it. Native sign-in remains explicit; missing remote logs remain unknown rather than fabricated zero usage. Require deterministic handler/transport tests before removing an SSH branch.
4. Remove orphaned legacy UI `MachinesSection` after static-import and browser verification; keep `MachineDialog` and the canonical Settings/Machines implementation. This is a small measurable deletion, not a second Machines redesign.
5. Remove duplicate probe/recipe code after equivalent results are proven. Retain SSH transport and migration indefinitely for unpaired/hybrid users until every operation's support matrix and policy allow retirement. No evidence supports deleting all SSH today.

## 7. Ranked incremental plan

Costs below are **planning estimates**, not measured effort. Difficulty and risk concern implementation; maintenance gains have explicit acceptance criteria. Every behaviour fix first gets a free deterministic failing test, demonstrated on the pre-fix checkout. No live paid-model tests are needed.

| Rank | Item / maintenance gain | Estimated cost | Risk | Difficulty | First PR-sized step and acceptance evidence |
| ---: | --- | --- | --- | --- | --- |
| 1 | Capability-specific recovery: prevent unrelated probes from restoring broken features; reuse current overlay | 1–2 days | Medium | Medium | Add failing approvals-vs-models recovery test, then restore only checked capabilities; inventory all adapter/probe/feature/UI consumers before schema change |
| 2 | Compatibility evidence and check-only updates: one support decision per installed capability, fewer misleading version warnings | 3–6 days, staged | Medium–high | High | Merge four compatibility constructors without behaviour change; add evidence fields/fixtures next; update-policy PR separately with no-auto-update regression, docs and CHANGELOG |
| 3 | Free Brain retrieval baseline: enables measured retrieval decisions and catches final-context loss | 2–4 days for first stage | Low product risk | Medium | Add offline importer/licence manifest and eight synthetic cases; measure lexical predictions, then natural subset and fake final-context capture; no runtime change |
| 4 | Shared native answer accumulator: reduce **3 collectors to 1** while preserving four protocols | 1–3 days | Medium | Medium | Extract replacement/append/item-close helper in existing `runtime`; run Codex/Pi/OpenCode/AGY deterministic fixtures, compare canonical output byte-for-byte |
| 5 | Harness metadata reuse: reduce **3 core launch/inspect catalogs** and cross-family omissions | 2–4 days | Medium | Medium | Unify one identity/launcher field across builtin inspect and remote recipes; matrix covers every family and preview, no new executable default |
| 6 | UI module split/dead-code removal: shrink 62,894-byte harness page and remove duplicate unused machine view | 1–2 days | Low–medium | Low | Extract Catalog/dialogs with unchanged markup; static import check, `make check-ui`, successful desktop/phone route crawl; delete orphan candidates only after verification |
| 7 | ACP SDK/TCK reuse trial: reduce bespoke protocol maintenance if net deletion and bounds preservation are proven | 2–4 days spike | Medium–high | High | Pin SDK/TCK outside runtime first; differential fake-agent transport fixtures, sanitised TCK evidence; report LOC delta and unsupported extension gaps before adoption |
| 8 | Machine operation parity: fewer SSH/Node recipe/result discrepancies without losing working SSH | 2–5 days per missing operation | Medium | Medium–high | Add pure-Node Environment availability test showing unsupported state honestly; implement one bounded read-only Node check only if needed, then remove equivalent duplication |
| 9 | Residency leaf extraction: easier state-machine tests and smaller root integration | 1–3 days | Medium | Medium | Move existing injected-clock state machine/tests to `engine/residency`, no behaviour changes; retain one llama supervisor and all root wiring |
| 10 | Brain scoring/temporal refinement: remove **2 BM25 formula sites**, then improve only benchmark-demonstrated stale-fact gaps | 1–2 days scoring; temporal cost unestimated | Low scoring / high temporal | Medium / high | Share pure scoring constants with unchanged rankings; require evidence baseline before temporal metadata or changed freeze semantics |
| 11 | Installer/vLLM leaf work: reduce root coupling only where repeated parsing/archive operations are demonstrated | Audit first; 2–4 days per proved leaf | Medium | Medium | Inventory archive/platform variants; extract one pure shared validation function and migrate its tests; no framework or new supervisor |

Ranks 1–3 establish trustworthy decisions before larger reuse changes. Metadata/accumulator/UI work can then be small independent PRs. SDK adoption, temporal graph memory, full supervisor replacement and SSH retirement are **conditional or rejected**, not assumed roadmap work. A file move without fewer duplicate edits, clearer test seams or lower dependencies does not satisfy reuse-first.

## 8. Transversal implementation inventory: checked versus changed

All entries below were indexed and their relevant dispatch/contracts/consumers inspected. **Changed: none** for every implementation; only this audit and the commit-message draft were written. No contract-changing recommendation may proceed without rerunning this inventory on its implementation branch.

| Contract/domain | Implementations and consumers checked |
| --- | --- |
| Runtime ownership/portable context | `workspace_runtime.go`, `workspace_native.go`, `workspace_sessions.go`, `workspace_prompt.go`, `workspace_frozen_context.go`, `workspace_routing.go`, `workspace_continue.go`, original `chat_conversation.go`/LLM client pipeline; `runtime/contracts.go`, `canonical.go`, `registry.go`, events; `discussion` prompt/session/turn helpers; policy and degraded projection |
| All agent families | Codex app-server + ACP; Claude ACP/extensions; Pi RPC + ACP; OpenCode HTTP + ACP; AGY native + Loom ACP bridge; Hermes ACP/chat/quota/usage; OpenClaw ACP; DeepSeek ACP developer preview; custom/registry ACP; local llama runtime and OpenAI-compatible cloud adapters. Checked launch JSON, inspect JSON, features, provider/model/skills/MCP/memory sinks, resume and usage switches; Gemini rejection |
| All current agent transports | local ACP stdio; ACP SSH including POSIX/Windows remote recipes; ACP over authenticated Node WebSocket; `runtime/agentstdio` with Codex app-server and Pi RPC; owned OpenCode HTTP server/SSE; AGY CLI stream and bridge; local Conversation adapter; configured Chat Completions adapter. Native detection/cache and probe projection inspected |
| ACP policy/session surface | `acp_registry.go`, `acp_session.go`, `acp_permissions.go`, `acp_files.go`, `acp_probe.go`, `agent_acp_compat.go`, `acp_compat.go`; `runtime/acp` protocol/session/events/replay/diffs/interactions/AGY/process files and tests; fake agents, capability/compatibility tests |
| Engine ownership and switching | `backend_*.go` serve/build/download/prebuilt/model/router/owned integration; `engine/llamacpp` args/config/help/router/model/observation/supervisor/slots; `engine.go`, `engine_service*.go`, `engine_direct.go`, `engine_node.go`, `bench_node.go`, `engine_vllm.go`/params/library/download/update; cloud catalog/model source eligibility and local chat selection |
| Brain and context stores | `brain` engine/search/text/scope/semantic/Markdown memory+context+transcript/legacy memory+migration/MCP; root `brain_http.go`, `brain_store.go`, `brain_semantic.go`, `brain_items.go`, `brain_context.go`, `brain_transcripts.go`, `brain_distill.go`, `brain_consolidation.go`, `brain_agents.go`; workspace freeze/assembly and discussion preparation |
| Persistence consuming these contracts | `store` bbolt/cache/files/crypto, root `store_compat.go`, state/runtime/chat records; private Brain source/index/vector storage, primary-vault/fallback Markdown memory and migration-only legacy memory/distilled data; `mem_store.go`, `chat_memory.go`, `tools/memory.go`, `mcp_session_memory.go`; remote-machine/key/lifecycle/probe/compatibility storage and Environment store. Legacy stores were not assumed to be active Brain fact semantics |
| Machines/terminal surfaces | `remote_machines.go`, `machine_node.go`, `machine_pairing.go`, `machine_migration.go`, `machine_metrics*.go`, `machine_folders.go`, `workspace_folders.go`; `node_bridge.go`, `node_harness.go`, Node lifecycle/discovery/listener/pairing/terminal; PTY/ConPTY/SSH/Node terminal selection; Environment/check/Docker and native usage commands |
| UI runtime/feature consumers | `core/state.js`, route registry, chat engine/execution/picker/requests/tools/messages/composer/view; harnesses page/account/history/lifecycle/lifecycle-state/options/machines; inspector selection/config/context/params; usage page/refresh; local/cloud pages/catalog/service; resources brain/memory/memory-items/skills/MCP/preferences/pages; settings page/machines/doctor/policy/updates/vLLM and onboarding |
| Other UI routes/application consumers | Machines page/panes reusing Settings machine implementation, workspaces/folders, terminals, Environment, projects, tasks, bench, voice/Jarvis, preview panel; all route records and route crawler list. These were checked for ownership/reuse, not a new functional browser certification |
| Monitoring and regression infrastructure | `tools/agents-watch` capability/schema/probe/pin/publication normalisation and tests, scheduled workflow; runtime fixtures/handshake/lifecycle tests; engine/router/service tests; Brain search/vector/memory/scope benchmarks/tests; UI Node tests, route crawler, CI/doc-link tooling |

Useful future transversal commands are `rg --files internal/loom`, `rg -n 'CompatibilityRecord|CapabilityChecks|HarnessFeatures|observeAgentProbe' internal/loom tools/agents-watch`, `rg -n 'remoteHarnessDefs|usesNodeHarness|RemoteMachine|NodeID' internal/loom`, and searches for every changed JSON key in `ui/next/js` and tests. An implementation PR must list the actual changed subset; this report does not pre-authorise omissions or a broader rewrite.

## 9. Validation performed and remaining limits

Validation used **Go 1.26.9** (`GOTOOLCHAIN=go1.26.9`), Node v22.23.1, temporary HOME/XDG/Loom data/cache paths; existing module cache was supplied explicitly. No real `~/.pi`, `~/.hermes`, `~/.dsh`, `~/IA` or Loom Node data was used. Logs and generated binary artifacts are excluded from the report deliverables.

| Command/check | Observed result |
| --- | --- |
| `go vet ./...` | Passed |
| `go test ./...` | Failed because sandbox denies sockets; first root panic `TestHandleHubSearch`, `backend_hub_test.go:111`; platform panic `TestFetchReleaseChannels`, `platform/sys_update_channel_test.go:11`. Both: `listen tcp6 [::1]:0: socket: operation not permitted`. Root/platform completion is unverified, not a pass |
| Remaining package results from that run | Brain, capability, diagnostics, discussion, doctor, engine/llamacpp, events, harness, notify, policy, project, resources, runtime and ACP/AGY/Codex/local/OpenAI/OpenCode/Pi children, store, tools, web and agents-watch passed; agentstdio has no standalone test files |
| Focused root deterministic runs | Eight tests passed: HarnessFeaturePaths, HarnessDiscoveryAndProbeProjection, NativeResumeCommand, ACPCompatibilityPinsAndWarnings, ACPCompatibilityRetainsActualLauncherPin, BrainVectorCheckpointsResumeAndModelInvalidation; then HarnessQuotaCapabilities and GeminiOffersAreRejected to verify the family inventory |
| Existing Brain benchmark | Passed; exact measured results in §5.3 |
| `make build` | Passed; embedded UI Linux binary built |
| `GOOS=windows go build -o <temporary-output> ./cmd/loom` | Passed; compile-only, no Windows runtime claim |
| `make check-ui` | Passed; 21 Node test-file subtests, not a count of assertions |
| `npm ci --prefix tools/tests/ui-routes --no-audit --no-fund` | Failed: DNS `EAI_AGAIN` for npm registry/Playwright dependency |
| `node tools/tests/ui-routes/crawl.mjs bin/loom` after build | Attempted; failed `ERR_MODULE_NOT_FOUND` for `playwright`; no routes/browser were exercised. The crawler also requires sockets, so this sandbox would need that restriction lifted on a suitable validation runner |
| `gofmt` | No Go files changed; no formatting mutation required |
| Documentation/patch checks | Repository documentation checker passed (188 local targets, none missing); new report's local-target scan and both deliverables' whitespace checks passed; `git diff --check` passed. New files were checked explicitly because they are untracked |

Reproduction of focused checks (inside the isolated environment described above):

```sh
GOTOOLCHAIN=go1.26.9 go test ./internal/loom -count=1 \
  -run '^(TestHarnessFeaturePaths|TestHarnessDiscoveryAndProbeProjection|TestNativeResumeCommand|TestACPCompatibilityPinsAndWarnings|TestACPCompatibilityRetainsActualLauncherPin|TestBrainVectorCheckpointsResumeAndModelInvalidation)$'
GOTOOLCHAIN=go1.26.9 go test ./internal/loom/brain -run '^$' \
  -bench '^BenchmarkBrainSearch$' -benchmem -benchtime=100ms
```

Still unverified: full root/platform tests after socket denial, browser/phone route behaviour, real installed harness compatibility, TCK conformance, SDK integration delta, actual remote-machine parity, GPU engine lifecycle, natural-history retrieval quality, temporal extraction quality, dataset redistribution rights and optional embedding-model quality. The proposals are bounded by these gaps; none is presented as an implemented fix or measured replacement benefit.
