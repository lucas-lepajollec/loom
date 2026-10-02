# Brain V1 / Context Service

Brain indexes local text and returns cited context without calling a model,
embedding service or external API. The package `internal/loom/brain` receives
its storage, document providers and availability check explicitly. Thin
`brain_*.go` adapters connect it to Loom's storage and web server. It does not
change discussion prompts or automatically send context to an executor.

Run `loom web 8091` (or the desktop app). The process must remain running;
the browser can be closed. Indexing runs at startup and every three minutes.
`POST /api/brain/reindex` also refreshes synchronously. Changes to source
definitions take effect immediately; new file content appears on the next
refresh. No harness settings are changed automatically.

## Sources and storage

Stored definitions have `id`, `label`, an absolute directory `path`,
`kind` (`context`, `personal`, `repo`) and optional `include` globs.
IDs are 1–64 ASCII letters/digits/underscore/hyphen, starting with a letter
or digit. Labels have a 200-byte limit. There are at most 100 stored sources.
Includes are relative to the root and support `*`, `?`, character classes,
and recursive `**`: e.g. `**/*.md`, `docs/**`, `notes/*.txt`.
They narrow the normal file selection; at most 64 globs, 256 bytes each.

Two built-ins are read-only:

- `conversations`: user and assistant text from Loom's native display journal,
  archives and common discussions. No system instructions, hidden reasoning,
  approvals, tool results or runtime metadata. Bound native discussions are
  not indexed twice. Paths identify the discussion and individual message.
- `memory`: the existing local agent's Markdown pages, read through Loom's
  decryption layer. Brain does not create another memory store or write pages.

Default requests include context, repositories, conversations and memory.
Personal sources are excluded even when `personal=true` alone is supplied:
the request must **also explicitly name each personal source ID**. This rule
applies to search, packs and reads, including a previously known chunk ID.
For reads, `source` or `sources` counts as explicitly naming the source.
A source filter restricts results to the named sources, including built-ins.
Private documents also stay out of default BM25 statistics.

Definitions live in `LOOM_HOME/brain/sources.json`, and a rebuildable index in
`LOOM_HOME/brain/index.json`, with private file modes where supported. Source
definitions contain settings/paths; the file index contains text. The index
uses Loom's existing encryption codec when vault encryption is active, on
each successful refresh. Enabling encryption therefore requires a Brain
refresh to replace any older plaintext file cache. All Brain text access is
blocked while that vault is locked. After unlocking, reindex to restore
built-in results immediately. Memory and discussion text is **never persisted
in Brain's cache**, including while the vault is unlocked.

The saved index retains file mtime/size and source-scope fingerprints. At
restart, unchanged file chunks are reused and the inverted index is rebuilt
in memory. Changed files are reread; removed, excluded and unreadable files
are dropped on refresh. A changed directory/kind/include list invalidates
that source's old cache. As with any mtime/size cache, an edit preserving both
attributes will not be detected. Reads return the indexed snapshot, not an
arbitrary file from disk. Invalid cache JSON is rebuilt; invalid definitions
or inaccessible encryption keys are errors, not silently erased settings.

## HTTP API

All routes use Loom's control-key authentication (`Authorization: Bearer …`),
distinct from the model-completion API key. The key is checked per request.
With no configured key, loopback access is open as for other Loom API routes.
Responses use `Cache-Control: no-store`. POST JSON uses
`Content-Type: application/json`, one JSON object and a 128 KiB body limit.

| Method / path | Input | Output |
| --- | --- | --- |
| `GET /api/brain/sources` | — | `{ok, sources:[{id,label,path,kind,include?,read_only,files,chunks,last_indexed,error?}]}` |
| `POST /api/brain/sources` | `{"action":"add","id":"project","label":"Project","path":"/path/to/project","kind":"repo","include":["**/*.md"]}` | Same source list; `add` creates/replaces the complete definition. `action` defaults to `add`. |
| `POST /api/brain/sources` | `{"action":"relabel","id":"project","label":"New label"}` or `{"action":"remove","id":"project"}` | Same source list. Built-ins cannot be changed. |
| `POST /api/brain/reindex` | No body | `{ok,sources,error?}` after refresh; partial source errors return `ok:false` with current counts. |
| `GET /api/brain/search` | `query`, optional `sources`, `personal=true`, `limit` | `{hits:[{source,path,heading,snippet,highlights,score,chunk_id}]}` |
| `POST /api/brain/pack` | `{query,budget_tokens?,sources?:[ids],personal?:bool}` | `{text,citations:[{chunk_id,source,citation}],tokens_used,chunks}` |
| `GET /api/brain/read` | `chunk_id`, or `source` + relative `path` + optional repeated `heading`; optional `sources`, `personal=true` | `{chunk_id,source,path,heading,text}` |

GET `sources` accepts comma-separated IDs and/or repeated parameters. GET
`heading` is the exact heading ancestry in order, e.g.
`heading=Architecture&heading=Storage`. A path/heading identifying several
chunks returns an error: use a chunk ID from search. The search limit defaults
to 10 and is capped at 100. Queries are at most 4096 bytes; an empty query
returns no results. Invalid requests/unknown filters return 400, wrong methods
405, wrong JSON content type 415, and a locked vault 423. Missing/wrong control
keys return 401; unreadable authentication configuration fails closed (503).

Paths are relative to their source. `heading` is an array of Markdown titles.
`highlights` contains `{start,end}` ranges in the **snippet**, measured as
Unicode code points, with an exclusive end. Clients must escape text and apply
ranges themselves; snippets never contain injected HTML. Scores are lexical
ranking values, not confidence estimates. Chunk IDs are deterministic hashes
of source/path/heading/position/text and can change after edits.

A pack starts with `Context from the user's Brain:\n`, followed by ordered
chunks and `[source: path › heading]` citations. Exact duplicate text is
included once, even across files/sources. Up to 100 ranked candidates are
considered, skipping whole chunks that do not fit. The budget defaults to
1500 tokens and accepts 1–8000; the estimate is `ceil(Unicode characters/4)`
for **all returned text**, including the prefix, separators and citations.
Nothing is truncated to fill a budget. No fitting/relevant chunks produces
empty text, empty arrays and zero tokens. This is an estimate rather than a
model tokenizer guarantee. Context is untrusted source text; the caller owns
its injection policy and consent to share it with an external executor.

## MCP for harnesses

The same engine is available at `http://127.0.0.1:8091/mcp/brain` using
Streamable HTTP and the Go MCP SDK. It exposes only `brain_search`,
`brain_pack`, `brain_read`, marked read-only. Arguments and structured results
match their HTTP equivalents; MCP `heading` and `sources` are JSON arrays.
Tool errors use MCP's error results. The control key, when set, must be passed
as a Bearer header on every HTTP request. This endpoint neither manages
sources nor triggers generation. It keeps DNS-rebinding and cross-origin
protection, bounds request bodies and does not retain MCP sessions.

Claude Code (substitute your reachable Loom URL; omit the header only if no
control key is set):

```sh
claude mcp add --transport http loom-brain http://127.0.0.1:8091/mcp/brain \
  --header 'Authorization: Bearer YOUR_LOOM_CONTROL_KEY'
```

The command writes a native Claude Code MCP entry for the current project.
Keep that entry private if it contains a literal key. See the official
[Claude Code MCP documentation](https://code.claude.com/docs/en/mcp).

Codex: add the following to its user `~/.codex/config.toml`, and supply
`LOOM_CONTROL_KEY` in the environment of the Codex process:

```toml
[mcp_servers.loom_brain]
url = "http://127.0.0.1:8091/mcp/brain"
bearer_token_env_var = "LOOM_CONTROL_KEY"
```

This uses Codex's documented HTTP transport and environment-backed Bearer
authentication; see [official OpenAI MCP documentation](https://learn.chatgpt.com/docs/extend/mcp?surface=cli).
Omit `bearer_token_env_var` when no control key is set. A harness on another
machine needs a reachable Loom address; `127.0.0.1` always means its own
machine. Existing Loom network/control-key requirements still apply.

Example personal tool call:

```json
{"query":"travel preferences","budget_tokens":800,"sources":["personal-notes"],"personal":true}
```

## Index limits and V1 scope

Brain accepts `.md`, `.mdx`, `.txt`, `.rst`, `.org`; repositories additionally
include `README*`, `docs/**` and Markdown anywhere. Known binary extensions,
invalid UTF-8/NUL-containing content, `.git`, `node_modules`, `vendor`, `dist`
and `build` directories are excluded. Files over 1 MiB are skipped. All
symlinks are skipped, including internal links; root-confined Go file access
also blocks escaping symlink replacements. Source directories are read-only.

There are at most 20,000 accepted files per source, 60,000 chunks and 64 MiB
of retained file/chunk payload across the whole index (including overlap,
paths and heading metadata). Directory entries are read in bounded batches. Limits stop further
acceptance and appear in source errors. These bound index data, not the total
Go process RSS; postings, metadata and old/new snapshots add memory overhead.
Individual source failures keep results from other readable files/sources.
Refreshes/source changes are serialized; readers see one published snapshot.
Stored discussions are read in batches of at most 128 records / 16 MiB,
without migrating archive metadata. Individual stored discussion records over
16 MiB are skipped with a source error; the running native discussion uses
its existing in-memory journal snapshot. Private runtime metadata is not indexed.

Markdown chunks follow ATX/setext headings, preserve ancestry, ignore fenced
code headings, treat titles longer than 512 characters as ordinary text, and contain at most approximately 1200 characters with a
100-character overlap inside a section. BM25 uses word frequencies with a
filename/heading boost, case/accent folding (French and English), no stemming,
and a boost for quoted exact phrases. V1 has no semantic embeddings, graph,
distillation, file watching or automatic prompt integration. The implementation
is Go-only and supports Linux, macOS and Windows without CGO.
