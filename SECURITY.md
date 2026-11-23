# Security policy

Loom is a local-first AI control station that manages inference engines, cloud providers, terminals, paired remote nodes, Codeman agent sessions and an MCP gateway (`/mcp/loom`), and exposes an OpenAI-compatible API. By default, it binds exclusively to the loopback interface (`127.0.0.1`). Reports concerning unauthorized remote execution, unintended process elevation, LAN or proxy authorization bypasses, SSRF, or path traversal are treated with highest priority.

## Supported versions

| Version | Support status |
| --- | --- |
| Latest published release and `main` | Supported |
| Older releases | Best effort; upgrading may be required (no independent backports) |
| Third-party forks or unofficial builds | Not supported by the Loom maintainers |

Source fixes are not automatically present in an installed binary; see [CHANGELOG.md](CHANGELOG.md) for release status. Build with the minimum patched Go version declared in `go.mod` or a newer supported toolchain. The [control plane audit](docs/control-plane-audit.md) records dated findings, remediation and remaining boundaries.

## Reporting a vulnerability

Use the repository's [private vulnerability reporting form](https://github.com/lucas-lepajollec/loom/security/advisories/new) when available.

If the form is unavailable, ask the maintainer for a private reporting channel through the [portfolio contact page](https://lucaslepajollec.com/#contact). Do not include exploit code, credentials, tokens, private data or sensitive configuration in that initial message, and do not report vulnerabilities in public issues.

Please include, when possible:

- the affected Loom version or commit;
- the operating system, deployment method and relevant environment details;
- clear reproduction steps and the security impact;
- sanitized logs, requests or a minimal proof of concept;
- any known workaround.

You should receive an acknowledgement within seven days and an initial assessment within fourteen days. Remediation and disclosure timing depend on severity, complexity and release coordination. Please allow a reasonable remediation window before publishing details.

## Operational security and deployment guidance

- **Loopback by default, TLS for remote access**: Do not expose the Loom web dashboard or API endpoints directly to the public internet without a TLS reverse proxy and authentication.
- **Authentication & separate credentials**: Set an access password (`loom password`) for browser sign-in. Network access requires a password or an automation control key (`loom set-web-key`, minimum 24 characters); automation control keys, `/v1` inference keys and `/mcp/loom` agent tokens remain separate. See [Interface access](docs/access.md) for migration, session protection and local password recovery.
- **Reverse proxies (`LOOM_TRUST_PROXY` and `LOOM_COOKIE_SECURE`)**: Forwarded headers (`X-Forwarded-For`, `X-Forwarded-Proto`) are ignored by default. Set `LOOM_TRUST_PROXY=1` (or `true`) only when Loom runs behind a trusted reverse proxy on loopback that strips or overwrites client-supplied forwarding headers; when enabled on loopback peers, Loom honors `X-Forwarded-For` for login/Bearer rate-limit buckets and `X-Forwarded-Proto: https` for `Secure` session cookies. You can also set `LOOM_COOKIE_SECURE=1` to force `Secure` cookies unconditionally. Anonymous local access (`unprotectedLocalRequest`) never trusts forwarded headers.
- **Codeman & `/mcp/loom` agent access**: Loom relays an independent Codeman server through `/api/codeman/*` using server-side credentials. External MCP agents authenticate to `/mcp/loom` with a dedicated Bearer token that can be rotated or revoked from Settings › Security › Agent Access (`POST /api/mcp/gateway/token` with `{"revoke": true}` or `DELETE /api/mcp/gateway/token`). High-impact MCP tools (such as `loom_machine_run`, `agents__start_session`, and write tools) default to requiring confirmation unless explicitly allowed by a capability policy rule; read-only discovery tools remain allowed by default.
- **Encrypted credential stores**: Remembered cloud provider API keys use the OS keychain where available, otherwise private AES-256-GCM sealed files under `LOOM_HOME/secrets/providers`. The installation-local key at `LOOM_HOME/secrets/providers/.key` also seals remote node credentials (`engine_node`, paired machines), internal engine capabilities, legacy `/v1` inference keys, self-signed HTTPS keys and notification secrets at rest. Back up `LOOM_HOME/secrets/providers/.key` and `loom.db` together, privately. Local encryption mitigates accidental plaintext exposure and is separate from the optional memory vault password; it does not protect against a compromised OS account. The optional vault does not encrypt external project files. Terminals, Codeman sessions and MCP tools execute with their own permissions on the selected machine.
- **Keep secrets out of reports**: Keep API keys, tokens, cookies and local configuration out of commits, screenshots and public issue reports. Back up the resolved `LOOM_HOME` data directory before changing installation or storage settings.

Interface updates require authenticated owner access and use official GitHub release binaries with checksum verification. The optional Linux system updater accepts no command arguments or arbitrary destinations; it validates root-owned installation paths before privileged execution. See [Updating Loom](docs/updates.md) for setup, trust and rollback boundaries.
