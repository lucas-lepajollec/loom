# Agents v2 step 3 verification

Antigravity now prefers the installed CLI's documented stream JSON input/output
protocol, with Loom's existing ACP bridge as fallback. See
[agent compatibility](agents-compat.md#agents-v2-step-3-antigravity) for the path
comparison, accepted mapping, native session continuity and capability gaps.

## Live observations (no generation)

- Installed `agy --version`: **1.3.1**.
- `agy --help`, `agy help models`, and `agy changelog` were read. Help advertises
  stream JSON input/output, explicit conversation resume, native models, effort
  `low|medium|high|xhigh|max`, execution modes and terminal sandbox.
- Installed binary strings/types were inspected for the print/stream input and
  output payloads. The temporary inspection dump was removed after use.
- Read-only `agy models` was attempted. Native startup failed with
  `listen tcp 127.0.0.1:0: socket: operation not permitted`; native log/crash
  directories were also read-only. A live model catalog was not obtained.
- Google's headless documentation and ACP registry distribution were inspected.
  The separate Google ACP server was not installed, started or authenticated.
  The supplied study/Poracode sources were read without wholesale copying.
- No live agent turn, quota-consuming prompt, login, permission mutation,
  quota reset or provider-key export was performed.

## Fixture and application checks

Synthetic NDJSON exchanges under
[`testdata/agents/antigravity`](../internal/loom/testdata/agents/antigravity/README.md)
run against fake owned processes, validating one user input and EOF. They cover:

- Normal/final-only/corrected text, native conversation IDs, per-step usage
  snapshot deduplication and cumulative result counters retained separately.
- Commands, file targets without filesystem reads/diff reconstruction, tool
  failures, unavailable headless permission/questions and raw denial metadata.
- Verbatim model/busy errors, interruptions/cancellation, missing results,
  unknown/future frames and nonzero exit after a success result.
- Malformed JSON, bounded credential-redacted stderr, missing/mismatched IDs,
  nullable/invalid counters and explicit launch policy.
- Builtin native selection versus old CLI fallback, read-only version/catalog
  probes, drift warnings, effort/sandbox configuration, native resume with only
  the new prompt, and explicit text handoff after context changes.
- Old structured-output CLI bridge input through one-shot `--print`.
- Canonical legacy display of errors and separation of turn spend from context
  occupancy. Existing request/UI field shapes remain unchanged.

## Commands and results

The Go build cache was redirected to a writable temporary cache because the
normal user cache is read-only in this sandbox. No dependency files changed.

| Check | Result |
| --- | --- |
| `gofmt` on changed Go files; `git diff --check` | Passed |
| `go vet ./...` | Passed |
| `go test ./internal/loom -run 'TestAgent\|TestACP\|TestAntigravity\|TestCodex\|TestNative' -count=1` | Passed |
| `go test -race ./internal/loom/runtime/antigravity -count=1` | Passed |
| `go test ./internal/loom/...` | Runtime/other leaf packages passed; root/platform failures below |
| `make build` | Passed; embeds the unchanged UI |
| `make check-ui` | Passed |
| `python3 tools/check-doc-links.py` | Passed |

The required full-suite command encounters existing environment failures:

- `TestHandleHubSearch` (`internal/loom`) and `TestFetchReleaseChannels`
  (`internal/loom/platform`) panic when httptest tries to bind a socket:
  `listen tcp6 [::1]:0: socket: operation not permitted`.
- `TestEngineKindFullVsServer` detects the pre-existing `/tmp/.git` ancestor as
  a full checkout, returning `full` instead of the expected standalone `server`.
  Engine/source-discovery code was not changed.

A concurrent Go validation attempt also exceeded the sandbox disk quota. The
owned temporary binary-inspection dump was removed and focused tests, vet,
build and race validation were rerun sequentially successfully. Unrelated
files/caches were preserved.

## Acceptance limits

No live generation, native IDE/CLI simultaneous-window locking or cross-window
resume was exercised. Resume and busy/lock behavior are fixture-verified. The
headless protocol has no permission/question reply channel or accepted separate
reasoning, structured plan or context-occupancy schema; those capabilities are
not fabricated. Unknown fields retain bounded raw rows. Native CLI MCP remains
CLI-owned; explicit Loom MCP selection fails visibly. Antigravity has no accepted
session-list/history-read API here; the old bridge did not provide usable
history discovery/replay either.

Backend only. The existing configuration list now includes `reasoning_effort`
(select) and `sandbox` (boolean); no new top-level UI fields or UI edits. No
commit was made; `COMMIT_MSG.txt` contains the proposed commit message.
