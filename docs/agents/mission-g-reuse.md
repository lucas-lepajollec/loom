# Native answer and metadata extraction record — 2026-10-10

Checks and outcomes below belong to the original implementation run. See [Mission J verification](mission-j-cleanup.md) for the consolidation run.

The [architecture](../architecture.md#native-answer-assembly) owns answer accumulation and all 43 fixture/baseline rules. The [ACP guide](acp-implementation.md#harness-consumer-matrix) owns catalog projection and the family matrix. This dated record is not a current acceptance report.

## Reuse decision

The existing `runtime` canonical events and protocol mappers are the reuse
boundary. Reviewed the committed Codex schema, Pi fixture corpus, OpenCode
OpenAPI schema and Antigravity 1.3.1 structured-stream fixtures, plus upstream
[Codex app-server documentation](https://learn.chatgpt.com/docs/app-server),
[Pi sources](https://github.com/earendil-works/pi/tree/main/packages/coding-agent),
[OpenCode server documentation](https://opencode.ai/docs/server/) and
[coder/acp-go-sdk](https://github.com/coder/acp-go-sdk).
These describe different wire protocols; the ACP SDK does not replace native
answer assembly or Loom's launcher/inspect metadata. Reuse the current mappers
and share only pure answer and item-close logic, rather than adopting a new
transport/SDK layer. The audit's ACP TCK/SDK trial remains rank 7, out of scope.


## Validation

All Go commands used `GOTOOLCHAIN=go1.26.9`, temporary HOME/XDG/Loom data,
explicit existing module cache and a temporary build cache. No real `~/.pi`,
`~/.hermes`, `~/.dsh`, `~/IA` or Loom Node data was used. No models/accounts
were called. Binary and validation logs are not deliverables.

| Check | Result |
| --- | --- |
| Pre-extraction capture and post-extraction comparison | All 43 canonical native fixtures and launch/inspect baseline passed |
| Replacement scopes, item closing, complete family/transport matrix | Passed |
| Focused root native/ACP/remote/Node/feature/compatibility/custom regressions | Passed |
| `gofmt`, `go vet ./...` | Passed |
| `go test ./...` | Attempted; root `TestHandleHubSearch` (`backend_hub_test.go:111`) and platform `TestFetchReleaseChannels` (`platform/sys_update_channel_test.go:11`) panic: `listen tcp6 [::1]:0: socket: operation not permitted` |
| Other packages in full run | Passed, including runtime and all native/ACP children, harness, engines, Brain, store, tools and agents-watch |
| Documentation link checker and `git diff --check` | Passed; 199 local documentation targets |
| `make build` | Passed |
| `GOOS=windows go build -o <temporary-output> ./cmd/loom` | Passed; compile only |
| `make check-ui` | Passed; 24 Node test-file suites |
| `npm ci --prefix tools/tests/ui-routes --no-audit --no-fund` | Attempted (bounded fetch); DNS `EAI_AGAIN` for Playwright npm download |
| `node tools/tests/ui-routes/crawl.mjs bin/loom` after build | Attempted; `ERR_MODULE_NOT_FOUND` for Playwright; no routes exercised |

Unverified: full root/platform suite after socket denial, desktop/390px/320px
browser routes, installed/native account execution, real SSH/Node machines,
Windows runtime and ACP TCK conformance. Existing UI/layout and transport
selection are unchanged. `COMMIT_MSG.txt` is the requested review handoff;
no commit was created.
