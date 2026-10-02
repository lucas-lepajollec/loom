# Terminals

Loom opens real shells on this machine or a saved SSH machine, optionally in a
chosen folder and with a command to run. Open them from Terminaux, a project,
a harness discussion or a machine. A terminal survives closing its browser tab
and replays up to 256 KiB of recent output when reattached. It ends when its shell
exits, when explicitly closed or when Loom exits; sessions do not survive a Loom
restart. At most 16 terminals may run at once. Native harness-session resume is
still a separate roadmap item.

## Platforms and shells

- Linux/macOS: a native PTY starts `$SHELL` (or `/bin/sh`) with `-l`, or `-lc`
  for a supplied command. Existing SSH targets use the same POSIX shell behavior.
- Windows 10 version 1809 and later, including Windows 11: native ConPTY with
  input/output pipes and resizing. Loom checks the ConPTY exports in kernel32;
  older systems report terminal support as unavailable with a French error.
  The shell is `pwsh.exe` when found on PATH, then `powershell.exe`, then
  `%ComSpec%` (or `cmd.exe` if unset). Commands use that shell's syntax.
  Arguments use native Windows quoting; cmd's `/S /C` command is preserved as
  shell syntax. The inherited Unicode environment includes terminal overrides
  (`TERM=xterm-256color`, `COLORTERM=truecolor`). A job with kill-on-close owns
  the shell and its descendants, assigned before the shell begins execution.
- Connected Windows OpenSSH servers: SSH forces a remote PTY (`-tt`) and starts
  Windows PowerShell interactively. The remote OS comes from the saved machine
  description. A UTF-16LE encoded PowerShell script sets the folder with
  `Set-Location -LiteralPath`, preserving spaces, apostrophes, Unicode and
  wildcard characters. An empty command leaves PowerShell open; a supplied
  command runs as PowerShell code and returns its exit status. Folder paths
  must be absolute, for example `C:\Users\alice\project`. SSH must be installed
  locally and Loom's key or an existing user key must be authorized remotely.

The native lifecycle follows Microsoft's
[ConPTY session documentation](https://learn.microsoft.com/en-us/windows/console/creating-a-pseudoconsole-session),
including draining output while closing a naturally completed console.

## Existing API

The control key protects terminal management. WebSockets use a single-use
30-second ticket and same-origin checks rather than putting the key in the URL.

| Route | Behavior |
| --- | --- |
| `GET /api/terminals` | Lists terminal snapshots and the host's `supported` capability. |
| `POST /api/terminals` | Opens `{target,dir,command,title}`; `target` is `local` or a saved machine ID. |
| `POST /api/terminals/ticket` | `{id}` returns the one-time attachment ticket. |
| `GET /api/terminals/ws?ticket=…` | Binary output, keystroke input and text `{"resize":[cols,rows]}` messages. |
| `POST /api/terminals/close` | `{id}` closes the owned session and removes its snapshot. |

## Validation

Portable tests cover Windows shell selection, shell argv/command composition,
Unicode environment blocks and encoded interactive Windows SSH commands. Native
ConPTY tests are Windows-only and cover output/exit codes, interactive input,
resize validation, repeated close without a reader, and descendant termination.
Run `go test -short ./internal/loom/ -run Term` on each target OS; cross-building
and Windows `go vet` check compilation but do not execute ConPTY on Linux.
