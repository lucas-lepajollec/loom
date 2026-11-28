# Session previews

Start the development server in a Loom terminal on the control-plane machine
or a saved SSH machine. In the session inspector, choose **Preview → + Web preview**
(or **Terminals → Application previews** for a separate browser tab). Select its
machine and HTTP port, then confirm opening the preview. An application listening
on `127.0.0.1:5173` on that machine becomes reachable through the preview's own
browser origin. It need not listen on the public network.

The proxy forwards root-relative assets, HTTP, redirects and WebSockets/HMR.
Applications that hard-code an absolute localhost URL or HMR client port must
configure those URLs for the preview origin. Only plain HTTP upstreams on
loopback ports 1024–65535 are supported; Loom's reserved local interface, Codeman and node
ports and existing preview listeners are rejected.

## Inside a session

The Preview tab lists files and loopback URLs mentioned in recent messages or
agent terminal output. Select a pill to inspect a file or confirm opening an
app. The launcher also offers local Linux listening ports and accepts a manual
machine and port on every platform. A listening port may belong to a service
that does not speak HTTP.

**Expand** keeps the current viewer mounted. **Escape** returns it to the panel.
Web and standalone HTML views have automatic sizing and four simulated widths
(1280, 768, 390 and 320 CSS pixels), with rotation. The browser bar keeps a
history of paths entered there. Navigation inside the isolated app belongs to
that app. **Open in new tab** creates a fresh ticket for the selected path.
The app may embed only within the exact Loom origin that opened its listener.

Select an image to inspect dimensions, size, transparency and zoom. Audio,
video and PDFs use the browser's native players and available codecs. Standalone
HTML runs in an opaque sandbox with external resources and inline scripts
blocked. Markdown and HTML support both rendered and source views.

The offline mesh viewer reads OBJ, STL, PLY and glTF 2.0/GLB geometry. It
supports binary and ASCII STL/PLY, triangulates polygons, and reads glTF
triangle lists, strips, fans and point clouds. glTF buffers may be embedded or
in readable sibling files. Scene node transforms are applied to static geometry;
materials, textures, animation, sparse accessors and compression extensions are
outside the geometry viewer. The viewer reports native-unit bounds and mesh
counts. Drag to orbit, Shift-drag to pan, and pinch or scroll to zoom. Arrow keys
rotate a focused canvas. The software fallback samples large meshes during
display. Geometry parsing runs in a worker and stops when switching files.

## Reachability

By default, each preview listens on Loom's bind address with an ephemeral TCP
port and uses the interface URL's hostname. The browser must be able to reach
that port on the **Loom host**. On a VM, allow the selected listener through the
VM/network firewall or configure a separate reverse proxy. Loom does not edit
firewalls, DNS or publish persistent tunnels when opening a preview.

SSH targets require the same authorized connection as other Loom machine
controls and server-side TCP forwarding (`AllowTcpForwarding` / `PermitOpen`).
Node-only connections require a saved SSH connection for live apps.
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
separate HttpOnly, SameSite cookie and redirects to the selected application path; the application
does not receive the link token. HTTPS cookies are Secure. Different ports on the same host are same-site.
For a cross-site HTTPS iframe, the ticket sets a partitioned cookie in
browsers that support partitioned storage. Subsequent requests
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

### Discovery and session suggestions

`GET /api/previews/ports?target=local` returns `{ok,ports:[5173,...]}`.
Discovery reads the Linux TCP listener tables. Other platforms and remote
targets return an empty list. The manual launcher remains available.

`GET /api/previews/session?session_id=ID` returns `{ok,previews:[...]}`.
Authenticated JSON `POST /api/previews/session` accepts `action` equal to
`add` (default) or `remove`, with the item fields below. Session, kind, target,
port and path identify an item. Adding it again updates its label. Removing
the last item releases the session entry. Suggestions live in memory, with up
to 48 per session and 512 sessions per server.

The MCP gateway at `/mcp/loom` accepts the same item fields through
`loom_preview_open`. A session-scoped caller can omit `session_id` and can
publish only to its own session. An unscoped caller supplies the session ID.
An orchestration SSE event named `preview` with `{session_id}` invalidates the
panel's list. Publishing a suggestion performs no file reads, listener creation
or command execution.

```json
{"kind":"port","session_id":"example","target":"local","port":5173,"path":"/gallery","label":"Gallery"}
```

```json
{"kind":"file","session_id":"example","path":"renders/scene.glb","label":"Scene"}
```

`target` defaults to `local`. File paths can be absolute or relative to the
session directory. A discussion without a working directory resolves relative
files in the configured files folder. Remote raw files require an existing
authorized files service on the selected machine.

### File streaming

`GET` or `HEAD /api/files/raw?scope=machine&dir=...&path=...` uses the same
authenticated file scope as the text reader. The optional directory narrows the
root for relative paths. Both the directory and final file are opened through
confined filesystem handles. Inline media types are allowlisted, HTML and SVG
receive sandbox headers and other files download as attachments. Regular files
up to 32 MiB support byte ranges. The text reader retains its 2 MiB limit.

Renderer extensions register an ID, pill icon, item predicate and asynchronous
view loader with `registerPreviewRenderer` in
[`renderers.js`](../internal/loom/ui/next/js/features/previews/renderers.js).
