# Repository hygiene report

Date: 2026-10-01. Branch: `codex/hygiene`; the worktree was clean at the start.
This pass changes documentation, ignore rules and build metadata only. No commit
or push was made. No Go implementation, UI JS/CSS, ACP, remote-machine,
project-link or model-sink implementation was edited.

## Inventory and decisions

“Keep” includes items retained for a maintainer decision; lack of a text
reference alone does not prove that a specification or optional tool is obsolete.

| File or directory | Decision | Reason / evidence |
| --- | --- | --- |
| `README.md` | Update | Described ordered JS assembly, old navigation, process replacement for all model switches and memory-only cloud keys; current routes, `web_next.go`, router and keyring code contradict these claims. |
| `Makefile` | Update | Add `make help` for every real target; preserve `make test` as the full suite and `api` as the web-command alias. |
| `.gitignore` | Update | Keep existing binary/database/model/log exclusions; add PID files, legacy key files, root migration config and generated router INI, and anchor the local runtime directory. |
| `CONTRIBUTING.md` | Update | HTTPS clone needs no contributor SSH key; document short/full tests, all-package vet and direct UI embedding. |
| `RELEASING.md` | Update | Replace obsolete UI assembly instructions with `make check-ui` and direct embedding. |
| `.github/workflows/release.yml` | Update / remove obsolete steps | Remove three calls to the absent `tools/assemble-ui`; each platform already compiles `cmd/loom`, which embeds the current UI. |
| `.github/workflows/ci.yml` | Keep | Already runs short tests, vet, demo checks, build and the current UI check; Linux tray dependencies warrant separate cleanup review. |
| `SECURITY.md` | Update | Private-repository assumptions would mislead public visitors; retain private disclosure channels without claiming they have been enabled. |
| `CHANGELOG.md` | Update | Record the onboarding/build-metadata corrections without inventing a release. |
| `docs/architecture.md` | Update | Clarify target versus current packages, actual registration entry point, delivered fitting defaults and existing optional keychain storage. |
| `docs/workspace-architecture.md` | Update | Label historical slices and correct direct embedding, SSE, router lifecycle, cloud key storage and current ACP registration. |
| `docs/agents/implementation-brief.md` | Update | Steps 1–5 were still imperative work items despite being delivered; mark historical status and point to the roadmap. |
| `docs/ROADMAP.md` | Keep | Current product/work inventory; broad real-platform/provider acceptance claims need owner review, not a hygiene rewrite. |
| `AGENTS.md` | Keep | Public maintainer guidance is still referenced; its preparatory-harness/memory-only-era boundaries need reconciliation with the roadmap and current code. |
| `docs/agents/harness-acp-brief.md` | Keep | Design/reference brief used by the ACP notes; do not discard it while ACP work is active. |
| `docs/agents/acp-implementation.md` | Keep | Referenced lifecycle/API notes; frontend-only-backend-slice and deferred-skills descriptions need review by the agents editing those features. |
| `docs/agents/acp-schema/schema.json` | Keep | Explicitly referenced protocol specification; documentation references are intentional use even though it is not compiled or embedded. |
| `docs/agents/acp-schema/v2/schema.unstable.json` | Keep / human decision | `git grep` found no filename reference and no embed directive uses it; an unstable protocol reference may be intentional, so removal is not treated as safe. |
| `tools/gen-icon/main.go` | Keep | `cmd/loom/main.go` invokes it with `go:generate`; it is also a Go package covered by `go build ./...`. |
| `cmd/loom/resource_windows_*.syso`, icons, `versioninfo.json` | Keep | Windows resource/build inputs and generator outputs, not disposable runtime binaries; Windows compilation verifies the package. |
| `staticcheck.conf` | Keep / human decision | Optional staticcheck configuration is automatically consumed when that tool runs; no current CI invocation does not make it a dead script. |
| `internal/loom/ui/marked.min.js`, `sw.js`, manifest, offline page, fonts | Keep | Explicit `go:embed` in `web_server.go`; Marked also has a served route, and PWA/fonts have live handlers. |
| `internal/loom/ui/next/` and `ui/tests/` | Keep | Default UI embedded by `web_next.go`, plus the tests run by `make check-ui`; no JS/CSS edits. |
| `internal/loom/ui/src/`, `tools/assemble-ui/`, `/classic` | Keep absent | No tracked old UI/assembler files exist; surviving names in historical docs describe their removal. |
| `docs/ui.png`, `loom-server.png`, `loom-models.png`, `loom-bench.png` | Keep / human decision | README still uses all four and already labels them as older inference-control screenshots; replacement requires current screenshots. |
| `demo/` | Keep; update `demo/README.md` | Independent fictional browser demo, referenced in README and CI/release checks; remove unexplained “Ecosystem Pass 09” terminology from its README. |
| `install.sh`, `install.ps1` | Keep | README release instructions and release acceptance depend on them; publication/installer acceptance is a separate owner decision. |
| `.project-local/`, `bin/`, local config/model/database/log files | Keep ignored | Runtime/build material, not public source; no tracked files matched the runtime-artifact inventory. |

No tracked file was deleted. Candidates were checked with `git ls-files`,
`git grep` and the Go embed/generate declarations; builds validate the packages
and direct embedding. The only removed material is the three obsolete workflow
steps calling an already-deleted tool. A repository-wide filename search does
not justify deleting vendored references, optional configuration or embedded
assets.

## Changes applied

- Corrected source-build prerequisites, release availability wording, the
  navigation entry points, runtime data resolution and dotenv precedence in
  README. Documented a development runtime under ignored `.project-local/`.
- Distinguished router API model loading from the legacy single-mode process
  replacement path; described the optional OS keychain accurately.
- Documented native ES-module embedding with no asset assembler or Node build
  requirement. Added architecture links and corrected the contributor/release
  commands. Added `make help` without changing other targets' behavior.
- Removed obsolete release-assembler invocations, retained all platform build
  commands, and added narrowly scoped runtime ignore patterns.
- Marked delivered/historical migration work, retained protocol references and
  softened private-repository assumptions in the security policy. Added the
  corresponding changelog note.

## Credential scan

Ran the requested `git grep -nI -E` credential-assignment pattern over tracked
working-tree text. Captured matches privately and emitted only file/line
locations; no matched value was printed.

One match: `internal/loom/ui/tests/next-settings.test.mjs:263`, a synthetic
request payload in the authentication-retry test. It is a test fixture, not an
account credential, and was retained. No real credential was identified by this
pattern. No tracked `.env`, database, model, PID, log or local-runtime artifact
was found. This scan does not cover ignored files, binary contents or Git history.

## Human decisions remaining

1. Reconcile `AGENTS.md` and the older workspace/ACP slice notes with current
   harness execution, keychain storage, project folders and skill sinks. The
   roadmap and active implementation have advanced beyond parts of those notes;
   their owners should decide which historical detail to archive or replace.
2. Decide whether the unstable ACP v2 schema and optional staticcheck policy are
   useful public references; retain them until that choice is explicit.
3. Capture current screenshots and decide which older inference views to retain.
4. Confirm release publication, supported versions, real Windows/macOS installer
   acceptance, OS keychain acceptance and private vulnerability-reporting
   availability. No GitHub workflow or installer was executed by this pass.
5. Review unnecessary GTK/AppIndicator setup in Linux CI/release jobs separately;
   the current Linux tray implementation is a pure-Go fallback. No dependency
   installation or removal was performed here.
6. Run the Go suite in an environment permitting loopback sockets and review
   `TestEngineKindFullVsServer` with an uncontaminated temporary directory: this
   environment has a pre-existing `/tmp/.git`, so ancestor repository detection
   classifies its standalone test binary as a full checkout. No runtime/test
   implementation or pre-existing temporary files were changed.

## Verification

| Command | Result |
| --- | --- |
| `go build ./...` | PASS with writable caches. |
| `GOOS=windows go build ./...` | PASS; cross-compilation, not real Windows runtime acceptance. |
| `go vet ./...` | PASS with writable caches. |
| `go test -short ./...` | NOT GREEN: local sockets are blocked (`httptest`, `socket: operation not permitted`) in `loom` and `platform`; `TestEngineKindFullVsServer` also fails because of the pre-existing `/tmp/.git` ancestor. |
| `make check-ui` | PASS; module syntax checks and all four Node test files, zero failures. |
| `make help` | PASS; lists all real targets and the default. |
| `git diff --check` | PASS. |
| `git check-ignore` for binary/local runtime/key/config/router/PID paths | PASS. |

The initial native build/vet/test attempts could not create ccache files outside
the writable sandbox. Only those three failed commands were retried once after
placing both Go and ccache caches in a temporary writable directory. Windows and
UI checks were not repeated. The test suite was not rerun after its socket and
temporary-directory failures; no unrelated implementation fixes were attempted.
The engine leaf, runtime registry, cloud protocol and store package tests passed.
