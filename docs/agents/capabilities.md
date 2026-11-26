# Capabilities folder: skills and scheduled tasks

The **Connections** page (sidebar) and Agents › Tasks read a **capabilities folder** that you
keep yourself, usually under Git:

```
<folder>/
  skills/<name>/SKILL.md          Agent Skills: a method, its scripts and how to use a tool
  skills/<family>/<name>/SKILL.md related skills grouped in a family folder
  tasks/<name>.md                 a scheduled task, described by its front matter
```

A family folder has no `SKILL.md` of its own. Its skills are listed under the
family and linked as `<family>-<name>`, so CLIs that read only one level of
folders still find them.

The folder is the source of truth. Loom reads it and never edits it, so any
agent can use the same skills and tasks without Loom.

## Connections

Connections has its own sidebar entry, because skills reach every agent on the
machine, not only Codeman sessions. It reads like a plugins page: a search
field, All / Skills / MCP filters, then one card per MCP server and per skill.
A family is one card that lists its skills. A card opens a side panel with the
skill's instructions (its `SKILL.md`) and files, or with the MCP gateway's
address and token.

- **Agent access**: the [Loom MCP gateway](../agent-access.md) for agents that
  should reach your machines and sessions.
- **Capabilities folder**: choose it once, inside your home folder. If the Files folder already has
  `skills/` or `tasks/`, Loom suggests it.
- **Agents on this machine**: under the folder, one line says whether every
  skill is linked into the user skills folders read by agent CLIs, with a
  **Link** button when some are missing:
  - `~/.claude/skills` for Claude Code;
  - `~/.agents/skills` for Codex, Gemini CLI, OpenCode, Pi and other Agent Skills
    clients.

  Link creates one symbolic link per skill. It never replaces a real folder
  or a link that points elsewhere. It removes only links that point into the
  folder at a skill that no longer exists. CLIs load each skill's name and
  description at startup, and the full skill only when a task matches.
- The folder's skills also reach MCP clients through `list_skills` and
  `read_skill`, as a read-only source.

API: `GET /api/capabilities/skill?name=` (one listed skill: body and files), `GET/POST /api/capabilities/folder` (`{ path }`, empty to stop using it)
and `POST /api/capabilities/link` (`{ client: "claude" | "agents" }`, or empty
for every client). Both answer with the folder state: `dir`, `skills`, `tasks`,
`clients`.

## Tasks

A task file looks like this:

```yaml
---
name: nightly-report            # same as the file name
description: What the task does and what it produces.
schedule: "30 4 * * *"          # fixed minute and hour, optional weekdays
# every_minutes: 60             # or an interval instead of schedule
agent: shell                    # shell, or a Codeman mode: claude, codex, gemini…
run: bash scripts/report.sh     # shell only, run from the folder
enabled: false
---
Instructions for an agent task.
```

Agents › Tasks lists the folder's tasks above the Codeman jobs, with their
state: up to date, changed, not in Codeman yet, cannot be scheduled, or
file removed. **Apply** creates or updates the matching Codeman jobs:

- A `shell` task types `run` into a shell session, then `exit`. It spends no
  model tokens.
- An agent task sends a one-line prompt asking the agent to follow the task
  file. Editing the instructions therefore needs no new apply.
- The job keeps `loom-task:<file>` in its notes so Loom can find it again. A job
  whose file was removed can be deleted from the same list.

Codeman stores a fixed time on every day or on selected weekdays, or an interval.
Other cron expressions are reported as impossible to schedule.
