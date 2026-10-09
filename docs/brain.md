# Brain / Context Service

Brain connects user-owned knowledge sources and returns cited context. By
default it uses BM25 without model calls or external APIs. Optional semantic
indexing and explicit distillation are described below. The package
`internal/loom/brain` receives its storage, document providers and availability
check explicitly. Thin `brain_*.go` adapters connect it to Loom's storage and
web server. A project automatically searches its connected second brains and
its own Loom discussions for the latest user message. External execution shares
that prepared context under the normal destination-consent rules.

Run `loom web 2510` (or the desktop app). The process must remain running;
the browser can be closed. Indexing runs at startup and every three minutes.
`POST /api/brain/reindex` also refreshes synchronously. Changes to source
definitions take effect immediately; new file content appears on the next
refresh. No harness settings are changed automatically.

The **Brain** page has three direct sections: second brains, Skills and MCP
servers. The first connects canonical knowledge sources and exposes search and
indexing status. Skills and MCP remain execution capabilities rather than
knowledge stores. The owned skills home can live in the primary second brain
so its folders sync with that vault; additional skills folders remain read-only
linked sources. Loom's conversation indexing, Markdown memory,
retrieval and semantic layers operate in the background as cognitive
continuity; users do not have to wire those layers into every project.

## Linked second brains

The Brain page groups user-owned second brains separately from Loom's built-in
continuity sources. Multiple sources may use the same connector type;
`connector` is `folder`, `git`, `git-remote`, `obsidian` or `webdav-mount`.
Folders, Obsidian vaults and WebDAV/network storage must already be mounted on
the Loom host. `git-remote` clones an HTTPS or SSH remote into Loom's private
managed source directory and can explicitly fast-forward it from the UI. URL
credentials are rejected; use the host's credential helper or SSH keys. Loom
does not automatically commit or push changes.

One source can be the writable primary second brain. Loom direct models receive
bounded `brain_write` and `brain_edit` tools, local ACP harnesses receive that
directory as an additional workspace root, and harnesses using Loom's
authenticated Brain MCP receive the same two write tools. Writes are confined
to relative Markdown paths, refuse symlink escapes, cap each page at 1 MiB and
refresh the index. The primary is the only source Loom asks agents to maintain
proactively. Other sources are either read-only or writable only for an explicit
user request; they are not attached as automatic writable roots.

`kind` is independent of the connector. Choose `personal` for a personal vault;
its results require both an explicit selected source ID and personal access.
The search UI can select several sources, then search/read their cited results
without a model call. Projects inherit the connected sources with a bounded
default retrieval budget. Editing a definition invalidates stale scoped chunks;
removing a definition deletes only its index, never its source files.

Includes narrow eligible text; excludes remove matching relative paths. Known
credential files (`.env`, `auth.json`, private-key files) and directories such
as `.ssh`, `.codex`, `.claude` and `.project-local` are excluded. This is not a
secret scanner for arbitrary note text: keep sensitive material out of linked
context or exclude its files. Sources remain the truth; BM25/vectors/distilled
memory are rebuildable selection layers, not another canonical knowledge store.

## Sources and storage

Stored definitions have `id`, `label`, an absolute directory `path`,
`kind` (`context`, `personal`, `repo`), `connector`, `permission`, `primary`,
optional remote/branch metadata and optional `include` and `exclude` globs.
IDs are 1–64 ASCII letters/digits/underscore/hyphen, starting with a letter
or digit. Labels have a 200-byte limit. There are at most 100 stored sources.
Includes are relative to the root and support `*`, `?`, character classes,
and recursive `**`: e.g. `**/*.md`, `docs/**`, `notes/*.txt`.
They narrow the normal file selection; at most 64 globs, 256 bytes each.

Three built-ins are read-only:

- `conversations`: canonical Markdown files in `Discussions/`, with visible
  text and tool summaries. Missing historical files are backfilled from journals.
- `distilled`: explicit distillation/review items; pending and rejected
  suggestions stay out of retrieval.
- `memory`: native Markdown memory indexes and files through the primary or
  encrypted fallback store.

Default requests include context, repositories, conversations, memory and distilled items.
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
built-in results immediately. Original memory and discussion text is **never persisted
in Brain's BM25 cache**, including while the vault is unlocked.

The saved index retains file mtime/size and source-scope fingerprints. At
restart, unchanged file chunks are reused and the inverted index is rebuilt
in memory. Changed files are reread; removed, excluded and unreadable files
are dropped on refresh. A changed directory/kind/include/exclude list invalidates
that source's old cache. As with any mtime/size cache, an edit preserving both
attributes will not be detected. Reads return the indexed snapshot, not an
arbitrary file from disk. Invalid cache JSON is rebuilt; invalid definitions
or inaccessible encryption keys are errors, not silently erased settings.

## HTTP API

Browser routes use Loom's authenticated session; automation/MCP clients use
the control-key authentication (`Authorization: Bearer …`),
distinct from the model-completion API key. The key is checked per request.
With neither an interface password nor a configured control key, loopback access
follows the normal bootstrap policy.
Responses use `Cache-Control: no-store`. POST JSON uses
`Content-Type: application/json`, one JSON object and a 128 KiB body limit.

| Method / path | Input | Output |
| --- | --- | --- |
| `GET /api/brain/sources` | — | `{ok,refreshing,refresh_seconds:180,sources:[{id,label,path,kind,connector?,remote?,branch?,permission?,primary?,include?,exclude?,read_only,files,chunks,last_indexed,last_checked,error?}]}` |
| `POST /api/brain/sources` | `{"action":"add","label":"Knowledge","path":"/path/to/notes","kind":"context","connector":"folder","permission":"read","primary":false}` | Same source list; `add` creates/replaces the complete definition. `action` defaults to `add`. |
| `POST /api/brain/sources` | `{"action":"add","label":"Knowledge","remote":"https://git.example/user/brain.git","branch":"main","kind":"repo","connector":"git-remote","permission":"write","primary":true}` | Clones a managed remote checkout, then saves and indexes it. |
| `POST /api/brain/sources` | `{"action":"sync","id":"knowledge"}` | Fast-forwards a managed Git source and refreshes its index. |
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
ranges themselves; snippets never contain injected HTML. Scores are BM25 or reciprocal rank fusion
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

## Skills home

Skills default to `LOOM_HOME/skills`. The authenticated, no-store endpoint
`GET /api/skills/home` returns exactly:

```json
{"mode":"loom","dir":"/path/to/loom/skills","relative":"skills","brain_source":"vault","fallback":false,"reason":"","count":2,"detected":[{"relative":"skills","count":2}]}
```

`mode` is the saved `loom` or `brain` choice; `dir` is the active absolute
folder. `relative` is the configured brain-relative folder (default `skills`).
`brain_source` is the primary source ID, or `""` when unavailable. `count`
counts immediate skill folders containing a regular `SKILL.md`. `detected`
lists candidate folders inside the primary, sorted by relative path, from
root (`.`) through depth three, skipping `.git`, `node_modules`, `.loom` and
`.obsidian`; directory symlinks are not traversed. Empty lists are `[]`.

`POST /api/skills/home` accepts `{"mode":"brain","relative":"skills"}`
(or omit `relative` for the default) and `{"mode":"loom"}`. Brain mode
requires an available writable primary. Absolute paths, `..` components,
escaping symlinks, the brain root and `.loom` are rejected. Operations use
`os.Root`, including folder creation and file copies. Overlapping old/new
homes are rejected; selecting the same home is a no-op.

Switching to Brain creates the target if needed and copies whole skill folders
from the previous owned home only where the name does not already exist.
It never overwrites or deletes the old home. Success returns the GET object
plus `"copied":["review"]` and `"conflicts":["existing-name"]`; both are
arrays of folder names. Switching back to Loom copies nothing and returns
empty arrays. Copies preserve regular-file modes, reject symlinks and known
credential filenames, and skip repository metadata and private harness/account
directories. Keep secrets out of skill instructions and assets.

If the saved Brain home becomes unavailable (no primary, locked vault,
missing/unreadable source or skills folder), `dir` falls back to
`LOOM_HOME/skills`, `fallback` is `true`, and `reason` explains why. The saved
Brain choice is retained and becomes active again when available. Errors use
`{"ok":false,"error":"message"}` (400 for invalid paths/settings or failed
copies; 423 for a locked vault). Unsupported methods return 405 with
`{"error":"method not allowed"}` and `Allow: GET, POST`. Authentication and
JSON body/content-type rules match the other browser/control endpoints.

Switches resync opted-in native harness sinks asynchronously. Their manifests
retain the exact link targets so old managed links can be replaced without
touching user replacements. Harnesses read the canonical folders through links
(the existing marked-copy fallback still applies on Windows). The active
skills folder is excluded from note indexing, including cached entries;
other notes and linked skills remain unchanged.

## MCP for harnesses

The same engine is available at `http://127.0.0.1:2510/mcp/brain` using
Streamable HTTP and the Go MCP SDK. It exposes the read-only `brain_search`,
`brain_pack`, `brain_read`, `list_skills` and `read_skill` tools, plus
`brain_write` and `brain_edit` for
write-authorized second brains. Omitting `source` targets the proactive primary;
another source ID is intended only for an explicit user-requested update. Arguments and structured results
match their HTTP equivalents; MCP `heading` and `sources` are JSON arrays.
Tool errors use MCP's error results.

`list_skills {}` returns `[{"name":"review","title":"Review","description":"Find bugs"}]`
for the owned library and linked skills available to Loom's harness distribution.
Names are folder names; collisions are qualified as `<source-id>:<folder>`.
`read_skill {"name":"review"}` returns `{"text":"complete SKILL.md text","files":["SKILL.md","scripts/check.sh"]}`.
File paths are relative to that skill, sorted, limited to 200 regular files;
symlinks are skipped, and `SKILL.md` is limited to 1 MiB. Both tools are read-only
and check vault availability on every invocation. They expose instructions,
not permissions, and let harnesses without native skills folders read the same
library. These tools never change sources, bindings or native configuration.
The control key, when set, must be passed
as a Bearer header on every HTTP request. This endpoint does not manage source

Tool errors use MCP's error results. A control key or dedicated gateway token
can be passed as a Bearer header on every HTTP request; browser sessions keep
their normal authentication. This endpoint does not manage source
definitions or trigger model generation. It keeps DNS-rebinding and
cross-origin protection, bounds request bodies and does not retain MCP sessions.

Claude Code (substitute your reachable Loom URL; omit the header only if no
control key is set):

```sh
claude mcp add --transport http loom-brain http://127.0.0.1:2510/mcp/brain \
  --header 'Authorization: Bearer YOUR_LOOM_CONTROL_KEY'
```

The command writes a native Claude Code MCP entry for the current project.
Keep that entry private if it contains a literal key. See the official
[Claude Code MCP documentation](https://code.claude.com/docs/en/mcp).

Codex: add the following to its user `~/.codex/config.toml`, and supply
`LOOM_CONTROL_KEY` in the environment of the Codex process:

```toml
[mcp_servers.loom_brain]
url = "http://127.0.0.1:2510/mcp/brain"
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

## One Loom MCP gateway per harness

`/mcp/loom` is a stateless Streamable HTTP gateway. It shares the existing
Brain and memory tool registration with `/mcp/brain`, including `memory_index`,
`memory_read`, `memory_write` and `memory_delete`. It also proxies every enabled
Loom MCP server through the same client pool used by Loom chat. Hidden tools
(`disabledTools`) remain hidden. Arguments and MCP results, including content,
structured content and `isError`, pass through without flattening or truncation;
proxied calls have a 60-second timeout. Discovery skips unavailable upstreams
and logs their names without upstream error text or credentials.

Upstream tool names use `<server>__<tool>`, sanitized to ASCII letters, digits,
underscores and hyphens, with a 64-character maximum. Lossy sanitization and
truncation append a deterministic identity suffix so an unavailable upstream
cannot redirect a cached name to another server. Remaining collisions receive
a deterministic suffix in sorted server/tool order. `loom_gateway_status` is
read-only and returns `{upstreams:[{name,enabled,connected,tools,error?}]}`;
`tools` counts visible tools and unavailable upstreams report
`error:"upstream unavailable"`. Disabled upstreams are listed without connecting.
Both MCP endpoints retain origin protection, the 128 KiB request-body limit,
no-store responses and normal Brain/vault availability checks.

Registration is explicit, through authenticated control APIs. The gateway token
is separate from the control key, stored only as a hash in Loom's store, and
accepted only at `/mcp/loom` and `/mcp/brain`. Registration generates a token if
none exists. Plaintext appears only in the private harness configurations,
process memory and the authenticated token-rotation response. Existing owned
configurations let Loom recover the token after restart. If a token exists but
no owned configuration retains it after restart, rotate it before registering.
The token is never written to the Brain folder or Markdown.

| Method / path | Input | Success output |
| --- | --- | --- |
| `GET /api/mcp/gateway` | — | `{url,token_set,harnesses:[{id,name,supported,registered,file}]}` |
| `POST /api/mcp/gateway` | `{harness,enabled}` | Same gateway object after registration/removal. |
| `POST /api/mcp/gateway/token` | `{rotate:true}` | `{ok:true,url,token,token_set:true}`; return the new plaintext token once and rewrite every registered entry. |
| `GET /api/mcp/portable` | — | `{servers:[{name,config}]}`; only entries present in the primary Brain sidecar and absent locally, with secret markers. |
| `POST /api/mcp/portable/import` | `{names:["server-name"]}` | `{ok:true,imported:["server-name"]}`; import all selected entries disabled with empty secret values. |

These control routes require the existing browser/control-key authentication;
the gateway token grants no control API access. POST uses the normal strict
JSON decoder and 128 KiB limit. Invalid requests, foreign/edited entries and
missing primary Brains return 400 with `{ok:false,error}`; locked vaults return
423. Authentication and method errors retain the normal control API shapes.
Gateway responses never contain a token except for token rotation.

Harness IDs and the sole owned entry are:

| Harness ID | User file | Entry |
| --- | --- | --- |
| `claude-code` | `~/.claude.json` | `mcpServers.loom = {"type":"http","url":URL,"headers":{"Authorization":"Bearer TOKEN"}}` |
| `codex` | `~/.codex/config.toml` | `[mcp_servers.loom]` with `url` and `http_headers = { "Authorization" = "Bearer TOKEN" }` |
| `opencode` | `~/.config/opencode/opencode.json` | `mcp.loom = {"type":"remote","url":URL,"headers":{"Authorization":"Bearer TOKEN"},"enabled":true}` |
| `antigravity` | `~/.gemini/config/mcp_config.json` | `mcpServers.loom = {"url":URL,"headers":{"Authorization":"Bearer TOKEN"}}` |

Codex's static HTTP header key is documented in the
[configuration reference](https://developers.openai.com/codex/config-reference/).
No environment export is needed. Its owned block is delimited by
`# loom-gateway begin` / `# loom-gateway end`; all other TOML text is preserved.
JSON editing preserves unrelated keys and entries, including large settings
files, but may normalize whitespace. Existing files get a private
`<file>.loom-backup` before their first change, only when that backup is absent.
Writes use an atomic replacement confined to the user's home. Loom refuses to
replace an existing `loom` entry it did not create, or one edited outside Loom.
Remove an edited entry manually and unregister it before rotating. Rotation
prepares every change first and rolls applied files back if a write fails.

`url` uses the running Loom listener's host and port, substituting `127.0.0.1`
for all-interface binds. Loom must remain running; restart the external harness
to load configuration changes. Loom restarts a retained ACP adapter on its next
turn when the owned gateway entry changes. A loopback URL targets the same machine as the
harness. No native sign-in, global permissions or other MCP entries are changed.
A registered ACP harness receives no individual MCP servers when it inherits
all globally enabled selections: the harness reads its native `loom` entry.
An explicit project/session selection, including an empty selection, keeps the
existing per-session ACP behavior; explicit harness bindings also remain
per-session. The native gateway entry stays available in that harness.

The portable sidecar is `<primary>/.loom/mcp.json`, with the existing
`{"mcpServers":{"name":CONFIG}}` definition format. `CONFIG` contains `enabled`
and optional `type`, `command`, `args`, `env`, `url`, `headers` and
`disabledTools`, as in Loom's local MCP file. Every env/header **value** is
replaced by `"${secret}"`; keys remain available to identify missing credentials.
Keep credentials in env/headers rather than commands, arguments or URLs.
Definitions are exported on changes, primary-source updates and Brain refresh;
a fresh installation retains sidecar entries missing locally so they can be
imported. Explicit local deletion removes that definition's previous export.
The 4 MiB sidecar is read and atomically written through `os.Root` confinement.
Malformed/unavailable sidecars are logged without credential contents and do not
prevent saving local definitions. Imports never overwrite local entries, never
enable a server and never connect to it; fill the empty env/header values and
explicitly enable it afterward.

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
and a boost for quoted exact phrases. There is no graph or file watching.
Project source selection enables discussion context retrieval; without it,
Brain does not add text to prompts.
Semantic indexing and distillation are opt-in additions to this same engine. The implementation
is Go-only and supports Linux, macOS and Windows without CGO.


## Optional semantic index

Semantic indexing is off by default. Loom downloads an embedding GGUF only
when `download` is requested, into `LOOM_HOME/brain/embed/`, using the existing
Hugging Face downloader. It resolves the installed `llama-server` through
`resolvedEngineBin`; no second engine installation or chat-engine restart occurs.

| Model ID | Hugging Face repository | Quantization | Approximate size |
| --- | --- | --- | --- |
| `nomic` (default) | [nomic-ai/nomic-embed-text-v1.5-GGUF](https://huggingface.co/nomic-ai/nomic-embed-text-v1.5-GGUF) | Q8_0 | 140 MiB |
| `bge-small` | [CompendiumLabs/bge-small-en-v1.5-gguf](https://huggingface.co/CompendiumLabs/bge-small-en-v1.5-gguf) | Q8_0 | 37 MB |

Both choices target English text. Nomic uses the `search_document:` and
`search_query:` task prefixes; BGE uses its retrieval query instruction.
A dedicated child process listens only on loopback at an allocated free port:
`--embedding --ctx-size 2048 -ngl 0`, model-specific pooling (Nomic mean, BGE CLS) and 2048-token batch buffers.
It runs on CPU, starts when indexing or searching needs it, and stops after ten
minutes without an embedding request, on disable, or on service shutdown.
Loom kills only the child it owns. No provider credentials are inherited.
The port reservation is released before launching; a competing bind produces
a readiness error rather than falling back to the chat engine.

Indexing is explicitly started, with at most one request and two chunks per
batch. Each completed batch appends a durable checkpoint. Requests resume
missing chunk IDs; they never run generation or restart automatically after a
crash. A partial last checkpoint is ignored and compacted on the next index
request. `LOOM_HOME/brain/vectors.bin` stores little-endian float32 vectors,
chunk IDs and model identity in framed records, encrypted with the existing
vault codec when active. It contains no chunk text. IDs include chunk contents,
so changed/deleted chunks stop matching immediately; explicit indexing prunes
obsolete vectors. Changing models/providers discards the previous vectors.
Vector dimensions must remain consistent, finite and nonzero. The vector file
is limited to 256 MiB, with at most 60,000 vectors and 4096 dimensions.

Search (HTTP and MCP `brain_search`) and context packs fuse the top 100 BM25
and cosine candidates with reciprocal rank fusion (`k=60`). Source filters and
personal-note authorization apply to both rankings. BM25 alone is used when
disabled, when no matching vectors are ready, or when embedding fails. Partial
indexes are usable. Source access is checked again after the model request.
An enable request's `sources` and `personal` control the indexing scope; personal
sources still require their explicit IDs plus `personal:true`. They are excluded
from default indexing, including cloud indexing.

A connected provider can instead supply an OpenAI-compatible `/v1/embeddings`
endpoint. Choosing its saved `provider_id` and embedding `model` requires
`consent:true`: indexed note contents and subsequent search queries leave this
machine. Consent is stored in `semantic.json`, tied to the provider destination
and model. Credentials remain in the existing provider/keychain layer. Redirects
are refused; disconnecting the provider prevents further requests. No provider
is contacted by merely reading state, and default BM25 never sends text.
Selecting a local model clears the cloud selection and its consent.

All additional routes use the same authenticated-session/control-key, no-store, method and strict JSON
rules as the APIs above:

| Method / path | Input | Output |
| --- | --- | --- |
| `GET /api/brain/semantic` | — | `{enabled,auto_index,model,provider_id?,consent,models,model_present,server_running,indexing,chunks_embedded,chunks_total,error?}` |
| `POST /api/brain/semantic` | `{"action":"enable","model":"nomic","sources":["project"]}` | Current state; no download/index yet |
| `POST /api/brain/semantic` | `{"action":"enable","provider_id":"saved-id","model":"embedding-model","consent":true}` | Current state; explicit cloud selection |
| `POST /api/brain/semantic` | `{"action":"download"}` | State after downloading the selected local model |
| `POST /api/brain/semantic` | `{"action":"index"}` | State; indexing continues in the background (two-hour maximum); poll GET |
| `POST /api/brain/semantic` | `{"action":"auto","auto_index":true}` | Explicit background refresh opt-in; same selected sources/destination, no download |
| `POST /api/brain/semantic` | `{"action":"disable"}` | Disabled state; cancels indexing and stops the owned child |

Enable/configuration changes during indexing require disabling first. Model or
provider errors are available in state; missing models require the explicit
`download` action. A locked vault blocks state, search and checkpoints. Enabling
vault encryption requires another semantic index request to rewrite older
plaintext vector checkpoints, just as the BM25 cache requires refreshing.

## Explicit discussion distillation

`POST /api/brain/distill` accepts exactly one of `discussion_id` or `since`.
`since` accepts `YYYY-MM-DD` (UTC) or RFC3339 and selects entire discussions
updated/saved on or after that date. It does not guess individual message dates.
Common discussions bound to a native archive are processed once, under their
common discussion ID; unbound archives use their native ID. Only visible user
and assistant text is projected, without system instructions, tools, hidden
reasoning, approvals or runtime state. Native message indexes refer to the
portable display-journal text projection; common indexes retain their original
zero-based transcript positions.

The active chat engine (`engineBase`, `engineRequestModel`, its own API key)
receives bounded batches with low temperature and a JSON-object output format.
Loom validates exact fields, item kinds, text bounds and a supplied message index;
invalid JSON/provenance is retried once, then fails. The model never chooses the
source discussion ID or date. A non-loopback linked engine additionally requires
`consent:true` in each distillation request before any transcript is sent.
There is no automatic or scheduled distillation, no automatic model load, and
no use of provider keys by the local embedding process.

Items have `{id,kind,text,source:{discussion_id,message_index},date,review?}`, with `kind`
being `decision`, `fact`, `todo` or `preference`. The date is the discussion's
update/archive-save date (the snapshot date for an active native discussion),
not an invented event date. Content-derived IDs dedupe
identical items. The durable store `LOOM_HOME/brain/distilled.json` uses the existing
vault codec and private file modes. It is a built-in Brain source, indexed on
startup/refresh and immediately after a successful review/deletion; pending and
rejected candidates are excluded. Legacy items without a review field remain
accepted for compatibility. The source is
excluded from the rebuildable BM25 text cache. Model-generated summaries remain
untrusted. New extraction results have `review:"pending"`; accepting an item
marks it reviewed without removing its original discussion/message provenance.
Rejected items are retained in the memory store but hidden from retrieval.
The UI shows active candidates/accepted items; the review API can restore a
rejected item. Explicit deletion removes it.

| Method / path | Input | Output |
| --- | --- | --- |
| `POST /api/brain/distill` | `{"discussion_id":"id"}` or `{"since":"2026-09-01"}`; optional explicit remote-engine `consent:true` | `{ok,items}` after generation, persistence and refresh |
| `GET /api/brain/distilled` | — | `{items:[...]}` |
| `POST /api/brain/distilled/review` | `{"id":"item-id","review":"accepted","text":"Reviewed text"}`; text optional; review accepted/rejected | `{ok}` after persistence and refresh |
| `POST /api/brain/distilled/delete` | `{"id":"item-id"}` | `{ok}` after deletion and refresh |

Limits: ten minutes per request, 1000 selected discussions / 64 MiB of retained
text, 24,000 JSON bytes / 100 messages per chat batch, 64 items per model output,
4000 bytes per durable item, 10,000 newly extracted items per request and a 4 MiB
durable store. Oversized messages fail without truncation. Upstream bodies and
credentials are never echoed in errors; model responses are bounded to 4 MiB.
Saving a new distilled item list rewrites it with the currently active vault
codec; older plaintext distilled data requires such a write after encryption
is enabled. As with other local data, vault locking blocks access immediately.


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

All routes use Brain authentication, strict bounded JSON, vault checks and
no-store responses. Scope is exactly `global` or `project:<id>`.

| Method / path | Input | Output |
| --- | --- | --- |
| `GET /api/brain/memory` | `?scope=global` or `?scope=project:<id>`; default global | `{index,items:[{file,name,description,type,text,updated_at,metadata?,malformed?}],path,warning?}` |
| `POST /api/brain/memory` | `{scope,file?,name,description,type,text}` | Saved `{file,name,description,type,text,updated_at,metadata?}` |
| `POST /api/brain/memory/delete` | `{scope,file}` | `{ok:true}` |
| `POST /api/brain/memory/consolidate` | `{discussion_id}` | Consolidation status after one run or a silent skip |
| `GET /api/brain/memory/status` | — | `{running,last_run,last_error,last_operations:[...],discussion_id?}` |

Timestamps are Unix milliseconds. Status persists with the existing codec.
Storage/model/validation failures populate `last_error`; unavailable models,
disconnected destinations and generating turns skip without errors.

Both Brain/gateway MCP servers expose:

- `memory_index {project_id?}` → `{global:{index,items,path,warning?},project?:{index,items,path,warning?}}`.
- `memory_read {scope,file}` → one memory file object.
- `memory_write {scope,file?,name,description,type,text}` → saved file object.
- `memory_delete {scope,file}` → `{ok,file}`.
- `search_discussions {query,project_id?,limit?}` → cited passages in the
  canonical `Discussions/` tree, readable with `brain_read`.

Existing brain search/pack/read and authorized note write/edit tools remain.
Legacy `remember`, `update_memory`, `forget_memory`, `list_memory` and
`get_handoff` MCP tools are removed. UI and agent wiring are separate work.

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
| `discussion` (default) | Loom-held Local/Cloud discussion's own route; harnesses use `brain.consolidation_fallback` |
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

## One-time migration

First access converts legacy `.loom/memory/<class>/*.md`, encrypted fallback
items and accepted distilled candidates. User-profile becomes a `user` file
named Profile; project notes become project `project` files. Reflex/procedural
become feedback; global semantic becomes user, project semantic becomes project;
episodic/working become project. Handoffs, discussion/project state, session
summaries and pending/expired/superseded items are skipped. Malformed originals
remain retained and are counted in the report.

Old folders are renamed to `memory.legacy/` before creating new files, including
on case-insensitive filesystems. Deterministic filenames and archived-source
recovery make interrupted retries safe. `.loom/migration-memory.md` marks
completion only after writes succeed; subsequent access leaves it unchanged.
Old flat agent pages used for explicitly selected shared preferences remain
readable by that preference API; they are not automatically user profiles.

Removed HTTP routes: `/api/brain/items`, `/api/brain/items/update`,
`/api/brain/items/forget`, `/api/brain/consolidation`, `/api/brain/consolidate`,
`/api/brain/core-files`, `/api/brain/continuity`, `/api/brain/continuity/run`,
`/api/brain/continuity/status`, `/api/brain/continuity/handoff`.
Explicit distillation/review routes remain independently available.

## Context engine

### Frozen discussion snapshot

Loom retains its frozen-snapshot mechanism. Session start captures global and
project `MEMORY.md` indexes and full text of `user` memories up to 1500 Unicode
characters total. Local/Cloud discussions also capture up to three topic files
ranked by BM25 against the **first user message**, within a separate 1500-token
allowance. Topics appear once in frozen system text. Harnesses read them on
demand through files or MCP.

The format upgrade invalidates old class-based snapshots once. Later
memory/query changes never invalidate an existing snapshot. Project, route,
instruction, compaction and explicit refresh changes retain existing invalidation
rules. Frozen memory allowance is 15,000 estimated tokens, sufficient for both
maximum indexes, profile and topics. No class-based selection, handoff pins or
usage-touch writes remain. Context items retain source/scope/reason/token
explanations; `classes` is an empty compatibility map. Memory is untrusted data,
never permission.

Brain **note passages** retain current-query retrieval and normal project budgets.
They are prepended only to the outgoing user message as
`<loom-context>\n…\n</loom-context>\n\n`, outside journals/portable history.
Memory folders are excluded from that passage path. Previews never persist a
new snapshot. `POST /api/runtime/sessions/refresh-context {id}` clears it for the
next turn without generation. `frozen_context`, `frozen_revision`, turn
`context_items`/`context_budget`, `context_extras` and revision guards retain the
existing send/replay contract. Item costs include separators and framing.

## Project continuity and retrieval scope

Projects retain instructions, folders, skills and membership. Current-query note
retrieval and scoped verbatim discussion search work from the first turn. Default
note budget is 1500 tokens, capped at 8000; semantic queries have a three-second
bound with BM25 fallback. Historical project continuity fields remain readable;
no model maintains another state store. Continuations retain `continued_from`;
verbatim transcripts supply recall.

HTTP/MCP search/read/pack accept `project_id` and intersect narrower
`path_prefixes` with allowed discussion paths. Personal sources require explicit
IDs and `personal:true`. Unscoped operator calls retain existing policy.
Runtime-private memory/tool state is not portable.

`GET/POST /api/context/preferences` retains `{page,external}` for older flat
pages, with a 4000-byte limit and external-sharing opt-in. It remains separate
from native Markdown user profiles.

## Freshness and automatic semantic maintenance

Source state distinguishes `last_checked` from the last successful `last_indexed`;
errors do not claim a successful index. The Sources page reads status every
15 seconds while visible and shows pending/running/overdue/error states. Source
refresh remains startup plus every three minutes and incremental mtime/size.

Semantic maintenance uses that same cycle only after explicit `auto_index:true`
selection. It resumes missing/changed chunk IDs and prunes obsolete vectors
through the existing indexer; unchanged text makes no embedding calls. A local
model must already be downloaded. No source edit starts an implicit download.
Cloud indexing uses the selected sources and previously consented destination;
it can send changed text and incur charges. Changing model/provider/source
selection clears automatic indexing, requiring another explicit opt-in. An empty
(default) source list is frozen to currently accessible non-personal source IDs
at auto-index opt-in; linking a new source does not silently expand that scope.
Disabling semantic search cancels indexing. No automatic distillation is added.

The existing English-focused `nomic` default remains unchanged. The optional
`nomic-v2` selects Nomic's multilingual v2 MoE Q8 GGUF with documented
`search_query:`/`search_document:` prefixes. See the
[official model card](https://huggingface.co/nomic-ai/nomic-embed-text-v2-moe-GGUF/blob/main/README.md).
Its runtime/quality acceptance depends on the installed llama.cpp and the user's
corpus; selection alone does not download or prove multilingual retrieval quality.

## Brain roles

Each connected second brain has one role:

- **Primary**: Loom's working memory. Retrieved for every relevant request and
  the only brain Loom and agents write durable knowledge to.
- **Context**: its useful passages are added to requests automatically.
- **Secondary**: never added on its own. The model is told the brain exists
  (label, source id, access) and searches it through Loom's brain search only
  when a request needs it. Read-only by default; "ask before changing" or
  writable can be granted per brain.

The role is stored on the source (`primary`, `secondary`) in `sources.json`;
existing sources keep their behaviour (Context) until changed.


## User profile

One global `semantic` item tagged `user-profile` describes the user (who they
are, how they work, preferences). The context engine always includes it first,
truncated to the semantic budget rather than dropped. Brain › Memory edits it
as **You**; agents update it with `update_memory` instead of creating another.

## Automatic cognitive continuity

Continuity has an **always-on deterministic layer**. At every workspace or native
Loom turn end, an asynchronous worker builds a discussion handoff without model
calls, network access or an installed engine. Bursts coalesce per discussion.
It uses the discussion title, first user message and last two requests (300
characters each), the latest ACP plan and its statuses, up to 20 deduplicated
reported edit/write/create targets, cumulative command count and last five
command titles, and failures from the latest turn. Paths are relative to the
workspace when possible. Reported write targets are not verified file diffs.

The last completed turn's final assistant message supplies the recap (up to 800
characters, cut at a sentence boundary where possible). Incomplete turns retain
the previous completed recap and record their errors. Open items are unfinished
plan entries and up to three questions from the latest assistant message. The
fixed Markdown headings are `Goal`, `Plan`, `Done`, `Recap` and `Open`; values
retain the discussion's language. Runtime, model and UTC time form the footer.
The full handoff fits 2,000 Unicode characters, reducing Done details before the
recap. Source text is quoted data, with HTML and code fences removed and
whitespace collapsed; it is never a grant of instructions or permissions.

Each discussion has a working item scoped to `task:<discussion_id>`, tagged
`discussion-state` and `handoff`, with discussion provenance. Deterministic
updates overwrite that item in place without creating supersession history.
A small encrypted Loom-store record keeps incremental first/request/plan/file/
command state; later updates read only the latest turn and stored state, never
rescan the transcript. Vault locking applies to both records and memory items.

A project also has a working `project-state` item, rebuilt from its three most
recently active discussions, newest first (ties by ID). Each entry includes its
title, first request, open items, recap's first sentence and `discussion:<id>`
marker. It fits 2,500 characters and updates in place. When a model refinement
is newer than those discussions, its bounded base text is retained above one
`Recent discussions` section; later discussion activity makes stale refinement
ineligible. The original refinement text is retained separately from aggregation.

Project state is pinned even on a new discussion's very first turn. A continuing
discussion adds its own handoff only from 20 turns onward, avoiding duplication
of short transcripts. Project and discussion pins share the working budget;
large pins are truncated instead of dropped. User-profile priority, scoped
retrieval, supersession, deduplication and the total memory budget still apply.
A project's two latest model-generated episodic summaries remain preferred
within the episodic budget.

**Refine with a model** is a separate opt-in layer, off by default. The existing
`enabled` setting controls only this layer; disabling it never disables
handoffs. With `loaded_only:true` (the default), local refinement uses an
already-loaded model and skips if none is loaded. Automatic refinement waits
for ten idle minutes by default and at least two new user messages or 1,500
characters. A cancellable scan runs at startup and once a minute, with one
model operation at a time. It deduplicates native archives bound to discussions,
requires a new user message since the saved checkpoint, waits while generation
or preparation is active, and discards results when the discussion changes.

Settings live in `LOOM_HOME/brain/continuity.json`. An empty provider uses Loom's
chat engine; `model` can override its request model. A provider ID selects an
existing connected provider and its own credential. Cloud providers and
non-loopback linked engines require stored `consent:true`. Credentials never
enter Brain settings or memory files. Refinement costs model time or provider
tokens; deterministic handoff construction costs neither. Pinned handoffs, like
other context, count toward the selected executor's input context budget.

A refinement receives the last 24 KiB of new user/assistant text, the previous
episodic summary and current state, all marked untrusted. Strict JSON validation
allows one retry; the operation has a two-minute timeout and lifecycle
cancellation. It writes an episodic `session-summary` in the project or global
scope and refined working state, retaining supersession history. For an unbound
discussion with a handoff, refined working state uses `discussion-refinement`
without replacing the deterministic handoff. Durable facts become review
candidates. Successful model checkpoints in `brain_continuity` survive restart
and participate in vault encryption.

All HTTP endpoints use Brain authentication, vault checks and
`Cache-Control: no-store`; timestamps are Unix milliseconds:

| Method / path | Input | Output |
| --- | --- | --- |
| `GET /api/brain/continuity/handoff` | `?discussion_id=…` | `{"discussion_id":"…","text":"…","updated_at":0}` |
| `GET /api/brain/continuity` | — | `{"enabled":false,"idle_minutes":10,"provider_id":"","model":"","consent":false,"loaded_only":true}` |
| `POST /api/brain/continuity` | All settings except optional `loaded_only` (defaults true); idle minutes 1–1440 | Saved settings object |
| `POST /api/brain/continuity/run` | `{"discussion_id":"…"}` | `{"discussion_id":"…","at":0,"summary_id":"…","state_id":"…","skipped_reason":""}` |
| `GET /api/brain/continuity/status` | — | `{"running":false,"last_run":0,"last_error":"","recent":[]}` |

The read-only Brain MCP tool `get_handoff` is available through `/mcp/brain` and
`/mcp/loom`. Pass exactly one of `{"discussion_id":"…"}` or `{"project_id":"…"}`.
It returns `{"discussion_id":"…","text":"…","updated_at":0}` or
`{"project_id":"…","text":"…","updated_at":0}`. Missing handoffs return an
error; reads never generate or call a network service.

Model run-now is synchronous and bypasses the idle/minimum thresholds, but still
requires enablement, a new user message, no generation and destination consent.
Skips return 200 with empty item IDs and a reason; failures use `{ok:false,error}`.
Concurrent model runs are rejected. Status keeps the latest 20 model observations
in memory, newest first, and does not describe deterministic handoff activity.

The model response schema is exactly:

```json
{"summary":"…","state":{"objective":"…","done":["…"],"next":["…"],"open":["…"]},"facts":[{"class":"semantic","text":"…"}]}
```

Fact class accepts `semantic`, `procedural` or `reflex`; arrays may be empty.
Summary, rendered state and each fact fit 8 KiB, with up to 32 entries per array.

## Agents linked to the brain

Explicit local opt-in links agents to the writable primary brain's Markdown
memory: `Memory/MEMORY.md` plus topic files globally, and
`Projects/<slug>/memory/MEMORY.md` plus topic files per project. Native sessions
can use these files outside Loom, without its server running.

[Claude Code](https://code.claude.com/docs/en/memory) uses `autoMemoryDirectory`
in `~/.claude/settings.json` and local projects' `.claude/settings.local.json`;
Loom adds local ignore rules and never edits repository `settings.json` or
`AGENTS.md`. [Codex](https://developers.openai.com/codex/guides/agents-md)
(`$CODEX_HOME/AGENTS.md`, default `~/.codex/AGENTS.md`),
[OpenCode](https://opencode.ai/docs/rules/) (`~/.config/opencode/AGENTS.md`),
Antigravity (`~/.gemini/config/AGENTS.md`, verified in the installed agy documentation) and Pi (only with installed documentation identifying
its global instructions file) receive one short marked block. It explains
session-start reads, Claude-format frontmatter, Why/How guidance, index updates
and avoiding secrets/duplicates. `.loom/brain.json` preserves other metadata
and records `agent_projects:{"project-id":{slug,directory,memory}}`, with stable
project-ID slugs and brain-relative memory paths.

`GET /api/brain/agents` and successful POSTs return
`{memory_dir,agents:[{id,name,supported,linked,file,note}]}`.
POST takes `{"id":"codex","enabled":true}` (or `false`); the toggle is consent.
Local IDs are `claude-code`, `codex`, `opencode`, `gemini`, `pi`. `file` is the
global native config/instructions path; `memory_dir` is the absolute global
folder, or `""` without an available primary. Remote/paired installation runtime
IDs report `supported:false,note:"local only for now"`. Existing Brain auth,
strict JSON, no-store and `{ok:false,error}` error rules apply.

One-time `.loom-backup` files and ownership checks preserve user text/keys.
Unlink restores previous Claude settings and retains memory/backups; edited
entries or foreign markers are refused. Source/project changes re-point links
immediately, with reconciliation every three minutes. Removing the primary
clears owned entries but retains opt-in. Conflicts return an error after the
source/project change is saved. Native trust and access rules still apply.
