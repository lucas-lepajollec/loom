# Implementation brief (for any coding agent)

You are continuing Loom. Read, in order: `AGENTS.md`, `docs/architecture-principles.md`,
`docs/architecture.md`, `docs/workspace-architecture.md`. Then inspect
`git status` — earlier work may be uncommitted; never discard it.

## Hard rules

1. **Do not touch the visual design.** `internal/loom/ui/next/css/*`, tokens,
   colors, spacing, typography, animations and the layout of existing screens
   belong to the design owner. You may only *use* existing classes and
   components (`ui/controls.js`, `ui/dialog.js`, `.card`, `.btn`, `.tag`, `.row`,
   `.tr`, `.set-line`, `.prow`, `.gauge`, `.mono-tile`, `.state`, `.dot`…). The design is
   "Mono": no brand color; color only for state (green ready, amber warning, red error). If a new component truly needs CSS, add the
   smallest rule in the relevant CSS file using existing tokens only
   (`var(--…)`), no new colors, no gradients, no emojis, no new fonts, and list
   it in your final report.
2. **No big-bang refactor.** One step of the plan per change set. Zero behavior
   change unless the step says otherwise. Keep every commit building and green.
3. **llama.cpp owns execution** (router mode, slots, batching). Never add a
   request queue, never restart the engine to change a model when the router
   is available, never mutate saved configs from API requests.
4. **Honesty in the UI**: a capability is shown as supported only if implemented
   and tested. Unknown data is displayed as unknown, never zero.
5. **Security**: never log or persist API keys; keep model output HTML escaped
   (`features/chat/md.js`); keep consent before sending a discussion to an
   external provider or harness.
6. French UI copy, sentence case, short labels, explanations in `<Tip>` (ⓘ),
   not in paragraphs under each control.

## Commands

```bash
make build                                   # builds bin/loom with ui/next embedded
go test -short ./...                         # Go suite (must stay green)
go vet ./internal/loom/
make check-ui                                # module syntax check of ui/next + UI unit tests
# never use plain `node --check file.js`: it parses .js as CommonJS and misses errors
```

Dev instance (does not touch the installed Loom on 8091):

```bash
LOOM_HOME=$PWD/.project-local/runtime LOOM_SERVICE=loom-dev-engine \
LOOM_UI_SERVICE=loom-dev-ui ./bin/loom web 2594   # http://127.0.0.1:2594/
```

## Work queue (do in order, one per change set)

### 1. Runtime registry (backend, package `loom`)
- Add `registerRuntime(RuntimeAdapter)` and a registry map; build
  `runtimeCatalog()` from it. Keep planned-only descriptors (claude-code, pi,
  hermes) as registered descriptors with empty capabilities.
- Extend `RuntimeDescriptor` with `Description`, `CLI`, `Consent` strings.
- Add generic routes `POST /api/runtimes/{id}/connect` and
  `POST /api/runtimes/{id}/quota` dispatching through optional interfaces
  (`Connectable`, `QuotaReader`); keep old routes as aliases.
- Tests: registry order, unknown id → 404, capability-less runtime cannot connect.
- Front: `features/harnesses/page.js` reads description/CLI/consent from
  `workspace.runtimes` and calls the generic route; delete the `INFO` map.

### 2. Engine interface + ParamSpec
- Define `Engine` (architecture §2.1) and wrap the current llama.cpp functions
  (`restartLlamaEngine`, `unloadEngine`, `buildLlamaServerArgs`, estimate) in a
  `llamaCppEngine` type. No file moves yet.
- Create `internal/loom/engine/llamacpp/params/llamacpp.json` with the curated Essential/Advanced
  entries now hard-coded in `features/inspector/params.js` (`META`, `KV_OPTS`,
  `SPEC_OPTS`, the advanced list). Serve merged with the `--help` catalog at
  `GET /api/engine/params`.
- Front: `params.js` renders rows from that endpoint; the component look stays
  identical (same `Row`, `Slider`, `Switch`, `Select`).
- Acceptance: screenshots of the panel before/after are identical; adding a
  flag to Advanced requires only a JSON edit.

### 3. Auto configuration with `--fit`
- When a bare model has no remembered config, do not force native `CTX` and
  `NGL=999`; pass `--fit on` and leave them unset. After load, read the actual
  context from the router (`/models` or `/props?model=`) and expose it in
  `/api/status` (`ctx_effective`).
- Keep explicit user values untouched. Tests with the fake router in
  `backend_router_test.go`.

### 4. Unified discussion events
- Emit the event vocabulary of architecture §2.3 for cloud/harness turns over
  SSE (same shape as the native journal), then make the native journal map to
  it. Remove polling from `features/chat/engine.js` once both paths stream.
- Acceptance: one renderer, local → cloud → local in one discussion still works,
  existing tests green, new Go tests for the event mapping.

### 5. Port the remaining classic features, then delete `ui/src` — DONE
Global search (Ctrl+K palette over discussions/projects/models), activity
center (downloads + engine jobs), memory pages editor, preset ordering,
encryption unlock and backups, push notifications, GPU device selection, web
key. Reuse existing components; ask the design owner before any new visual
pattern. When all are ported: remove `/classic`, `ui/src`, `tools/assemble-ui`
CSS/JS concatenation and old UI tests.

### 6. Go package split (architecture §4), leaf-first
`store/` → `platform/` → `engine/llamacpp/` → `runtime/…` → `discussion/` →
`tools/` + `resources/` → `web/`. One package per change set.

## Final report for every change set
What changed (files), what was verified (commands + results), what was not
verified, any CSS you had to add, and the exact next step.
