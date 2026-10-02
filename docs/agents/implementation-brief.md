# Implementation guide for coding agents

Read [AGENTS.md](../../AGENTS.md), the
[architecture principles](../architecture-principles.md),
[architecture](../architecture.md), [workspace contracts](../workspace-architecture.md)
and [roadmap](../ROADMAP.md). Inspect the branch, working tree, code and
configuration before editing; preserve unrelated work.

## Boundaries

1. Reuse the existing UI components, CSS and tokens. The visual design belongs
   to the design owner. Keep the common discussion/composer and complete local
   parameter editor; do not introduce a second chat or redundant header.
2. Keep changes focused. The leaf package split is delivered; application
   state, lifecycle policy and HTTP domain handlers deliberately remain in Loom.
   Do not recreate completed migrations described in architecture §4.
3. Engines own inference. Keep llama.cpp router mode, native slots and one
   argument builder; do not add scheduling or restart a router for model changes.
   Temporary API overrides never alter saved model settings.
4. Descriptors must match implemented capabilities. Native runtime state,
   approvals and hidden reasoning remain private. Unknown metrics stay unknown.
5. Keep credentials out of logs and public records. Provider keys may be
   explicitly remembered in the OS keychain. Preserve vault checks, bounded
   input, model-output escaping and consent before external context handoff.
6. Add UI copy in both English and French dictionaries. English is the default;
   use short sentence-case labels and existing Tip components for explanations.
7. Keep one Brain/context engine. Project retrieval is opt-in, personal sources
   require explicit selection, and cloud embeddings require stored consent.
8. Terminals and harness processes must retain their ownership, authentication,
   native permissions and shutdown rules. Never kill unrelated processes.

## Validation

```sh
make build
go test -short ./...
go vet ./...
make check-ui
python3 tools/check-doc-links.py
```

`make build` directly embeds `ui/next`; Node is needed for UI checks, not an
asset build. Use the Makefile’s module-aware syntax check. Go HTTP tests need
loopback sockets. Native accounts, GPUs, keychains and ConPTY require separate
real-platform checks.

Use an isolated development data directory and port:

```sh
LOOM_HOME="$PWD/.project-local/runtime" LOOM_SERVICE=loom-dev-engine \
LOOM_UI_SERVICE=loom-dev-ui ./bin/loom web 2594
```

Keep local notes in ignored `AGENTS.override.md` and `.project-local/`. Never
publish private paths, infrastructure, logs, keys, databases or model files.

## Current integration references

- [ACP execution and development agent](acp-implementation.md).
- [ACP design reference](harness-acp-brief.md).
- [Engines and vLLM](../engines.md).
- [Brain sources, semantic indexing and distillation](../brain.md).
- [MCP files](../mcp-files.md), [terminals](../terminals.md) and
  [native usage](../harness-usage.md).

Report the concrete change, checks and results, unverified behavior and any
remaining decision. Update public workflow documentation and the changelog when
behavior, API endpoints or CLI flags change.
