# Agent access

Agents such as Claude Code or Codex can use Loom through its authenticated MCP
gateway at `/mcp/loom` (Streamable HTTP). An agent with the gateway token can
do what the person does in Loom: see the machines, run commands on them, use
the second brain and, with Codeman, start and drive other agent sessions.

## Connecting an agent

Settings › Security › Agent access shows the address and creates the token. The
token is shown once, with ready-to-copy setups:

- Claude Code: `claude mcp add --transport http loom <address> --header "Authorization: Bearer <token>"`
- other clients: an `.mcp.json` entry of type `http` with the same header.

Replacing the token cuts off every agent that holds the old one. Only the token's
hash is stored.

## Tools

| Tool | Effect |
| --- | --- |
| `loom_overview` | Loom version, machines, which machine provides each brick, whether agent sessions are available, and a short guide. Start here. |
| `loom_machines` | This machine (`local`), paired nodes and saved SSH machines, with modules and whether a command can run there. |
| `loom_machine_run` | Runs one non-interactive command through Loom's terminals (local PTY, SSH or a node's terminal module) and returns the exit code and the last 64 KiB of plain-text output. Stopped after `timeout_seconds` (default 60, at most 600). The terminal is removed afterwards. |
| `agents__*` | Codeman's own MCP server (`codeman mcp`): `list_sessions`, `start_session`, `send_prompt`, `wait_for_session`, `read_output`, `interrupt_session`, `delete_session`. |
| `brain_*`, `memory_*`, skills, discussions | The second brain tools, unchanged. |
| `<server>__<tool>` | The person's own MCP servers from `mcp.json`, unchanged. |

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

Every call goes through the gateway's policy check with the subject
`mcp.tool:<server>/<tool>`: `mcp.tool:loom/loom_machine_run`,
`mcp.tool:agents/start_session`, and so on. The default is allow, as for every
gateway tool. A `confirm` rule turns the call into a request that waits for the
person (up to 24 hours); a `deny` rule refuses it. `loom_machine_run` also
keeps the `node.terminal` check of every terminal. The `agents__*` tools talk to
Codeman directly, so `mcp.tool:agents/<tool>` is their Loom-side check.

A gateway token carries no discussion identity, so only global and
local-machine rules apply (see [policy](policy.md)).
