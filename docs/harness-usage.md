# Native harness usage

Loom keeps three different observations separate: subscription quota windows,
retained Loom turns (`GET /api/usage`), and native harness usage including sessions
started outside Loom. Native usage is an observation, not a billing ledger.

## Account quotas

Existing quota refresh routes dispatch through the registry and `QuotaReader`:
`POST /api/runtimes/{id}/quota` with `{}`, or the legacy
`POST /api/usage/refresh` with `{runtime_id}`. The existing 30-second throttle,
vault checks, timestamps and stale-cache behavior apply.

- Codex and Antigravity readers keep their existing native commands.
- Claude Code runs `claude -p /usage --output-format json`. Every recognized
  window in `result` reports its name, remaining fraction and reset date.
  Reset dates use their IANA zone and the current or next year. Unknown lines,
  zones or formats leave a raw `note`; recognized windows still survive.
- Hermes runs `hermes usage`, locally or on a saved SSH machine. Provider names,
  remaining percentages and explicit UTC reset dates come from native output.
- Pi, OpenCode and Gemini do not advertise quota support.

`windows[].remaining` is a nullable fraction between 0 and 1;
`remaining_percent` is retained for existing clients. `reset_at` and `fetched_at`
are Unix seconds. Missing dates and fractions remain null. Local Hermes exposes
quota observations without enabling its unimplemented chat adapter.

## Native usage API

`GET /api/usage/native?days=7` returns `{ok:true,harnesses:[...]}` in registry
order. Omitted `days` means 7; 30 is also supported. Every registered harness has
its own result, even when unavailable. Readers run concurrently and their
failures do not fail the entire response.

`POST /api/usage/native/refresh` accepts `{"runtime_id":"hermes","days":30}`
and returns `{ok:true,harness:{...}}`, bypassing that entry's cache. Unknown
harnesses return 404; unsupported windows and invalid JSON return 400.

Both routes use the existing control-key authentication, vault-lock checks and
`Cache-Control: no-store`. They never send a discussion or start generation.
The native cache is in memory for five minutes, indexed by runtime and window;
concurrent reads for the same entry share one operation.

Each result contains `runtime_id`, `days`, `sessions`, `input_tokens`,
`output_tokens`, `cache_read_tokens`, `total_tokens`, nullable `cost_usd`,
`by_model` (`model`, `tokens`, optional native session count), human-readable
`source`, Unix-second `fetched_at`, and `error` (empty on success). An unavailable
or partial result's numeric fields must not be treated as a complete account
measurement. `cost_usd` is null if any contributing record lacks a native cost;
manual prices are never applied to native activity.

| Harness | Source | Window and accounting |
| --- | --- | --- |
| Claude Code | `~/.claude/projects/**/*.jsonl` | Assistant usage within the window; streaming fragments of one native message count once. Total includes native input, output, cache reads and cache creation. No native cost is inferred. |
| Codex | `~/.codex/sessions/**/*.jsonl` | Last cumulative token count per session, subtracting the last count before the window when present. Counter differences follow the model in `turn_context`; cached input is already included in input. No native cost is inferred. |
| Pi | `~/.pi/agent/sessions/**/*.jsonl` | Assistant `usage.input/output/cacheRead/cacheWrite/totalTokens` and `usage.cost.total`, filtered by entry timestamp. |
| OpenCode | `opencode stats --days N --models` | CLI totals and model statistics; padded tables and native model blocks are supported. Compact CLI numbers can be rounded. Message counts are not session counts. |
| Hermes | `hermes insights --days N` | Native totals, estimated USD cost and Models Used table, locally or via saved-machine SSH. |
| Gemini / Antigravity | No reliable integrated native source | `error:"non disponible"`; costs remain null. |

Local journal discovery skips files whose mtime predates the requested window,
reads at most 2,000 recent regular files, and does not follow directory symlinks.
Each harness has a 30-second context budget. Individual journals are bounded to
128 MiB, JSONL lines to 8 MiB, Claude deduplication to 100,000 message IDs per
file, and CLI output to 2 MiB; truncated/inaccessible data produces a partial
error. JSONL projections retain only timestamps, numeric usage, model names and
minimal event/message identifiers needed for accounting; message contents,
prompts, tool calls and private paths are never retained or returned.

Remote Hermes uses the saved machine identity, `loomSSHKey`, `sshArgs` and the
existing PATH preamble. Discovered tool directories are quoted; stored arbitrary
ACP launch commands are never reused for usage reads. Native Windows commands
reuse the existing launcher and PowerShell SSH helpers. Arbitrary custom
harnesses and remote JSONL sources remain explicitly unavailable.

Parsers have synthetic fixtures in `internal/loom/testdata/harness_usage`.
OpenCode's native model-block format was checked against its
[upstream stats command](https://github.com/anomalyco/opencode/blob/dev/packages/opencode/src/cli/cmd/stats.ts).
