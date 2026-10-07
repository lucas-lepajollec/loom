# Loom: portable work, explicit execution

Loom is a local-first control plane. It owns discussions, projects and selected
context; engines and harnesses execute. A discussion can continue through Local,
Cloud or a Harness without changing its identity. A harness is an executor,
not a model: it may use its native subscription or a Loom model when its native
protocol supports that choice.

## Cognitive continuity

Changing executor should not require reconstructing why the project exists or
copying a large handoff. A Loom project selects a machine, workspace and default
executor/model. Its Loom discussions form its conversation-memory scope
automatically. Connected second brains and accepted Loom memory add bounded,
cited context for the current request, while portable user/assistant history
stays with the discussion.

One connected source may be the writable primary second brain. Agents may keep
durable decisions, preferences and project facts current there through bounded
Markdown tools; other sources retain explicit read/write policies. A new
executor gets the same prepared context. Its private native memory, approvals,
tools, credentials and hidden reasoning remain with the original runtime.

Explicit distillation creates candidates; pending/rejected candidates do not
enter retrieval. Existing accepted memory remains readable.
Sources remain the authority; BM25, vectors and a possible future graph are
derived indexes. Skills and MCP servers are execution capabilities, not facts.

## Sources and machines

Brain can link multiple independent knowledge folders with an explicit purpose,
source selection and personal-data policy. It assumes no particular note
application, repository name or owner-specific filesystem. Files are refreshed
incrementally. Semantic refresh is separately opt-in; cloud embedding consent
names the destination and may incur provider charges.

Project retrieval of the built-in conversation source is limited to discussions
currently assigned to that project. HTTP/MCP callers can supply `project_id` to use that scope.
General Brain calls without project scope retain the operator's explicit source
selections; project scope is not a multi-user authorization boundary.

A main Loom may control engines on a headless engine node and harnesses over
SSH. It need not duplicate Brain, discussions or the full UI on the GPU host.
Native harness histories can be listed/imported where the adapter advertises
ACP session listing/loading. Import records runtime, machine and original
session provenance and deduplicates repeats, even after changing executor.
The UI defaults to a fresh native session for subsequent work with the imported
portable transcript and selected project. Original native sessions are retained.

## Stability before expansion

Routine observations must be bounded, serialized and paused when hidden. Slow
remote observations and prompt preparation must not hold a global registry lock
and freeze unrelated interface reads. Menus must complete touch clicks and stay
within the visible viewport. Preserve Loom's design and full local parameters.

Loom delegates inference, batching, slots, KV cache and sampling to its engines.
Unsupported provider/harness capabilities remain explicitly unavailable.
Authentication separates browser sessions, inference API credentials and machine
credentials. Public code, examples and tests contain synthetic data, not private
corpora, credentials or machine-specific rules.

The accepted long-term direction (Machines/Nodes, capabilities, Brain v2,
interface architecture, tasks, orchestration and voice) is in
[DIRECTION.md](DIRECTION.md).

See the [current roadmap](ROADMAP.md), [workspace contracts](workspace-architecture.md)
and [Brain API and limits](brain.md). Graph indexing and automatic routing come
after stability and real-device/provider acceptance. Consumer ChatGPT/Gemini web
conversation import is not implemented as a native harness-history API.
