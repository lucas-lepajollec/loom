# Security policy

Loom is a local-first workstation control plane and OpenAI-compatible proxy for `llama-server`. By default, it binds exclusively to the loopback interface (`127.0.0.1`). Reports concerning unauthorized remote execution, unintended process elevation, LAN authorization bypasses, or path traversal in model/preset management are treated with highest priority.

## Supported versions

Security fixes target `main`. See [CHANGELOG.md](CHANGELOG.md) for release status;
a supported-release policy must accompany the first public binary release.

## Reporting a vulnerability

Use the [private vulnerability reporting form](https://github.com/lucas-lepajollec/loom/security/advisories/new) when available.

If the form is unavailable, ask the maintainer for a private reporting channel through the [portfolio contact page](https://lucaslepajollec.com/#contact). Do not include exploit code, credentials, private data or sensitive configuration in that initial message, and do not report vulnerabilities in public issues.

Once a private reporting channel is agreed, include the affected version or commit, reproduction steps, impact and a sanitized proof of concept when possible. Response times are not guaranteed.

## Local deployment guidance

- Do not expose the Loom web dashboard or API endpoints directly to the public internet without a secure TLS reverse proxy and authentication.
- When enabling LAN access, configure an API key in Settings → Server API.
- Keep API keys and local configuration out of commits, screenshots and public issue reports. Back up the resolved `LOOM_HOME` data directory before changing installation or storage settings.
