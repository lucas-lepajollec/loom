# MCP configuration files

Loom owns `mcp.json` in the resolved `LOOM_HOME` directory on Linux, macOS and
Windows. It uses the common named-server format:

```json
{
  "mcpServers": {
    "filesystem": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/your/folder"],
      "env": {"TOKEN": ""},
      "enabled": false,
      "disabledTools": ["write_file"]
    },
    "remote": {
      "type": "http",
      "url": "https://example.com/mcp",
      "headers": {"Authorization": ""},
      "exposure": "on_demand",
      "enabled": false
    }
  }
}
```

`command`/`args`/`env` configure stdio. `url`/`headers` configure Streamable HTTP,
or legacy HTTP/SSE when `type` is `sse`. Loom's per-entry fields include
`enabled` (defaults to `true` when absent), `disabledTools` (upstream tool names),
`exposure` and an optional `description`.
Unknown top-level and entry keys survive Loom edits. Writes use a temporary file,
flush and rename, with mode `0600`; on Windows Unix permission bits are not an ACL,
so keep `LOOM_HOME` protected by the account's filesystem permissions.
Environment variables and headers can contain secrets: keep this file private.

The first load imports the former database `state/mcp` value only when the file
does not exist. The original database value remains a recovery backup; a migration
marker is saved only after a successful file write or validation of an existing
file. It is never used as active configuration again. A failed write leaves the
backup available for retry. An existing user file takes precedence, including an
invalid file, which is never replaced by the migration.

Loom checks modification time, size and file identity when configuration is read.
Editor changes apply on the next read without restarting; changed or deleted
servers invalidate their pooled sessions. Invalid JSON or configuration keeps the
last good configuration in use and blocks Loom writes until repaired. Before the
first valid load, an invalid file returns an error. No process is launched by the
file-metadata or linked-source APIs.

## Connections and tool exposure

Connections lets you add, edit, test, disable and delete external MCP servers,
and toggle individual tools. You can fill the form manually, paste an
`mcpServers` or VS Code `servers` JSON snippet, import a linked file, or search
the [official MCP Registry](https://registry.modelcontextprotocol.io/). Registry
presets fill the manual form for review. Saving an enabled server starts its
connection. Registry queries send the search terms to the official registry,
and results are cached for five minutes.

- `on_demand` exposes three discovery tools in place of the server's full tool
  schemas. `direct` injects the server's enabled tools with their existing names.
- New entries created in Connections, imported from linked files or registered
  by an agent default to on demand. A missing `exposure` inherits the gateway
  setting. Legacy entries start in direct mode until the owner enables
  **Token-saver mode (on demand)** in Agent Access.
- Connections estimates token usage at four JSON bytes per token, with
  approximately 350 tokens for the discovery tools. The estimate covers
  external tool definitions, with actual savings varying by tokenizer and tool
  catalog. An upstream still connects to discover its available tools.

## Linked sources

Linking stores only `{path,label}` in the database and leaves the source file
read-only. Listings reread it and support:

- Claude Code `~/.claude.json`: top-level `mcpServers` and
  `projects.<directory>.mcpServers`.
- Standard `.mcp.json` and Cursor `~/.cursor/mcp.json`: `mcpServers`.
- VS Code `.vscode/mcp.json`: `servers`.

Suggestions include existing Claude Code and Cursor files in the OS user's home,
and `.mcp.json` / `.vscode/mcp.json` in Loom's current working directory, excluding
files already linked. Suggestions check existence only; linking is explicit.
`~` paths are expanded using the OS user home; linked paths are stored as absolute
paths with existing symlinks resolved. Source reads are limited to 4 MiB.

Each listed server contains `source`, `label`, `name`, `transport`, `env_names`,
`header_names`, `read_only:true` and, for Claude projects, `project`. Neither
credential values nor command arguments/URLs are returned by this listing.
Project server selectors use `<absolute path>#project=<URL-encoded directory>`;
use the returned `source` unchanged to distinguish repeated server names.

Adoption copies a server into Loom's file with `enabled:false`. By default all
environment variable names become empty placeholders. Only explicit
`with_env:true` copies environment values. Header names always become empty
placeholders; this option does not authorize copying header credentials.
Source files are never written. Normalized-name collisions return HTTP 409 rather
than replacing an existing Loom server. Unknown source-entry extensions are not
adopted; Loom preserves them when editing its own file.

## HTTP API

All endpoints use the existing authenticated `/api/*` boundary. New endpoints
return `Cache-Control: no-store`; POST requires `application/json`, strict JSON
and the existing 128 KiB request limit.

| Request | Payload / response |
| --- | --- |
| `GET /api/mcp/file` | `{path,mtime,error?}`; UTC RFC 3339 `mtime` or null, sanitized validation error if any |
| `GET /api/mcp/sources` | `{ok,sources:[{path,label,servers,error?}],suggested:[{path,label}]}` |
| `POST /api/mcp/sources` | `{path,label?,action:"link"}` (default action); `{path,action:"unlink"}`; returns the updated listing |
| `POST /api/mcp/sources/adopt` | `{source,name,with_env?}`; returns `{ok,name,env_to_fill}` without credential values |
| `GET /api/mcp/registry/search?q=<query>&limit=<n>` | `{ok,servers:[{id,title,description,version,website_url,repository_url,suggested_name,presets}]}` with a limit of 1–25 |
| `GET /api/mcp/gateway/mode` | `{ok,tool_mode}` |
| `POST /api/mcp/gateway/mode` | Send `{tool_mode:"smart"\|"direct"}` to set the default for servers with no explicit exposure |

`GET /api/mcp` returns the owned-server status and configuration fields, the
default `tool_mode` and `token_budget` (`direct_tokens`, `injected_tokens`,
`saved_tokens`, `saved_percent`, `on_demand_tools`, `direct_tools`). You can
update just `exposure` with `POST /api/mcp/save` and
`{name,exposure,exposure_only:true}`. An empty exposure inherits the default. Use
`create_only:true` on creation to reject a conflicting name. `POST /api/mcp/test`
reconnects an enabled server and includes `latency_ms`. The existing delete,
toggle and per-tool endpoints keep their request formats.
