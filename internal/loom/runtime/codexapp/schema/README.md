# Codex app-server message snapshot

Generated with `codex-cli 0.159.2` using
`codex app-server generate-json-schema --out /tmp/loom-codex-schema`.
`messages.json` merges the message schemas consumed by Loom and their embedded
definitions without changing the original properties/unions. `VERSION` records
the native executable version. The standard-library Python generator at
`tools/generate-codex-types.py` reproduces the Go projections from this snapshot.
See [refresh instructions](../../../../../docs/agents-compat.md).
