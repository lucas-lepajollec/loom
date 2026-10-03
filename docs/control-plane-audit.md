# Control plane audit — 2026-10-03

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
