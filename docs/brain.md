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
linked sources. Loom's conversation indexing, reviewed memory,
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

- `conversations`: user and assistant text from Loom's native display journal,
  archives and common discussions. No system instructions, hidden reasoning,
  approvals, tool results or runtime metadata. Bound native discussions are
  not indexed twice. Paths identify the discussion and individual message.
- `distilled`: durable decisions, facts, todos and preferences extracted only
  on request, with discussion/message provenance. New items are review candidates, excluded from retrieval until accepted.
  They can be edited/kept, rejected or deleted through dedicated endpoints.
  The built-in source definition remains read-only.
- `memory`: the existing local agent's Markdown pages, read through Loom's
  decryption layer. Brain does not create another memory store or write pages.

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
Brain and memory tool registration with `/mcp/brain`, including `remember`,
`update_memory`, `forget_memory` and `list_memory`. It also proxies every enabled
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
| `gemini` | `~/.gemini/settings.json` | `mcpServers.loom = {"httpUrl":URL,"headers":{"Authorization":"Bearer TOKEN"}}` |

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


## Memory items (`.loom/`)

Brain v2 adds durable memory items alongside existing free-form memory pages
and distilled review candidates. Loom writes these files through its memory
operations; agents must use HTTP or MCP rather than edit `.loom/` directly.
The generic `brain_write` / `brain_edit` tools reject `.loom/` paths.

With a writable primary second brain, items live in the user-owned vault at
`<vault>/.loom/memory/<class>/<id>.md`, as plaintext YAML frontmatter and a
Markdown/plain-text body. These files remain readable by other tools even when
Loom's private vault encryption is enabled. Without a writable primary, the
same layout lives under `LOOM_HOME/brain/loom-memory/memory/<class>/<id>.md`;
files use the existing at-rest codec when encryption is enabled. The companion
`brain.yaml` is in `.loom/` or `loom-memory/`, respectively, and contains
`format: 1`, `created_at` (Unix milliseconds), and `imported_distilled: true`
after the one-time import completes. Unrelated metadata keys are preserved.
Loom's vault availability check protects both stores, including cached reads.

Example item:

```markdown
---
id: mem_example
class: semantic
scope: project:example
tags: [release]
importance: 0.5
confidence: 0.7
created_at: 1791417600000
updated_at: 1791417600000
last_used_at: 0
provenance:
    kind: user
    note: Explicitly requested by the owner
supersedes: []
status: active
---
Release candidates require a successful local validation run.
```

Every field except `text` appears in frontmatter; `text` is the exact body,
limited to 8 KiB of valid UTF-8. IDs are stable, unique, safe filename tokens
(1–128 ASCII letters/digits/underscore/hyphen, starting with a letter/digit);
creation generates one when omitted. The supported fields are:

| Field | Values / meaning |
| --- | --- |
| `class` | `working`, `session`, `episodic`, `semantic`, `procedural`, `reflex` |
| `scope` | `global`, or `project:<id>`, `machine:<id>`, `agent:<id>`, `task:<id>` with a nonempty ID |
| `tags` | String array (up to 128 tags, 256 bytes each) |
| `importance`, `confidence` | Finite scores from 0 to 1; creation defaults 0.5 / 0.7; explicit zero is retained |
| `created_at`, `updated_at`, `last_used_at` | Unix milliseconds; `last_used_at: 0` means never touched |
| `provenance` | Required `kind`: `user`, `agent`, `discussion`, `import`, `distilled`; optional `discussion_id`, zero-based `message_index`, `agent`, `note` |
| `supersedes` | Array of predecessor IDs (up to 128); history files remain present |
| `status` | `active` (creation/list default), `candidate`, `superseded`, `uncertain`, `expired` |

Writes use a temporary file and atomic rename, confined to the selected root.
Lists scan the six class directories and cache items until Loom writes. Invalid
item files are skipped and counted in the returned `malformed` field; metadata
or storage access errors remain errors. There is no filesystem watcher: edits
made outside Loom can require a service restart to refresh a cached list.

`remember` matches active items with the same class, scope and normalized text
(case folded with whitespace collapsed). It updates recency and keeps the
maximum importance instead of duplicating; existing text/provenance remain.
`update` patches only supplied text, tags, importance, confidence, status and
scope fields. When `supersede:true` accompanies a meaningful normalized text
change, it creates a successor that references the old ID, and marks the old
file `superseded`. `forget` marks an item `expired` without deleting its file.
Go callers can use `MemoryStore.Remember`, `Update`, `Forget`, `List` and
`Touch`; touching IDs updates only `last_used_at` for later context use.

All item routes use the existing authenticated-session/control-key wrapper,
strict bounded JSON and `Cache-Control: no-store`. Errors return
`{ok:false,error}` (423 while Loom's vault is locked).

| Method / path | Input | Output |
| --- | --- | --- |
| `GET /api/brain/items` | Optional `classes`, `scopes` (comma-separated or repeated; singular `class` / `scope` also accepted), `status`, `query`, `limit` | `{ok:true,items:[...],malformed:N}` |
| `POST /api/brain/items` | `{class,scope,text,provenance,...}`; optional `id`, `tags`, `importance`, `confidence`, `supersedes`, `status` | `{ok:true,item}` |
| `POST /api/brain/items/update` | `{id,patch:{text?,tags?,importance?,confidence?,status?,scope?},supersede?}` | `{ok:true,item}` |
| `POST /api/brain/items/forget` | `{id}` | `{ok:true,item}` with status `expired` |

Lists default to active items; `status=candidate` lists pending review and
`status=all` includes candidates and history. A project scope
also includes `global`; other scopes match exactly. Classes/scopes combine as
unions within each filter. Text queries are case-insensitive substrings. Results
sort by importance descending, then update recency descending, then ID for
stable ties. A zero/omitted limit returns all matches; negative limits fail.

The authenticated `/mcp/brain` server also exposes `remember`, `update_memory`,
`forget_memory`, and read-only `list_memory`, using the corresponding JSON
request/result shapes above. MCP filters use `classes` / `scopes` arrays.
These operations record and manage knowledge. Discussion context selection uses
these same items; the existing UI is unchanged.

On first use of each store, Loom imports accepted distilled items:
`decision` → `episodic`, `fact` / `preference` → `semantic`, `todo` → `working`.
Legacy items with empty review import as `uncertain`; pending/rejected items are
excluded. Imported items have global scope and retain distilled provenance,
discussion ID, message index and date. Deterministic IDs allow retries after an
interruption; `imported_distilled: true` is saved only after all files succeed.
The original `distilled.json` is never changed or removed by import. Later
review changes do not rerun this one-time conversion.

## Cheap consolidation and candidate review

A `candidate` is a proposed memory item, persisted in the same class directory
as active memory. Candidates never enter `SelectMemory` or discussion context;
default HTTP and MCP lists still return only active items. Review uses the
existing operations: accept with
`POST /api/brain/items/update {"id":"mem_…","patch":{"status":"active"}}`,
optionally including `text` in the patch; reject with
`POST /api/brain/items/forget {"id":"mem_…"}` (retained as `expired`). Editing
a candidate, including with `supersede:true`, keeps it pending unless the
patch explicitly changes its status.

After a user message is accepted, Loom runs a deterministic collector in the
background. It makes no model call and reads only that user message, excluding
attachments, prepared context, assistant output and private runtime state.
Detection is case-insensitive with Unicode word boundaries:

| Signals | Proposed class |
| --- | --- |
| `retiens`, `souviens-toi`, `n'oublie pas`, `remember`, `keep in mind`, `note that` | `semantic` |
| `toujours`, `jamais`, `désormais`, `à partir de maintenant`, `always`, `never`, `from now on` | `reflex` |
| `je préfère`, `j'aime pas`, `je veux pas`, `I prefer`, `I don't like` | `semantic` |
| `pour publier`, `la procédure`, `les étapes`, `steps to`, `the way to` | `procedural` |

The first matching row determines the class. Each matching sentence proposes
one item, split on `.`, `!`, `?` or newlines. Fenced/indented code, questions,
sentences shorter than 12 Unicode characters and messages over 4000 UTF-8 bytes
are skipped. Trimmed text is bounded to 500 bytes without splitting UTF-8.
At most three candidates are proposed per message. Candidates have importance
0.5, confidence 0.4, tag `auto`, and `discussion` provenance with the discussion
ID, zero-based portable message index and agent ID when known. Scope is
`project:<id>` when the discussion has a project, otherwise `global`.

Candidate insertion skips matching text in any class/scope whose status is
neither `superseded` nor `expired`. Matching lowercases text, collapses whitespace
and trims surrounding punctuation. Existing items are left unchanged. At most
50 candidates may be pending in the selected memory store; further suggestions
are dropped until review frees space. Dedupe and the cap share the store lock.
Collection errors are logged and do not fail the send. A locked or unavailable
Brain prevents collection.

`brain.auto_candidates` defaults to true. Brain has no general configuration
route, so its persisted value in `LOOM_HOME/brain/consolidation.json` is managed
through the following dedicated authenticated, no-store routes:

| Method / path | Input | Output |
| --- | --- | --- |
| `GET /api/brain/consolidation` | No body | `{"auto_candidates":true}` (current boolean) |
| `POST /api/brain/consolidation` | `{"auto_candidates":false}` (required boolean) | `{"auto_candidates":false}` (saved boolean) |
| `POST /api/brain/consolidate` | `{"discussion_id":"discussion-id","consent":true}` (`consent` defaults false) | `{"ok":true,"items":[MemoryItem,…]}` |

Whole-discussion consolidation is opt-in and uses the existing distillation
selection, batching, strict model-output validation and selected chat engine.
A linked engine outside loopback requires explicit `consent:true` before any
transcript is sent. It writes new candidate memory items, with `distilled`
provenance and the same scores/tag, dedupe and pending cap described above:
`decision` → `episodic`, `fact`/`preference` → `semantic`, `todo` → `working`.
The response includes only newly saved items, in model-result order; duplicates
and suggestions beyond the cap are omitted (`items:[]` when none are saved).
Turning off automatic collection does not disable explicit consolidation.
The legacy `/api/brain/distill` and `/api/brain/distilled*` routes and their
`distilled.json` review workflow remain available. Consolidation never writes
that legacy file. Errors use `{"ok":false,"error":"…"}`; a locked vault
returns 423, other invalid requests return 400.

## Context engine

Discussion preparation selects a deterministic memory pack without a model
call. Scope filtering precedes scoring: only `global`, the discussion's
`project:<id>` (when attached), and `agent:<runtime id>` are eligible. Only
`active` and `uncertain` items participate; uncertain items rank after active
items within their class. Machine items are excluded. An unbound discussion
can additionally select its own `task:<discussion_id>` working item tagged
`discussion-state`. Other working memory requires the matching project scope;
global and agent working items are not injected. The current continuity state
is pinned first, before the usual class order.

Selection visits classes in this order, under explicit token ceilings:

| Class | Default ceiling | Selection |
| --- | ---: | --- |
| Reflex | 300 | Always eligible, highest importance first |
| Working | 300 | Pinned continuity state, then matching project items by importance |
| Procedural | 300 | Relevant methods |
| Semantic | 500 | Relevant facts and preferences |
| Episodic | 300 | Two newest project session summaries, then relevant events |
| Session | 0 | Disabled by default |

The shared total ceiling is **1500 estimated tokens**, even though the class
ceilings sum to 1700. Unused class space does not raise another class's ceiling.
`brain.MemoryBudgets` and `brain.DefaultMemoryBudgets()` expose these defaults
for later configuration. Estimates use the existing one-token-per-four-Unicode-
characters heuristic. Rendered class/scope labels and the memory header count
against the ceilings. Pinned continuity state is truncated with an ellipsis
when necessary. Other items that do not fit are skipped, and a later smaller
item can still fit. Persisted memory text is never truncated.

Procedural, semantic and other episodic items need lexical overlap between the
latest user text and their text or tags; the two newest project session summaries
are eligible without overlap. Both passage and memory queries keep the last
2000 bytes of the trimmed user text. Matching uses Brain's existing English/
French stop words, case and accent folding. Procedural and semantic ranking
combines query-term overlap with importance, confidence and a small recency
bonus. Episodic ranking prefers the newest relevant event. Recency uses creation/
update timestamps relative to the newest eligible candidate, never the wall
clock or `last_used_at`, keeping unchanged previews stable.

A relevant eligible successor suppresses its retained predecessors, following
supersession chains across classes. Items whose normalized text is already
contained in retrieved Brain passages or a previously selected memory item are
skipped. The compact `Loom memory (why: class/scope):` block follows project
instructions/files/passages and precedes skills, Brain declarations and the
discussion's instructions. Non-project discussions still receive eligible
global and runtime-scoped memory. Unavailable or locked memory blocks preparation
rather than silently sending a different context.

`DiscussionContext.items` explains every included part of `system`, in order.
Each entry has `kind`, `label`, `source`, `reason` and estimated `tokens`; memory
entries also have `class` and `scope`, with their stable memory ID as `source`.
Kinds are `global_preferences`, `project` (continuity or instructions),
`project_files`, `brain_passage`, `memory`, `skill`, `primary_brain`,
`secondary_brains`, and `discussion_instructions`. Passages and secondary
Brain declarations each get their own entry. Headers, separators and rounding
are attributed to the following item, so item costs and `budget.by_kind` sum
exactly to `estimated_tokens`. Existing `brain_citations` remain available.

`DiscussionContext.budget.memory` contains `used`, `available` (1500 by default),
and a `classes` map with the same used/available pair per class. These memory
costs measure the memory block itself, including its header, independently of
separators between system sections. This permits displays such as
`1240 / 1500 memory tokens` without confusing memory and vault-passage budgets.
Explanations and budget metadata do not enter the context revision hash;
unchanged system text retains the existing route/history revision semantics.

Preview and preparation never call `Touch`. Both portable execution and native
local generation touch only included memory IDs when the accepted turn reaches
the runtime send boundary. Rejected/stale sends and idempotent retries do not
mark items used. Usage writes are best-effort bookkeeping and do not turn an
accepted send into a retry if storage becomes unavailable.

## Project continuity and retrieval scope

The project form deliberately stores only the project title, machine, workspace
and default executor/model. An empty workspace inherits that machine's saved
default. Every Loom discussion assigned to the project automatically belongs to
its conversation-memory scope; users do not select reference discussions or
maintain a parallel project synopsis.

Connected second brains, accepted distilled items and the project's discussion
history are searched for the current draft from the first turn onward. The
default context budget is 1500 estimated tokens, bounded by the existing 8000
token maximum. Native local preparation retrieves once. Project semantic
queries have a three-second bound and fall back to BM25 without generating a
response. Historical project continuity fields remain readable for compatibility
but are no longer exposed as required project setup.

HTTP search/read accepts optional `project_id`; pack and all MCP read-tool request
schemas also accept it. This checks the project's effective sources and current
project conversation paths, including known chunk IDs and native archive bindings.
MCP search/read/pack and JSON pack schemas accept `path_prefixes` as a map of source IDs to relative
path prefixes. Absent means unrestricted within the selected source; explicit
`[]` denies it. Prefixes match an exact path or subtree boundary. Project scope
intersects a caller's narrower conversation filter; it never expands it.
General API calls without project scope keep their existing operator-wide policy.

The prepared preview and sends share revision checks and text/message limits.
Runtime-private memory and tool state do not transfer.

`GET/POST /api/context/preferences` reads/saves `{page,external}` in Loom's existing
configuration store. Select an existing readable memory page of at most 4000
bytes; its text stays in the existing encrypted memory store. Empty selection
turns it off. Local discussions receive a selected page; cloud/harnesses receive
it only with `external:true` and the normal execution-sharing policy. A deleted,
unreadable or oversized selected page blocks context preparation until resolved.
Linked local engines retain the existing explicit engine destination policy.

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

Continuity is **off by default**: summaries cost model time locally and money
with a provider. Once enabled (ten-minute idle threshold by default), it is
frugal: with the local engine and `loaded_only` (default `true`), Loom uses the
model already loaded and skips with `local engine has no model loaded` rather
than loading one; an automatic run also waits until there are two new user
messages or 1,500 characters of new text. Run-now ignores that minimum. While the
Brain service runs, a cancellable scan runs at startup and once a minute, with
one summary operation at a time. It reads native display transcripts and
workspace discussion text, deduplicating native archives bound to discussions.
A discussion needs at least one new user message since its saved summary
checkpoint. Idle time follows the last turn's timing (native journal timestamps,
workspace turn timing, or saved time for older records). No summary starts while
any discussion turn is generating or being prepared. Results are discarded if
the discussion changes or generation starts during the model request.

Settings live in `LOOM_HOME/brain/continuity.json`, independently of candidate
collection. The empty provider uses the selected Loom chat engine and its
request model; a nonempty model overrides that default. A provider ID resolves
an existing connected Loom provider, its endpoint and credential, with the
provider's default model when model is empty. Credentials are never copied to
Brain settings or memory files. A cloud provider or non-loopback linked engine
requires stored `consent:true`; otherwise no text is sent and status records the
skip reason. Disabling continuity prevents both automatic and explicit runs.

Each call receives only the last 24 KiB of new user/assistant text, the previous
session summary and the current project/discussion state. These are marked as
untrusted data. Strict JSON validation allows one retry. Output is bounded to
8 KiB per summary/rendered state/fact and 32 entries per state array or facts
array. Values use the discussion's language. The two-minute operation timeout
and Brain lifecycle cancellation bound background requests.

A successful run writes active memory through the existing store: one episodic
`session-summary` per discussion in `project:<id>` or `global`, and one working
`project-state` per project. Unbound discussions instead get their own working
`discussion-state` in `task:<discussion_id>`. Changed text supersedes the previous
item, retaining history. Durable semantic/procedural/reflex facts become review
candidates through the existing dedupe and pending limits. Message-count
checkpoints live in Loom's `brain_continuity` store bucket and participate in
vault encryption. They advance only after successful memory writes.

Context selection pins the applicable working state first, counting labels and
header against the working and total budgets; oversized state is truncated with
an ellipsis. A project's two newest session summaries are preferred within its
episodic budget even without lexical overlap. Other memory keeps the existing
scope, relevance, supersession and dedupe rules. Selection remains deterministic
and does not alter persisted memory text.

All endpoints use normal Brain authentication, strict request decoding, vault
access checks and `Cache-Control: no-store`:

| Method / path | Input | Output |
| --- | --- | --- |
| `GET /api/brain/continuity` | — | `{"enabled":true,"idle_minutes":10,"provider_id":"","model":"","consent":false}` (current settings) |
| `POST /api/brain/continuity` | All five settings fields above; idle minutes 1–1440 | Same saved settings object |
| `POST /api/brain/continuity/run` | `{"discussion_id":"…"}` | `{"discussion_id":"…","at":0,"summary_id":"…","state_id":"…","skipped_reason":""}` |
| `GET /api/brain/continuity/status` | — | `{"running":false,"last_run":0,"last_error":"","recent":[{"discussion_id":"…","at":0,"summary_id":"…","state_id":"…","skipped_reason":""}]}` |

Run-now is synchronous and bypasses idle time, but still requires a new user turn,
enablement, no generation and destination consent. Skips return 200 with empty
item IDs and a reason; failures use the normal `{ok:false,error}` response.
Concurrent runs are rejected. Status retains the newest 20 observations in memory,
newest first; timestamps are Unix milliseconds (zero before any run). Settings
and summary checkpoints survive restart; the recent status list does not.

The model response schema is exactly:

```json
{"summary":"…","state":{"objective":"…","done":["…"],"next":["…"],"open":["…"]},"facts":[{"class":"semantic","text":"…"}]}
```

Fact class accepts `semantic`, `procedural` or `reflex`; arrays may be empty.
