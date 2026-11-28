# Agent access

Agents such as Claude Code or Codex can use Loom through its authenticated MCP
gateway at `/mcp/loom` (Streamable HTTP). An agent with the gateway token can
do what the person does in Loom: see the machines, run commands on them, use
the second brain and, with Codeman, start and drive other agent sessions.

## Connecting an agent

Settings › Security › Agent access shows the address and creates the token. The
token is shown once, with a ready-to-copy setup for each client:

- `loom mcp-bridge` (recommended, multi-CLI): `{"mcpServers":{"loom":{"command":"loom","args":["mcp-bridge"]}}}`.
  Automatically discovers the per-session scoped MCP token Loom writes (owner-only
  `0600`) for `$CODEMAN_SESSION_ID`, or falls back to `LOOM_MCP_URL` / `LOOM_MCP_TOKEN`
  (`LOOM_GATEWAY_URL` / `LOOM_GATEWAY_TOKEN`).
- Claude Code: `claude mcp add --scope user --transport http loom <address> --header "Authorization: Bearer <token>"`.
  The user scope gives Loom to every Claude Code session on that machine,
  including the sessions Codeman starts; sessions already open get it when
  they restart.
- Codex: a `[mcp_servers.loom]` entry for `~/.codex/config.toml` with `url` and
  `http_headers = { "Authorization" = "Bearer <token>" }`.
- Other clients: an `.mcp.json` entry of type `http` with the same header.

Run the setup on each machine where agents run. When they connect, the gateway
sends them a short guide (MCP server instructions), so they know to start with
`loom_overview` without being told.

The panel shows the last agent that used the token ("Claude Code, 2 minutes
ago"). Replacing the token cuts off every agent that holds the old one and
resets that line. Only the token's hash is stored.

## Tools

| Tool | Effect |
| --- | --- |
| `loom_overview` | Loom version, machines, which machine provides each brick, whether agent sessions are available, and a short guide. Start here. |
| `loom_machines` | This machine (`local`), paired nodes and saved SSH machines, with modules and terminal availability. Paired nodes include the current saved `node_url` when available. `loom_overview` includes the same machine list. |
| `loom_machine_run` | Runs one non-interactive command, on a machine given by id, name or host (default: this machine), through Loom's terminals (local PTY, SSH or a node's terminal module) and returns the exit code and the last 64 KiB of plain-text output. Stopped after `timeout_seconds` (default 60, at most 600). The terminal is removed afterwards. |
| `loom_subagent_catalog`, `loom_subagent_spawn`, `loom_subagent_list`, `loom_subagent_send`, `loom_subagent_wait`, `loom_subagent_read`, `loom_subagent_stop` | Dynamically discover available Codeman CLIs/models/effort/permissions and orchestrate tracked Loom sub-sessions with automatic scoped MCP access, optional Git worktree isolation (`.loom-worktrees/<slug>` + `.worktreeinclude`), full-conversation reads (`mode='full'`), approval bubbling (`signal='blocked'`), depth/concurrency caps, and strict same-tree isolation. |
| `loom_board_post`, `loom_board_read` | Post and read concise updates, questions, handoffs and results on the shared coordination board of the current orchestration tree. |
| `agents__*` | Codeman's own MCP server (`codeman mcp`): `list_sessions`, `start_session`, `send_prompt`, `wait_for_session`, `read_output`, `interrupt_session`, `delete_session`. |
| `brain_*`, `memory_*`, skills, discussions | The second brain tools, unchanged. |
| `<server>__<tool>` | Tools from external MCP servers exposed directly. |
| `loom_mcp_search`, `loom_mcp_describe`, `loom_mcp_call` | Find, inspect and call tools from on-demand external MCP servers. |
| `loom_mcp_execute` | Run a bounded pipeline against enabled external tools and return selected results. |
| `loom_mcp_list` | List external servers, tool counts, connection status, discovery latency and credential key names. |
| `loom_mcp_register` | Propose creating, updating or deleting a server, toggling it or changing its exposure. Every change requires owner approval. |

For on-demand servers, the instructions list server names and tool counts.
Search by keyword or use `query:"*"` with a `server` filter. Describe a result
to get its full schema, then pass its exact server name, tool name and
`arguments` object to `loom_mcp_call`. Local and Cloud discussions use the
same discovery and execution tools when on-demand MCP is active. A single call
accepts `fields` (dot paths) and `max_chars` (default 12,000, maximum 65,536).
Small native responses keep their MCP content blocks. See [MCP configuration](mcp-files.md).

### Code Mode

Pass ordered steps to `loom_mcp_execute`. Only the last result reaches the
model. For example, this Home Assistant pipeline returns names of active
entities from a server whose tool returns a JSON array.

```json
{
  "steps": [
    {"op":"call","id":"states","server":"home","tool":"get_states","args":{}},
    {"op":"filter","id":"active","from":"states","where":{"field":"state","op":"eq","value":"on"}},
    {"op":"return","from":"active","fields":["entity_id","attributes.friendly_name"]}
  ]
}
```

- `call` passes `args` to one enabled tool. A complete `{{step_id.path}}`
  reference retains its JSON type. Scalar references can also appear in strings.
- `parallel` takes a `calls` array of `{server,tool,args}` objects and returns
  results in input order, with up to eight calls in flight.
- `filter` supports `eq`, `neq`, `contains`, `gt`, `lt` and `exists`.
  `map` and `pick` project nested fields from an array of objects or one object.
- `foreach` takes `from` and one `call` with `{{item.path}}` references.
  An explicit `limit` selects a prefix. The maximum is 25 items.
- `return` selects `from` and optional fields. Omit `from` to select fields
  across named steps. Dot paths can include zero-based array indices.

Each pipeline has a 60-second deadline, 30 upstream calls, 256 KiB of
intermediate JSON and 16 KiB of final output. Truncated output carries
`trimmed`, `total_bytes` and a UTF-8 preview. Each call checks the real
upstream policy subject and current enabled state. An error stops the pipeline.
Calls that completed before an error may already have changed upstream state.

### Managing external servers

Use `loom_mcp_list` to identify the server, then propose a change with
`loom_mcp_register`. The `action` field accepts `create` (default), `update`,
`toggle`, `set_exposure` and `delete`. For example,
`{"action":"update","name":"home","disabled_tools":["unlock_door"]}` replaces
the hidden tool list. Omitted configuration fields retain their saved values.
Supply new environment variable names in `env_names` and fill their values in
Connections. A create requires a new name. If the server changes while an
approval is pending, Loom asks the caller to submit a fresh proposal.

### Agent sessions through Codeman

Loom does not drive agent CLIs itself. When Codeman is connected in Loom and the
`codeman` command is installed on the machine serving Loom, Loom starts
`codeman mcp --url <Codeman address>` and exposes its tools as `agents__*`. The
Codeman credential is passed in that process's environment only; it is never
written to a file or an argument. Codeman's rules apply unchanged: an agent can
prompt, interrupt or delete only the sessions it started; listing, reading and
waiting are open. Upgrading Codeman upgrades these tools.

`loom_gateway_status` reports the `agents` upstream with the person's MCP
servers. Without a saved Codeman connection or without the `codeman` command,
the `agents__*` tools are absent and `loom_overview` says why.

## Permissions

External tool calls, including `loom_mcp_call`, go through the policy check with the subject
`mcp.tool:<server>/<tool>`: `mcp.tool:loom/loom_machine_run`,
`mcp.tool:agents/start_session`, and so on. A `deny` rule refuses the call.
A `confirm` rule makes the call wait for the person, up to 24 hours.
Registration uses `mcp.manage` and requires an explicit approval for every
proposal. A deny rule can block management altogether.

"Ask me before acting" in the Agent access panel adds three global confirm rules,
`agent-access-ask-run` (`loom_machine_run`), `agent-access-ask-session`
(`agents__start_session`) and `agent-access-ask-subagent` (`loom_subagent_spawn`);
they show and can be edited in Settings › Permissions. Reading, searching and
following an agent's own sessions stay free.

A waiting call appears in Loom's activity menu (the bell) and in the phone's
inbox, with Allow and Deny. It names the machine and command, or the call's
arguments; that detail is kept in memory only, never stored, audited or sent
in a notification. A push notification, when enabled, offers the same answers.
A denied call returns an error to the agent. `GET /api/policy/pending` lists
the waiting confirmations; the answer is
`POST /api/workspace/sessions/<session_id>/requests/<request_id>` with
`{"decision":"allow_once"}` or `{"decision":"deny"}`.

`loom_machine_run` also keeps the `node.terminal` check of every terminal. The
`agents__*` tools talk to Codeman directly, so `mcp.tool:agents/<tool>` is their
Loom-side check.

A gateway token carries no discussion identity, so only global and
local-machine rules apply (see [policy](policy.md)).
