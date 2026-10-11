# Architecture principles

Loom controls; engines execute inference. Local and Cloud share a discussion,
portable transcript and project context. Engines and loaded models are separate
states. Unsupported capabilities remain explicitly unavailable.

The runtime registry contains local inference and Chat Completions adapters.
Coding harnesses are retired; historical event/state types exist only for
reading existing discussions. Native tools, permissions and credentials are
not migrated or executed. See [migration](harness-retirement.md).

Sources are truth. Brain BM25/semantic indexes and context selection are derived;
Markdown memory and transcript files remain inspectable. Personal sources
require selection and write permission. Context sent externally requires consent.

Reuse packages and upstream protocols. Keep inference scheduling, slots, KV
cache, sampling and GPU offload in llama-server/vLLM. The remote node manages
engine lifecycle, models, terminals and observations without owning discussions,
Brain or cloud providers. Native coding applications remain independent.

Control credentials, inference keys and MCP gateway tokens have distinct scopes.
Filesystem and command tools retain the existing policy checks. No telemetry,
unmanaged process killing or machine-specific public configuration.
