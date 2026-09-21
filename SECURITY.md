# Security policy

Loom is a local-first workstation control plane and OpenAI-compatible proxy for `llama-server`. By default, it binds exclusively to the loopback interface (`127.0.0.1`). Reports concerning unauthorized remote execution, unintended process elevation, LAN authorization bypasses, or path traversal in model/preset management are treated with highest priority.

## Supported versions

Security updates target the latest release and the `main` branch.

## Reporting a vulnerability

Use the repository's [private vulnerability reporting form](https://github.com/lucas-lepajollec/loom/security/advisories/new).

If private reporting is unavailable, open a minimal public issue asking for a private contact channel. Do not include exploit code, credentials, private data or sensitive configuration in that issue.

Include the affected version or commit, reproduction steps, impact and a sanitized proof of concept when possible. You should receive an initial acknowledgement within 7 days and an assessment within 14 days.

## Local deployment guidance

- Do not expose the Loom web dashboard or API endpoints directly to the public internet without a secure TLS reverse proxy and authentication.
- When enabling LAN access, configure an API key in Settings → Server API.
- Store sensitive configuration and local environment variables in untracked `.env.local` files.
