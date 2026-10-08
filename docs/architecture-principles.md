# Architecture principles

These rules apply to every change in Loom, whoever writes it. A feature request
that seems to conflict with them should be discussed before it is implemented.

## Loom controls, engines execute

1. **Engines own inference.** For llama.cpp, model loading, slots, batching, KV cache,
   sampling and GPU offload stay in `llama-server`. Loom resolves the user's
   intent into native arguments and observes the result; it never adds its own
   slot scheduler or token path. The accepted engine-service policy (§3b of
   `DIRECTION.md`) admits model swaps only after in-flight responses drain;
   that residency queue does not schedule inference within a loaded model.
2. **Engine and model are separate states.** With a router-capable engine, Loom
   starts `llama-server` once and loads models through its API
   (`backend_router.go`). A model change never restarts the engine.
3. **One resolved configuration.** Engine defaults, model metadata, saved model
   settings, preset and temporary overrides resolve into a single argument list
   (`buildLlamaServerArgs`). Nothing else builds launch arguments.
4. **External requests are ephemeral.** Load parameters sent by an API client
   produce a temporary variant. They never change saved settings or presets.
5. **Show every decision that changes runtime behavior.** Slots, context, KV
   type, offload or a fallback to the historical single-process mode must be
   visible, not silent.
6. **Adding an upstream flag should be small.** Expert exposes the installed
   engine's `--help` directly; curated Essential/Advanced controls are the only
   place that needs hand-written UI.

## One discussion, several executors

7. A discussion belongs to Loom. Local models, cloud models and harnesses are
   execution choices inside it. A harness is an executor that may itself use a
   local or cloud model, not a third kind of model.
8. Harness adapters declare what they really support. An unsupported capability
   is shown as unsupported, never simulated.
9. Loom owns shared definitions (projects, skills, MCP servers, providers);
   each harness keeps its runtime state, private memory, approvals and sessions.
10. Missing data is unknown, not zero: quotas, costs and metrics are only shown
    when a source reports them.

See `workspace-architecture.md` for the discussion/runtime contracts.
