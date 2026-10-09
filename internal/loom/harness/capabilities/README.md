# Agent capability snapshots

These are normalized, credential-free protocol observations made by
`tools/agents-watch`, not declarations of what Loom implements or proof of live
turn compatibility.

Baselines come only from the weekly workflow's clean GitHub runner: a snapshot
taken on a personal machine would record that machine's extensions, skills and
settings. A missing snapshot is generated there as **baseline created** and
included in the review PR (or the review issue when Actions cannot open PRs);
failed probes never publish an accepted baseline.

Refresh installed agents with:

```sh
make agents-watch AGENTS_WATCH_ARGS=--update-capabilities
```

To inspect the latest published npm packages instead, use
`AGENTS_WATCH_ARGS='--install --update-capabilities'`. Review candidate JSON,
path diffs, release notes and failures before accepting a version. Ordinary local
runs write candidate artifacts only. `--publish` puts successful observations in
the isolated version-review worktree and also opens an attention issue when an
existing snapshot changes.

See [the watch guide](../../../../docs/agents-compat.md#weekly-compatibility-watch-and-accepting-versions)
for captured endpoints, normalization policy and detection limits.
