# Security policy

Loom is a local-first AI workspace that controls inference engines, cloud connections, coding-agent harnesses and terminals, and exposes an OpenAI-compatible API. By default, it binds exclusively to the loopback interface (`127.0.0.1`). Reports concerning unauthorized remote execution, unintended process elevation, LAN authorization bypasses, or path traversal in model/preset management are treated with highest priority.

## Supported versions

Security fixes target `main` and the next update of the latest published release.
Older release lines do not receive independent backports. Source fixes are not
automatically present in an installed binary; see [CHANGELOG.md](CHANGELOG.md)
for release status. Build with the minimum patched Go version declared in
`go.mod` or a newer supported toolchain. The [control plane audit](docs/control-plane-audit.md)
records dated findings, remediation and remaining boundaries.

## Reporting a vulnerability

Use the [private vulnerability reporting form](https://github.com/lucas-lepajollec/loom/security/advisories/new) when available.

If the form is unavailable, ask the maintainer for a private reporting channel through the [portfolio contact page](https://lucaslepajollec.com/#contact). Do not include exploit code, credentials, private data or sensitive configuration in that initial message, and do not report vulnerabilities in public issues.

Once a private reporting channel is agreed, include the affected version or commit, reproduction steps, impact and a sanitized proof of concept when possible. Response times are not guaranteed.

## Local deployment guidance

- Do not expose the Loom web dashboard or API endpoints directly to the public internet without a secure TLS reverse proxy and authentication.
- Set an access password for browser sign-in. Network access requires a password or an existing control key; automation control keys and `/v1` inference keys remain separate. Use TLS for remote access and see [Interface access](docs/access.md) for migration, session protection and local password recovery.
- Keep the optional encrypted vault and credential recovery requirements in mind when backing up. Explicitly remembered cloud API keys use the OS keychain where available, otherwise private encrypted files under `LOOM_HOME/secrets/providers`. Back up its `.key` and sealed files together, privately. Local encryption does not protect against a compromised OS account and is separate from the optional vault password. The vault does not encrypt external project files or native harness stores. Harnesses, terminals and MCP tools execute with their own permissions on the selected machine.
- Keep API keys and local configuration out of commits, screenshots and public issue reports. Back up the resolved `LOOM_HOME` data directory before changing installation or storage settings.

Interface updates require authenticated owner access and use official GitHub release binaries with checksum verification. The optional Linux system updater accepts no command arguments or arbitrary destinations; it validates root-owned installation paths before privileged execution. See [Updating Loom](docs/updates.md) for setup, trust and rollback boundaries.
