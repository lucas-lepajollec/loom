# Control plane review

## Unreleased backend cleanup

### Removed surfaces

| Removed | Retained callers and data |
| --- | --- |
| Environment service registration and deletion, reachability checks and matrix, Docker inventory, Proxmox settings, resources and certificate fingerprint probe. The removed `/api/env/` routes were `services`, `services/delete`, `check`, `matrix`, `docker`, `proxmox`, `proxmox/resources` and `proxmox/fingerprint`. Their store, probes and tests were removed. | Machines still supplies inventory, metrics, terminal, voice and engine controls. |
| `/api/tasks` and `/api/tasks/stream`, their snapshot DTOs, progress calculation, history projections, SSE recorder and page-specific tests. Non-session notification links to the retired page were removed. | Live discussion turns, event replay, policy requests, notifications, Codeman schedules and Activity still have consumers. |
| `/api/projects/context`, `/api/projects/info` and `/api/context/preferences`. Removed the repository inspector, context-file and directory selection helpers, preference editor, inherited prompt assembly and their editor tests. | Session folders still use `/api/projects`, `/api/projects/rename` and `/api/projects/delete`. Stored folder IDs, transcript paths and legacy metadata remain readable. Discussion instructions, portable history and explicit refresh remain available. |
| Under `/api/brain/`, removed `sources`, `reindex`, `search`, `pack`, `read`, `distill`, `distilled`, `distilled/delete`, `distilled/review`, `memory` and `memory/delete`. Removed remote Git checkout and sync, page-specific filters, selection, model-assisted distillation, review and mutation tests. | The MCP gateway and retrieval tools still use the source index. Semantic settings control embedding consent and background refresh. Markdown memory, transcripts and consolidation still use the read-side migration for old records. |
| The previous class-based memory writer, edit, delete, history, filter, touch and import-marking operations and their private DTOs and tests. Removed the distilled JSON writer. | Migration reads fixed legacy-format fixtures, including an encrypted fallback, through the production reader. Old files are archived by the existing migration. |
| `/api/bench/catalog` and its model catalog filter, obsolete Artificial Analysis credential-header and quota handling, and the corresponding page documentation. | Activity and workers still use benchmark definitions, runs and queues. Model watch reads the public OpenRouter and Epoch AI feeds. |
| Unreferenced `tools/generate-codex-types.py`, `docs/screenshots/brain.png` and retired page instructions. Removed the empty startup goroutine and unused adapters for process execution/termination, unused Unix detached-process and PID checks, Unix home probing, release lookup, asset naming, update errors, memory filenames, skill slugs, machine lookup and MCP selection validation. Removed unused native-context, test-stream and frozen-turn helpers. | The platform-specific restart and tray paths, historical event readers and migration fixtures still serve supported callers. UI response-shape tables and phone fixtures still mention retired routes. |

### Security findings corrected

| Boundary | Finding and correction |
| --- | --- |
| Control and engine relay | Method and install/update policy checks could be skipped by linked-engine dispatch. Action methods now run before routing. The relay authorizes the captured machine before forwarding. Selected-node update check, ping and apply enforce their respective methods. Engine control responses reject redirects, strip upstream cookies and sandbox the returned content. |
| Files and node modules | Files writes now require POST at both entry points. Directory browsing and workspace creation require the worker terminal/files module. Module changes require an explicit boolean. Drive-relative strings are rejected as remote work directories. |
| File access | Dot and parent components are checked before path cleaning. Confined reads check the opened descriptor for regular-file type. Note writes use private exclusive temporary files, claim new names without replacing existing notes and use distinct trash names. Listings and tree traversal read bounded batches. |
| Machine Files relay | Forwarding is restricted to the selected, validated node and one hop. It uses the node credential, drops browser cookies, refuses redirects and applies sandbox and no-sniff headers to returned content. |
| Credential disclosure | Browser key status and updates expose masked metadata. Explicit generation returns the new credential. The peer handshake requires a control Bearer with no browser Origin. Legacy inference and crawler key fields are filtered from live settings, preset views and responses from older peers. Editing masked settings preserves their stored values. CLI setters stopped echoing supplied keys. |
| Codeman | Gateway policies and upstream requests now use the same decoded path. Ambiguous paths and unsupported methods are rejected. Settings responses redact the voice key on reads and writes. A saved password is bound to the configured address and account, including reconnect and missing or damaged configuration. Bodies and header waits are bounded and proxied content gets no-store, sandbox and no-sniff protections. |
| Market feeds | Fixed public source URLs use public-IP validation at dial time, including redirected connections. DNS is resolved once per dial and the client connects to the checked IP. JSON/archive limits and fetch deadlines bound the reads. Failed watch-preference saves are reported. |
| Local state and request limits | New private application directories, memory, snapshots, preset files, exports, device cache, note files and service logs/PIDs use owner-only modes where the OS implements them. Files writes receive their editor-specific request allowance. The worker inference body limit covers both direct and native backends. |

### Accepted boundaries

- Loom has a single owner. An unconfigured local UI requires both a loopback Host and loopback peer. A configured browser uses its authenticated session and origin checks. The public shell, sign-in state and entry points exist to establish or end a session.
- Pairing exchanges an expiring, single-use code. Notification answers use scoped action tokens. Terminal and preview access use separate scoped grants. The MCP and inference interfaces have their own credentials. These entry points do not confer general management access.
- Machine-wide browsing and terminal workspace creation intentionally expose paths available to the service account. The selected Files folder uses `os.Root`. Existing operator-managed folders retain their permissions. Windows access also depends on account ACLs.
- The owner can configure a LAN or loopback node, engine or Codeman server. Those connections deliberately allow private destinations. The configured peer receives its designated credential. Cleartext LAN HTTP relies on a trusted network. A self-signed Codeman certificate is supported only at a loopback address, which places trust in local processes. Public market fetches have a different trust boundary.
- An explicit key-issuance operation must hand its new credential to the owner. Pairing and peer handshake similarly need to deliver a credential to the peer. Visible user-authored transcripts, notes, terminal output and preset text may themselves contain sensitive content. They are owner data and do not undergo general secret scanning.
- A global server write timeout would interrupt quiet but valid SSE, long polls, generation and terminal sessions. Request contexts, stream revocation and operation-specific limits apply. Disk modes on pre-existing shared directory trees are left under operator control.

Regression coverage includes the main and worker muxes, an in-memory legacy peer, Codeman settings and destination changes, private file creation, market address rejection, and encrypted legacy-memory migration. Deployed reverse proxies, real Codeman servers, remote nodes and host ACLs require separate operational verification.

## Historical review — 2026-10-03

> Historical audit evidence: coding harnesses and their integration fixtures
> described below were retired by the unreleased Local/Cloud transition.
> See the harness-retirement migration notes for the current scope.


This is a source audit and remediation record for the changes following
v0.1.4, starting at `0ae7f23`. It covers the Go control plane, embedded UI,
engine-only worker, execution adapters, Brain, storage, installers and CI.
It is not a penetration-test certification or acceptance of every operating
system, native agent or deployed reverse proxy. All reproductions use synthetic
state, loopback fixtures or owned subprocesses; no real account credentials,
model data or remote machines are used.

## Trust and authority

Loom is a **single-owner control plane**. An authenticated owner can launch
terminals, configure executable harnesses/MCP servers, install engines and access
chosen files. Those are privileged capabilities, not ordinary chat permissions.
Run Loom as an ordinary OS user; do not give its service unrestricted sudo.
The Linux update helper is a narrow exception for official release replacement.

| Boundary | Contract checked | Evidence / limits |
| --- | --- | --- |
| Browser → control API | Password sessions, separate control Bearer, revocation, same-origin mutations, anonymous loopback gate | `web_login*`, `web_local_access*`, `web_control_*` tests |
| Inference → management | Separate `/v1` credential; worker rejects browser origins and has no discussion/Brain routes | `engine_worker*`, `engine_node*`, OpenAI front tests |
| Browser → shell / preview | Expiring single-use tickets, isolated preview origin, stripped credentials, revoked streams | Terminal and real WebSocket/SSE tests; HTTPS routing still operator-owned |
| Loom → remote engine | Explicit endpoint and separate machine credential, no credential redirects or browser-cookie forwarding | `engine_node*`, direct-engine fixtures |
| Harness → files / approvals | ACP `os.Root` confinement for client file methods; native filesystem modes and escalation remain separate | ACP file/mode/approval tests; a CLI's own tools are governed by that CLI |
| Sources → Brain / providers | Read-only source ingestion, personal-source opt-in, provenance, bounded index; cloud transfer requires consent | Brain/context/semantic tests; filenames are exclusions, not a general secret scanner |
| Storage / recovery | Transactional bbolt updates, private DB permissions, independent optional vault, invalid auth fails closed | Store, vault, migration and update tests |
| Installer / updater | Official release/checksum path, root-owned fixed helper, no arbitrary destination, rollback | Platform, privileged-path fixtures and isolated node installer tests |
| UI / PWA | Raw Markdown HTML escaped, safe links, attachment downloads, no private-data offline cache, anti-framing | Vendored-parser payload tests, web assets and PWA checks |
| Supply chain | Patched supported Go, pinned ACP bridges and Actions, dependency monitoring | `govulncheck`, Go module inventory, CI and Dependabot |

## Corrected findings

Severity here describes the inspected Loom data flow, not a scanner's generic
severity score. These fixes are source changes until a new binary is released
and installed.

| Finding | Impact | Correction |
| --- | --- | --- |
| A01 — anonymous local API accepted arbitrary Host | High: DNS rebinding could satisfy same-origin checks against an open loopback API | Require a loopback Host **and** direct loopback peer for access without credentials; ignore forwarded headers; removal of the last credential cannot open remote access |
| A02 — legacy actions accepted safe HTTP methods | High: ambient browser credentials could authorize state changes through GET, including vault decryption and MCP execution | Require POST for action routes in main and worker; preserve explicitly dual read/write routes such as remembered model settings |
| A03 — accepted terminal and discussion streams outlived owner access | High: logout/rotation did not revoke a terminal ticket or existing stream | Bind transient grants to the credential generation and browser session; validate pending tickets, each terminal input and quiet connections; close terminal/SSE/preview WebSockets on revocation, normally within one second; bound terminal tickets to 128 |
| A04 — ignored decoder errors and unbounded legacy JSON | Medium: malformed requests could execute with default values; large inputs consumed memory | Validate complete JSON before legacy action/proxy dispatch; cap control requests at 1 MiB and chunked uploads at their existing 16 MiB encoded-request limit |
| A05 — download checked a name and reopened it | Medium: a symlink substitution could escape the workspace download boundary | Open through `os.Root`, require a regular file and use that same handle for stat, base64 chunks and HTTP ranges |
| A06 — output limits applied after subprocess completion | Medium: noisy shell output could exhaust RAM; Crawl4AI responses were unbounded | Retain a bounded live shell tail, preserve useful final output, cap crawler success/error responses at 32 MiB/64 KiB |
| A07 — vulnerable build toolchain | Multiple reachable standard-library advisories in a Go 1.25.0 source scan | Require Go 1.26.8+; update x/crypto to 0.56.0; rerun reachability scans and add a CI gate |
| A08 — remote-engine redirects | Medium: management/inference clients could follow a changed endpoint with credentials | Refuse redirects on the explicit linked-engine HTTP client, including sibling hosts |
| A09 — inbound ACP request flood | Medium: a faulty/malicious agent could create unlimited blocked permission handlers | Keep ordered notifications responsive but disconnect when more than 32 inbound requests are outstanding |

The patched Linux source scan reports **zero reachable vulnerabilities** and
zero vulnerabilities in imported packages. Its verbose report retains one
module-only advisory for the unmaintained `x/crypto/openpgp` package
([GO-2026-5932](https://pkg.go.dev/vuln/GO-2026-5932)); Loom does not import that
package. SSH module advisories are addressed by x/crypto 0.56.0. This observation
is dated and does not cover separately installed native engines/harnesses or all
vendored JavaScript advisories.

The Go update follows the [supported release policy](https://go.dev/doc/devel/release)
and [call-graph vulnerability analysis](https://go.dev/doc/security/vuln/).
Checksums authenticate downloaded bytes relative to the selected release's
checksum manifest; they are not independent signatures or protection against
a compromised upstream release account.

## Performance measurements and changes

Synthetic benchmarks use bounded corpora on one Linux amd64 development host,
Go 1.26.8, two short samples. They are comparative measurements, not deployment
latency guarantees. Run them without models or external services:

```sh
go test ./internal/loom/brain -run '^$' -bench BenchmarkBrainSearch -benchmem
go test ./internal/loom -run '^$' -bench BenchmarkRuntimeDeltaProjection -benchmem
```

| Operation | Before | After | Reason |
| --- | --- | --- | --- |
| Search common term, 60,000 chunks, 10 hits | 15.9–17.2 ms; ~10.2 MiB allocated | 1.8–2.0 ms; ~0.60 MiB | Dense scores plus bounded top-results heap; stable ranking/ties retained |
| Quoted search, same corpus | ~152 ms; ~68 MiB allocated | 22.5–24.2 ms; ~0.60 MiB | Normalize candidate text once; ASCII fast path; avoid sorting/retaining all matches |
| Display projection of one ACP delta with 2,000 retained events | ~12.8 ms; ~7.8 MiB allocated | ~39 ns; 8 bytes allocated | Delta projection no longer clones the entire conversation/journal; metadata events still copy immutable current-turn provenance |

The last row measures **projection only**, not the full event handling or storage
pipeline. Synchronous ACP journal persistence still occurs. No crash-recovery
semantics were weakened by batching writes in this change. Shell quoting also
reuses its compiled validation expression instead of compiling it per argument.

## Remaining work and operational limits

1. **Runtime lock and journal cost (performance priority):** `runtimeSessions.mu`
   still covers some workspace SSH preparation and synchronous bbolt persistence.
   Slow SSH can delay unrelated session actions for its bounded timeout; long ACP
   histories still serialize and fsync on updates. Next: separate preparation
   from compare-and-set state publication, then design an append journal with
   explicit durable replay/migration semantics. Do not casually keep bbolt open:
   its current open/close contract permits separate Loom CLI processes to work.
2. **Execution authority:** native agent modes are not universal sandboxes.
   Local model shell tools, terminals and custom MCP commands have Loom's OS-user
   authority. Textual command/path guards are convenience rules and do not
   confine arbitrary shell programs. Strict read confinement remains unavailable
   where upstream lacks it. Unknown capabilities remain unsupported.
3. **Transport and secrets:** remote passwords/tokens need HTTPS or an authenticated
   private tunnel. HTTP on a trusted LAN remains an explicit supported tradeoff.
   SSH uses trust-on-first-use (`accept-new`) and rejects changed host keys;
   first-contact identity verification remains the operator's responsibility.
   The optional vault does not encrypt every external source, native store or
   engine-link record. Protect the complete data directory and its backups.
4. **Third-party execution:** native CLIs, model engines, bridge packages, vendored
   UI libraries and MCP dependencies need their own version/advisory lifecycle.
   Arbitrary user-selected forks and install commands execute upstream code.
   Loom's Go scan cannot certify those programs or their supply chains.
5. **Architecture:** neutral runtime events and typed sinks now align the actual
   execution contract, but `internal/loom` still contains roughly 60,000 lines
   of application orchestration. Prioritize extraction of auth/control routing,
   session coordination and installer ownership behind tested contracts; avoid
   another whole-repository rewrite or a parallel inference/memory engine.
6. **Acceptance:** full source/race/UI checks and cross-compilation are distinct
   from real Windows/macOS sessions, GPU/vLLM installation, user-service boot,
   native account login and a deployed HTTPS preview route. Those device and
   infrastructure checks remain separate. Updating Git does not update an
   installed binary or an existing published release.

## Regression and maintenance gates

The remediation suite exercises malicious Host/peer combinations, action-method
and malformed-body rejection, owner logout with pending and live terminal access,
SSE and HMR revocation, credential redirects, confined downloads, native
permissions, inbound ACP flooding, Markdown payloads, ranking and immutable
provenance. Existing installer rollback, data preservation, Brain privacy and
cloud/provider tests remain in the complete short suite.

CI now includes the full short suite, vet, full race suite, pinned govulncheck,
UI/demo checks, build, documentation links and isolated node installation.
Dependabot tracks Go modules and pinned GitHub Actions weekly. These are ongoing
regression gates, not proof that no future or unknown vulnerability exists.
