# Workspaces and native harness access

## Saved folders

Settings → Workspaces saves named directories, with one default for each
execution machine. First-run setup offers a **Loom** directory:
`LOOM_HOME/workspaces/loom` on the control plane, or `~/loom` on a saved SSH
machine whose home directory is known. Listing that suggestion creates nothing;
the managed directory is created on first use or explicit confirmation.

Choosing a harness for a new discussion automatically uses its machine's
default. A project's folder on that machine takes precedence. Existing
discussions retain their folder when the default changes. Changing execution
machines selects a folder on the destination rather than reusing a path from
the previous machine. Custom SSH launchers without an associated saved machine
require a manually supplied remote path.

The discussion's Session panel selects a saved workspace or a new absolute
directory. A new directory can be used once, saved by name, made the default,
and explicitly created if missing. Paths belong to the **execution machine**,
not the browser device. Remote validation uses SSH; Windows OpenSSH targets use
PowerShell. Removing a workspace removes its definition, never the directory
or its files. Project folder associations and existing discussion paths remain.

## Native protections and approvals

The Session panel distinguishes native filesystem protections from Loom's
approval policy. Loom does not implement an operating-system sandbox around an
agent. Its confined delegated ACP file API does not constrain native shell
commands, native tools, MCP servers or other processes.

| Protection | Support |
| --- | --- |
| Keep native settings | Every harness; available native modes/settings remain authoritative. |
| Native workspace write protection | Known Codex ACP launchers use the advertised `workspace-write` mode. Reads outside the workspace remain possible; upstream writable temporary directories/additional roots still apply. Escapes require native approval, which Loom does not automatically grant in this mode. |
| Full filesystem access | Known Codex ACP launchers use the advertised `agent-full-access` mode, after confirmation. OS permissions still apply. |
| Confine both reads and writes to the workspace | Unavailable unless a future adapter can prove native support; currently disabled. |

Managed modes are reapplied before each turn, including restored/live native
sessions. An adapter that does not advertise the required mode fails before
prompt execution. Native commands may change their own session settings;
reported mode updates remain visible. Unknown/custom launchers are not assumed
to implement Codex protections merely because of their name.

For other harnesses, use the modes/configuration actually advertised by their
ACP session. Loom's **Ask**, **Auto edits**, and **All** options only answer
permission requests the native harness sends to Loom. They cannot add a
permission request or sandbox that the harness does not implement. Network
access is likewise governed by the native runtime. See the
[Codex security documentation](https://developers.openai.com/codex/security/)
and [Codex ACP adapter](https://github.com/agentclientprotocol/codex-acp).

## Installed, connected, signed in

Installation makes the CLI available. **Connect** explicitly reads its native
catalog and adds it to Loom's discussion selector. The Harnesses page groups
connected adapters above the others. Disconnecting hides new selector choices;
it does not uninstall, delete existing discussions or sign out of the account.
Previously used harnesses and explicitly added custom/machine adapters retain
their connection when upgrading.

An ACP authentication-method list is not proof that an account is signed in.
**Native account** opens a terminal on the harness's machine; OAuth, keys and
credential storage remain with its own CLI. Complete the native flow, then
refresh/reconnect its catalog in Loom. No Loom-wide harness password is created.

| Harness | Native account flow opened by Loom |
| --- | --- |
| Claude Code | `claude auth login` |
| Codex | `codex login --device-auth` |
| OpenCode | `opencode auth login` |
| Gemini CLI | `gemini`, then its authentication selector |
| Pi | `pi`, then `/login` |
| Hermes | `hermes setup model`, then choose the provider/account |
| Antigravity | `agy`, then its native sign-in flow |

The terminal may print a browser URL or device code. Follow the harness's
instructions; some flows require a browser on that machine or additional native
headless setup. Account login for arbitrary custom launchers is explicitly
unsupported: use their own documented CLI. Nothing copies native credential
files into Loom's database, public source or browser storage.

Gemini CLI and Antigravity are separate executors with separate native setup,
tools and sessions. A Gemini model is a model family, not a harness identity.
See [Gemini CLI](https://geminicli.com/docs/),
[Antigravity CLI](https://www.antigravity.google/docs/cli/install/),
[Pi](https://github.com/badlogic/pi-mono/tree/main/packages/coding-agent), and
[Hermes commands](https://hermes-agent.nousresearch.com/docs/reference/cli-commands/).

## Workspace API

Routes use the normal authenticated control-plane API and JSON POST rules.

| Route | Input / behavior |
| --- | --- |
| `GET /api/workspaces?target=local` | `{ok,workspaces:[{id,name,target,path,default,managed}]}`; target may instead be a saved machine ID. |
| `POST /api/workspaces?target=local` | `{action:"save",workspace:{id?,name,path,target?,default?,managed?},create?:false}`; returns the saved `workspace` plus current `workspaces`. |
| Same POST | `{action:"default",id}` or `{action:"remove",id}`; never deletes files. |
| `POST /api/runtime/sessions/configure` | Existing discussion configuration accepts `workspace_id` or explicit `workdir`, and `filesystem_policy` (`native`, `workspace-write`, `full-access` where supported); full access requires the existing explicit consent field. |
| `POST /api/runtimes/{id}/disconnect` | Empty JSON object; forgets Loom connection only. |
| `GET /api/runtimes/{id}/login` | Returns a terminal plan (`target`, `command`, `title`, `note`); does not execute login. |
