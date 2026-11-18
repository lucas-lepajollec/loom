# Loom vision

A local-first AI workspace for conversations, local inference, cloud models and
shared knowledge. One discussion keeps its history and context when its model
changes. Loom manages the engines, model libraries and user experience; inference
stays in the engine.

Brain connects one or more second brains with explicit primary/write rules.
Sources stay authoritative and memory supports cognitive continuity without
making users configure an implementation graph. Projects stay simple: title,
workspace, machine, default model and their own discussions.

Loom does not run coding agents itself. Its Agents module is an interface for
an independent Codeman server, which runs the native CLIs with their own
accounts; Loom adds the workspace around them (sessions, terminals, approvals,
schedules, quotas). Existing agent transcripts from the retired integration
remain readable. See [Agents architecture](agents/architecture.md),
[migration](harness-retirement.md) and [direction](DIRECTION.md).
