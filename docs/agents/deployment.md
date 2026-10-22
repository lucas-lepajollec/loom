# Agents module deployment

## Requirements

- A Codeman server (1.x, `/api/v1`) that the **Loom server** can reach. It can
  run on the same machine (`http://127.0.0.1:3000`) or another host.
- The CLIs you want installed and signed in **on the Codeman host**. Loom never
  installs or logs into them.

## Connect

Open **Settings → Agents** (or the Agents page, which shows the same screen
until Codeman is connected).

- **Same machine as Loom:** Loom looks for Codeman the way Codeman's own tools
  do — its default port, its systemd user service (`codeman-web*.service` and
  drop-ins, including `--port`, `--https`, `CODEMAN_BASE_URL`), its macOS
  launchd agent, its data directory `.env` and Loom's environment. When the
  password is found there, **Connect** is a single click; otherwise Loom asks
  for it once. This works when Loom runs as the same OS user as Codeman.
- **Not installed:** the screen shows Codeman's official installer
  (`curl -fsSL https://getcodeman.com/install | bash`) and a search-again button.
- **Another machine:** "Codeman runs on another machine?" takes `host:port`,
  username and password.

Every connection is tested before anything is saved; a refused password or a
non-Codeman service is reported without changing the current setup. An `https`
Codeman on loopback may use its self-signed certificate; remote `https`
certificates are verified. "Advanced options" sets the official-interface
address used by "Open in Codeman" when the browser reaches Codeman at another
host. **Disconnect** forgets the address and password.

The configuration lives in `<LOOM_HOME>/codeman.json`; the password is sealed
under `<LOOM_HOME>/secrets/providers/`; **Disconnect** removes both.

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
