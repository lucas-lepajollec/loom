# Mobile layout implementation record — 2026-10-10

Checks and outcomes below belong to the original implementation run. See [Mission J verification](agents/mission-j-cleanup.md) for the consolidation run.

Durable phone layout rules live in [workspace contracts](workspace-architecture.md#narrow-screens), module ownership in [architecture](architecture.md#agent-ui-modules), and regression/crawl commands in [UI validation](agents/ui-validation.md).

## Root cause and changes

The agent header shared one flex row between its logo, shrinking text column
and actions. Its fixed-height state pill allowed multiple lines of text to paint
outside its box. On phones, installation grids reduced the Manage/Use columns
to 56–64px and hid version data. Machine detail used the same pattern. Desktop
Environment tables required horizontal scrolling; local model tables hid metrics
when their header disappeared.

The existing `mobile.css` overrides now supply shared wrapping buttons, pills,
segments, page tabs, long values, key/value rows, dialogs and popovers. An object
header grid places identity/refresh in its first row, state in the second,
description and metadata across the width, and actions below. Both installation
views reuse `scope-control` labels and stack their rows. Model and Environment
tables keep labeled metrics on phones. No colors, tokens, decorative surfaces,
execution capabilities, data schemas or runtime policies were added.

Reuse: existing vendored Preact/htm, pinned Playwright 1.64.0, Modal/Tip/Switch/Seg
and Usage/Bench's `data-label` pattern. No new dependency, protocol implementation
or runtime layer was needed for a presentation change.


## Validation outcome and remaining acceptance

- Passed: `make check-ui`, Go 1.26.9 `make build`, `go vet ./...`, Windows
  `GOOS=windows go build ./cmd/loom` (output directed to `/tmp`), fixture shape and
  mutation guards, static imports and extraction behavior tests.
- No Go files changed; gofmt has no changed-file target.
- Attempted full `go test ./...` with a temporary HOME: `TestHandleHubSearch`
  (`internal/loom`) and `TestFetchReleaseChannels` (`internal/loom/platform`)
  panic because httptest cannot bind a loopback socket. The affected package
  suites cannot complete; this is not reported as a green Go test run.
- Attempted the required binary route crawl after rebuilding: it cannot bind
  `127.0.0.1` (`EPERM`). Direct Chromium launch is also denied. Therefore no
  rendered route results, screenshot pair or real-before failure proof exists.
- Pending: run baseline and full after crawl, inspect screenshots on desktop and
  both widths, resolve any measured layout failures, and physical iPhone
  acceptance. No native CLI/account/GPU behavior is claimed as observed here.

No commit or external publication was performed. `COMMIT_MSG.txt` records the
proposed summary, inventory and checks, including these acceptance limitations.

Original ignored evidence locators were `.project-local/mission-f/`:
`before-unit.log`, `before-header-unit.log`, `before-machine-unit.log`,
`before-table-unit.log`, original sources and attempted build/crawl logs.
The requested `before-agent-390.png` / `after-agent-390.png` screenshots were
not generated in that original environment.
