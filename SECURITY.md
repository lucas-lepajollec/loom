# Security policy

Loom is a local-first AI workspace that controls inference engines, cloud connections, coding-agent harnesses and terminals, and exposes an OpenAI-compatible API. By default, it binds exclusively to the loopback interface (`127.0.0.1`). Reports concerning unauthorized remote execution, unintended process elevation, LAN authorization bypasses, or path traversal in model/preset management are treated with highest priority.

## Supported versions

Security fixes target `main`. See [CHANGELOG.md](CHANGELOG.md) for release status;
a supported-release policy must accompany the first public binary release.

## Reporting a vulnerability

Use the [private vulnerability reporting form](https://github.com/lucas-lepajollec/loom/security/advisories/new) when available.

If the form is unavailable, ask the maintainer for a private reporting channel through the [portfolio contact page](https://lucaslepajollec.com/#contact). Do not include exploit code, credentials, private data or sensitive configuration in that initial message, and do not report vulnerabilities in public issues.

Once a private reporting channel is agreed, include the affected version or commit, reproduction steps, impact and a sanitized proof of concept when possible. Response times are not guaranteed.

## Local deployment guidance

- Do not expose the Loom web dashboard or API endpoints directly to the public internet without a secure TLS reverse proxy and authentication.
- Network access to the dashboard requires a control key, separate from the inference API key used for `/v1`. Configure both before exposing their respective services.
- Keep the optional encrypted vault and OS keychain recovery requirements in mind when backing up. The vault does not encrypt external project files or native harness stores. Harnesses, terminals and MCP tools execute with their own permissions on the selected machine.
- Keep API keys and local configuration out of commits, screenshots and public issue reports. Back up the resolved `LOOM_HOME` data directory before changing installation or storage settings.
