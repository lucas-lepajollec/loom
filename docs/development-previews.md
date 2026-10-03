# Development application previews

Start the development server in a Loom terminal on the control-plane machine
or a saved SSH machine. In **Terminals → Application previews**, select its
machine and HTTP port, then confirm opening the preview. An application listening
on `127.0.0.1:5173` on that machine becomes reachable through the preview's own
browser origin. It need not listen on the public network.

The proxy forwards root-relative assets, HTTP, redirects and WebSockets/HMR.
Applications that hard-code an absolute localhost URL or HMR client port must
configure those URLs for the preview origin. Only plain HTTP upstreams on
loopback ports 1024–65535 are supported; Loom's reserved local interface/node
ports and existing preview listeners are rejected.

## Reachability

By default, each preview listens on Loom's bind address with an ephemeral TCP
port and uses the interface URL's hostname. The browser must be able to reach
that port on the **Loom host**. On a VM, allow the selected listener through the
VM/network firewall or configure a separate reverse proxy. Loom does not edit
firewalls, DNS or publish persistent tunnels when opening a preview.

SSH targets require the same authorized connection as other Loom machine
controls and server-side TCP forwarding (`AllowTcpForwarding` / `PermitOpen`).
Loom uses owned `ssh -W 127.0.0.1:<port>` streams; it does not expose an
unauthenticated local forwarding socket on the control plane.

When the interface uses HTTPS, configure a **separate HTTPS origin** for the
preview. An HTTP fallback is refused. For a single fixed reverse-proxy upstream,
set the Loom service environment, then restart Loom:

```sh
LOOM_PREVIEW_BIND=127.0.0.1:2513
LOOM_PREVIEW_ORIGIN=https://preview.example.test
```

The reverse proxy must preserve the public Host, support WebSocket upgrades and
forward to that listener. This fixed address permits one active preview at a
time; close it before opening another app. For direct listeners or a separately
provisioned port-aware reverse proxy, `LOOM_PREVIEW_ORIGIN` supports `{port}`, for
example `https://preview.example.test:{port}` with appropriate TLS listeners.
These variables only describe existing infrastructure; they do not provision
certificates or a reverse proxy. The public preview origin cannot equal Loom's.

## Sessions and lifecycle

Opening from Loom issues a single-use, one-minute bootstrap link. It becomes a
separate HttpOnly, SameSite cookie and redirects to the app root; the application
does not receive the link token. HTTPS cookies are Secure. Subsequent requests
require that preview session. Password/key changes, parent-session logout or
expiry, a locked data vault, closing the preview or stopping Loom revoke access.
Sessions and previews expire after eight hours. At most eight distinct previews
may exist; they are transient and are not restored after a restart.

The upstream never receives Loom's Authorization header or session cookie.
Application Set-Cookie values are namespaced and relayed back under their
original names only to that application. Apps that need to access those cookie
names from `document.cookie` may need configuration or a dedicated development
origin. This is a development preview, not a general application hosting system.
Cross-origin writes are rejected; legacy control actions require POST so loading
a URL cannot execute them. The control-plane interface cannot be
framed by an application preview.

**Close preview** stops its listener, owned SSH streams and upgraded WebSocket
connections. It leaves the development app and terminal running. Stop those
separately when finished. No third-party relay or hosted tunnel is required.

## API

`GET /api/previews` lists `{ok,previews:[{id,target,port,origin,expires}]}`.
Authenticated JSON `POST /api/previews` accepts:

- `{action:"open",target:"local",port:5173,consent:true}`: create/reuse a
  preview for that machine/port and return its one-time `url`.
- `{action:"ticket",id,consent:true}`: another one-time link for an existing
  preview. Do not persist or log these links.
- `{action:"close",id}`: stop only that preview's resources.

A remote `target` is a saved SSH machine ID. The protected bootstrap and app
routes run on the preview listener, not under `/api` or Loom's browser origin.
