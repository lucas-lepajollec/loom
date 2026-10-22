# Agents module deployment

## Requirements

- A Codeman server (1.x, `/api/v1`) that the **Loom server** can reach. It can
  run on the same machine (`http://127.0.0.1:3000`) or another host.
- The CLIs you want installed and signed in **on the Codeman host**. Loom never
  installs or logs into them.

## Configure

1. Install and start Codeman following its README (for example its systemd user
   service). Set `CODEMAN_PASSWORD` if Codeman listens beyond loopback.
2. In Loom, open **Agents → Connection** and enter the address as the Loom
   server reaches it, the username (default `admin`) and the password.
3. Optional: set the official interface address if the browser reaches
   Codeman at a different host than Loom (used only for "Open in Codeman").

The configuration lives in `<LOOM_HOME>/codeman.json`; the password is sealed
under `<LOOM_HOME>/secrets/providers/`. **Forget the password** removes it.

If Codeman sits behind a reverse proxy with its own hostname, add that host to
`CODEMAN_ALLOWED_HOSTS` on the Codeman side; Loom connects without an `Origin`
header, which Codeman accepts.

## Operate

- Codeman is updated, restarted or stopped independently. Loom shows
  "unreachable" and reconnects; sessions keep running in tmux.
- Version and update availability are shown in **Agents → Control**. Codeman's
  own self-update stays in its interface.
- Loom's **Settings → Permissions → Agent sessions (Codeman)** can require
  confirmation for, or deny, launches through Loom. It does not change what
  Codeman allows its CLIs to do; permission modes are Codeman settings.

## Security notes

- The browser never receives the Codeman password; all traffic goes through
  Loom's authenticated, origin-checked routes.
- Anyone who can use Loom can drive Codeman sessions through it. Protect Loom
  with its access password before exposing it to a network.
- Codeman's defaults apply to its CLIs (for example permission bypass for
  Claude unless Codeman is set otherwise). Review them in Codeman.
