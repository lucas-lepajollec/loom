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
      "enabled": false
    }
  }
}
```

`command`/`args`/`env` configure stdio; `url`/`headers` configure Streamable HTTP.
`type` is optional metadata retained on writes; it does not add a new transport
implementation. Loom's per-entry fields are `enabled` (defaults to `true` when
absent) and `disabledTools` (real tool names, defaults to none).
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

Existing `/api/mcp`, `/api/mcp/save`, `/api/mcp/delete`, `/api/mcp/toggle`,
`/api/mcp/tool`, `/api/mcp/test` and harness adoption remain available. The existing
owned-server editing API retains its configuration fields; the linked-source
listing is a separate metadata-only view. Harness adoption still omits environment
values and starts disabled.
