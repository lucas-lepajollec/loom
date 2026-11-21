# Memory, transcripts and MCP retrieval

Folders group sessions. Local and Cloud discussions use their own instructions
and history. A thread keeps a frozen snapshot of its instructions until a
configuration change, compaction or explicit refresh.

The standalone Brain page and its REST endpoints for source editing, search,
packing, reading, distilling and editing memory have been retired. Existing
source definitions and reviewed distilled items remain readable. Loom keeps
the retrieval index for MCP clients and continues writing discussion transcripts.

## Retrieval tools

`/mcp/brain` serves Streamable HTTP. The tools include `brain_search`,
`brain_read`, `brain_pack`, `brain_write`, `brain_edit`, memory and skill
operations. Writes require a write-authorized source. Personal retrieval
requires explicit selection and opt-in. External clients supply their own
Bearer credential. See the [offline retrieval benchmark](brain-retrieval-benchmark.md).

`GET /api/brain/semantic` reports the embedding configuration and index state.
The authenticated POST endpoint controls embedding consent, selection, indexing
and disabling. This also controls automatic refresh for saved MCP sources.

## Loom MCP gateway

`/mcp/loom` exposes Brain tools and enabled Loom MCP servers to manually
configured clients. Control authentication is required for setup. A dedicated
gateway token authorizes MCP only, not dashboard or inference APIs.

- `GET /api/mcp/gateway`: `{url, token_set}`; never reveals the token.
- `POST /api/mcp/gateway/token {"rotate":true}`: creates a random token and
  returns it once with `Cache-Control: no-store`; only its hash is persisted.
- Existing external configurations are not modified during rotation. Update
  clients manually. Old ownership metadata is retained inertly for migration.

Keep tokens private and use a reachable protected endpoint. Loom must remain
running. Portable definitions in the primary brain remain secret-free and are
imported disabled. See [MCP files](mcp-files.md).

## Markdown memory in the primary brain

The second brain owns durable memory; Loom connects it. Agents and people read
and edit the same native Markdown files with or without Loom. Memory follows
Claude Code auto memory: a short `MEMORY.md` index and one topic file per memory.
No phrase detection, per-turn handoffs, continuity summaries or mirrored core
notes participate in memory.

The default layout is:

```text
Memory/MEMORY.md
Memory/profile.md
Projects/<stable-project-slug>/memory/MEMORY.md
Projects/<stable-project-slug>/memory/<topic>.md
Discussions/<project-slug-or-_>/<YYYY-MM-DD>-<title-slug>-<short-id>.md
.loom/brain.json
.loom/migration-memory.md
```

Dates are UTC and short discussion IDs are deterministic hashes. A project slug
comes from its name, handles collisions and remains stable after renaming.
Discussion filenames remain stable after creation; moving a discussion changes
its folder and metadata. Loom writes only in these trees and `.loom/` metadata.
Existing notes keep their structure. Relative paths, safe filenames, root-confined
IO and refusal of symlinks protect the boundary. Writes use atomic replacement.

`.loom/brain.json` preserves unrelated keys and supports:

```json
{
  "memory_folder": "Memory",
  "projects_folder": "Projects",
  "project_memory_folder": "memory",
  "discussions_folder": "Discussions",
  "project_folders": {"project-id": "stable-project-slug"},
  "discussion_files": {"discussion-id": "Discussions/_/2026-10-08-title-short-id.md"}
}
```

Configured folder names are relative, cannot overlap and cannot contain hidden,
parent or symlink paths. Loom owns the identity maps. Without an available
writable primary, an equivalent tree lives in `LOOM_HOME/brain/loom-memory/`,
using the existing encryption codec when enabled. Primary files remain plaintext;
Loom's vault checks still block HTTP/MCP/context access while locked.

A memory file is:

```markdown
---
name: Release checks
description: Before preparing a release
type: feedback
metadata:
  discussion_id: discussion-id
  date: 2026-10-08T12:00:00Z
---
Run the agreed release checks before publishing.

**Why:** A previous release omitted validation.

**How to apply:** Follow the current checklist and report its results.
```

`type` is `user`, `feedback`, `project` or `reference`. Names are at most 300
bytes, descriptions 1000 bytes, bodies 8 KiB of valid UTF-8. Feedback/project
memories state the rule/fact, then `**Why:**` and `**How to apply:**`;
consolidation validates those headings. Unknown frontmatter/metadata keys survive
updates. External edits are reread on every access. Malformed files remain listed
with `malformed:true`, and Loom never overwrites or deletes them. Repair them
with ordinary file editing.

Index entries are `- [Title](file.md) — retrieval hook`. Creates, updates and
deletes rebuild the index. Matching normalized names or exact normalized text
reuse an existing file. `MEMORY.md` is limited to its first 200 lines / 25 KiB.
Oversized indexes or collections expose a `warning`; omitted topic files remain
on disk and accessible by filename. Condense or merge memory when the index grows.

The MCP tools `memory_index`, `memory_read`, `memory_write` and `memory_delete`
operate on this store. The HTTP consolidation controls remain available to the
owner. Use `GET /api/brain/memory/status` for status,
`GET/POST /api/brain/memory/settings` for model selection and
`POST /api/brain/memory/consolidate` with a discussion ID for an explicit run.

## Discussion transcripts

At every workspace and native chat turn end, Loom asynchronously writes the
complete visible transcript as Markdown: title, project, executor, model,
creation/update dates, verbatim user/assistant text and one summary line per tool
call. It excludes hidden reasoning, system prompts, launch credentials,
approvals and raw tool results. Existing visible text is preserved as it stands;
this is not a secret scanner. Interrupted turns retain partial text. Ordered
writes prevent older queued snapshots replacing newer ones.

The conversation source indexes `Discussions/`, including agent-written files.
Missing historical files are backfilled from stored journals. Project scope
follows current membership and canonical paths. Memory/transcript folders are
excluded from ordinary source-file indexing, avoiding duplicate retrieval and
persistence of their text in the disposable BM25 cache. Their built-in sources
are indexed in memory through the availability/codec boundary.

## Background memory consolidation

Discussions become eligible after five minutes since their last turn, or when
the user leaves them. The running service scans once a minute; navigation marks
the prior discussion paused. On-demand consolidation bypasses the idle delay.
One run is allowed at a time. Generation/context preparation prevents it; a new
accepted turn cancels an in-flight background call. Changed discussion or memory
snapshots cause stale results to be discarded.

Each run makes **one model call**, with no retry or trigger phrases. Input
contains the next unprocessed transcript segment (at most 24 KiB), both bounded
indexes and up to 32 KiB of files they reference. Saved byte offsets/prefix hashes
survive restart; transcript rewrites reset the checkpoint. Large transcripts are
consumed in successive runs without skipping new text. Supplied text is untrusted.

Save information useful later that cannot be derived from code/Git. User facts
and preferences are `user`; corrections/confirmed approaches are `feedback`;
ongoing work/decisions are `project`; pointers are `reference`. Never retain
secrets or private runtime state. Keep files short and prefer updating existing
files to creating near-duplicates.

The model response is a strict JSON array, at most 32 operations:

```json
[
  {"op":"create","scope":"global","name":"Writing style","description":"Answer length","type":"user","text":"Prefers concise replies."},
  {"op":"update","scope":"project","file":"release.md","name":"Release checks","description":"Before release","type":"feedback","text":"Run checks.\n\n**Why:** Avoid omissions.\n\n**How to apply:** Follow the checklist."},
  {"op":"delete","scope":"global","file":"obsolete.md"}
]
```

Empty `[]` is valid. Create may specify a filename; update/delete require an
existing safe filename. Project operations require the discussion's project.
Unknown fields, duplicate keys, nulls, invalid types/scopes, missing fields,
traversal and oversized text fail before applying the batch. Multiple operations
on one filename are refused. IO failures may leave already applied operations;
status records those, and the checkpoint advances only on success. Create/update
record the discussion ID and UTC date in frontmatter `metadata`.

Existing Loom configuration supports `brain.consolidation_model`:

| Value | Behavior |
| --- | --- |
| `discussion` (default) | Loom-held Local/Cloud discussion's own route; archived retired discussions may use an explicitly configured fallback |
| `{"provider_id":"saved-id","model":"exact-model"}` (JSON string value) | Connected provider/model pair |
| `local-loaded` | An already-loaded local model; never load or restart an engine |
| `off` | Disable all consolidation, including on demand |

`brain.consolidation_fallback` accepts a provider pair, `local-loaded` or `off`,
and defaults to no model. A discussion's accepted Cloud route retains destination
consent. A separate provider requires `brain.consolidation_consent`, a JSON string
`{provider_id,endpoint,model}` matching its saved endpoint and chosen model.
Linked local routes outside loopback require `brain.consolidation_local_consent`
equal to the exact engine base URL. Destination changes/disconnections invalidate
eligibility. Credentials stay in their existing layer, never in Brain files.
Status reads never invoke models. Consolidation can consume provider/model tokens.
