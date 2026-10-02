# Interface access

Loom has one access password, shared by its owner across devices. Set it in
**Settings → Security and data → Interface access → Set password**. Use at least
12 characters. On another device, open the same Loom URL and enter that password.
The browser keeps a session for up to 30 days; no key needs to be copied to each
device. **Sign out** disconnects that browser. **Change password** requires the
current password, revokes other browser sessions and signs the current device in
again. The optional encrypted-data vault remains independent.

## Existing installations

An existing control key continues protecting access until a password is set.
If the browser already knows that key, it can open Settings and choose a password.
If it does not, the first migration screen requests the existing key once and
lets the operator choose the password. Subsequent sign-ins ask only for the
password. The old browser-storage key is removed; Loom never writes the new
password or session identifier to localStorage.

The existing control key remains accepted for automation and remote Loom engine
links. `/v1` retains its separate inference key. The human access password is
not accepted as a Bearer token and is not sent to engines or harnesses.

## Local setup and recovery

On the machine running Loom, run:

```sh
loom password
```

Enter and confirm the new password at the masked prompts. This also recovers
access when the old password is forgotten and revokes existing browser sessions.
No service restart is required. A password manager can pipe a secret into
`loom password --stdin`; never put it in command arguments, shell history or an
environment variable. Recovery does not change inference keys, the control key,
vault encryption or its recovery key. Local OS access is required.

## Transport and session protections

Use HTTPS for remote browser access. Direct LAN HTTP remains supported, but it
does not encrypt passwords or cookies in transit. Cookies are host-only,
`HttpOnly`, `SameSite=Strict`, and `Secure` for direct TLS. For a TLS-terminating
reverse proxy, preserve the original Host and set `LOOM_COOKIE_SECURE=1` in the
Loom service environment before restarting the interface. Only do this when
the browser actually uses HTTPS. Forwarded headers alone never enable secure
cookies or change the rate-limit identity.

Password records use salted Argon2id (version 1: 64 MiB, 3 iterations, one lane).
Only session-token hashes are stored. Sessions survive process restarts, expire
after 30 days and are bounded to 32 devices. Browser mutations and login/logout
use Go's same-origin protection in addition to cookie SameSite. Login/password
operations are limited to 10 attempts per peer per minute and 60 globally;
only two password-hashing operations can run concurrently. Unreadable or malformed
authentication data denies access.

Authentication is checked when each HTTP request starts. Revocation rejects
subsequent requests; it does not terminate an already accepted SSE or terminal
connection. This is a single-owner login, not a multi-user permissions system.

## HTTP endpoints

| Endpoint | Purpose |
| --- | --- |
| `GET /api/auth/status` | Public sign-in state; no credentials returned |
| `POST /api/auth/login` | `{password}` → browser session cookie |
| `POST /api/auth/logout` | Revokes the current cookie and clears it |
| `POST /api/auth/password` | Authenticated `{password,current_password}`; current password required after initial setup |

Authentication responses are `no-store`. Login, password setup and changes use
JSON bodies, never URL parameters. See the [OWASP password-storage guidance](https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html)
and [Go's origin protection](https://pkg.go.dev/net/http#CrossOriginProtection)
for the underlying mechanisms.
