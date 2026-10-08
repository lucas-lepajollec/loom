# Antigravity CLI stream fixtures

Synthetic NDJSON exchanges for installed `agy` 1.3.1, derived from Google's
[headless contract](https://antigravity.google/docs/cli/headless/) and installed
help/binary type inspection. These are not live captures or paid generations.

Each row contains the provider `frame`, ordered canonical `events`, and an
optional fake-process `exit_code`. The result row's expectations also include
settlement after process exit. The fake process validates the single input
`user` frame and EOF before replay. No native executable or account is accessed.

`normal` tests per-step usage snapshot deduplication versus cumulative result
usage. `tool-file` retains reported targets without reading disk or inventing
diffs. `permission-question` is a synthetic negative case: native headless tools
cannot obtain interactive input; no request card or reply is fabricated.
`unknown` intentionally uses unaccepted future reasoning/plan/context names to
verify raw preservation, not to advertise support for those fields.

Other cases cover final-only/corrected responses, verbatim error/busy messages,
interrupts, missing results and nonzero exit after a success result. Additional
Go tests cover cancellation, malformed JSON, diagnostic redaction, missing or
mismatched conversation IDs, nullable counters and launch policy.

Run:

```sh
go test ./internal/loom/runtime/antigravity -run TestAntigravityFixtureCorpus -v
```

See [agent compatibility](../../../../../docs/agents-compat.md) for the accepted
mapping, native ACP comparison, session continuity and live-check limitations.
