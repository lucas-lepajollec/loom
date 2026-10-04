# Loom: portable work, explicit execution

Loom is a local-first control plane. It owns discussions, projects and selected
context; engines and harnesses execute. A discussion can continue through Local,
Cloud or a Harness without changing its identity. A harness is an executor,
not a model: it may use its native subscription or a Loom model when its native
protocol supports that choice.

## Cognitive continuity

Changing executor should not require reconstructing why the project exists or
copying a large handoff. The next executor receives a small, reviewed baseline:

1. An explicitly chosen shared preference page, with separate consent for
   cloud/harness sharing.
2. Project purpose, rationale, constraints and accepted decisions.
3. The dated working state maintained by the user.
4. Bounded capsules of explicitly linked reference discussions.
5. Discussion instructions and portable user/assistant history.

Selected project files and relevant Brain passages add deeper context under
their existing bounds. The inspector shows the baseline, reference IDs,
citations, approximate text-token cost and exact prepared text. A new executor
gets this same project baseline; its private native memory, approvals, tools,
credentials and hidden reasoning remain with the original runtime.

Working state and capsules are edited and reviewed, not silently invented by a
background model. Explicit distillation creates candidates; pending/rejected
candidates do not enter retrieval. Existing accepted memory remains readable.
Sources remain the authority; BM25, vectors and a possible future graph are
derived indexes. Skills and MCP servers are execution capabilities, not facts.

## Sources and machines

Brain can link multiple independent knowledge folders with an explicit purpose,
source selection and personal-data policy. It assumes no particular note
application, repository name or owner-specific filesystem. Files are refreshed
incrementally. Semantic refresh is separately opt-in; cloud embedding consent
names the destination and may incur provider charges.

Project retrieval of the built-in conversation source is limited to explicitly
linked discussions. HTTP/MCP callers can supply `project_id` to use that scope.
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

See the [current roadmap](ROADMAP.md), [workspace contracts](workspace-architecture.md)
and [Brain API and limits](brain.md). Graph indexing and automatic routing come
after stability and real-device/provider acceptance. Consumer ChatGPT/Gemini web
conversation import is not implemented as a native harness-history API.
