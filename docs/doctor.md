# Loom Doctor and diagnostic bundles (phase 4b backend)

Doctor observes the current installation without starting engines, loading
models, executing paid turns, signing in, installing/updating software or
launching MCP servers. Checks run concurrently, with a five-second deadline per
check and at most two concurrent Doctor runs. A timeout is a warning with
`detail: "check_timeout"`. Fixes are suggestions; Doctor never executes them.
The Doctor page and degraded-state presentation are separate UI changes.

## Exact Doctor API

Use the existing authenticated control API. `GET /api/doctor` runs all checks.
`POST /api/doctor/run` accepts `{}` for all checks, or
`{"id":"engine.reachable"}` to rerun one check. POSTs require JSON content type.
Both return the report directly, with a numeric Unix-millisecond `at`:

```json
{
  "checks":[
    {
      "id":"engine.reachable",
      "area":"engine",
      "status":"warn",
      "title":"engine.reachable",
      "detail":"engine_outdated",
      "fix":{"label":"open_settings","href":"#/models"}
    }
  ],
  "at":1791540000000
}
```

`area` is `core | machines | engine | agents | brain | notifications | security`.
`status` is `ok | warn | fail | skip`. `fix` is optional, with required `label`
and optional `action` or `href`; current fixes use `href`. IDs, title and detail
codes are language-independent. Translate dynamic `agent.<runtime_id>`,
`machine.<machine_id>` and `mcp.<server_name>` titles using their prefix plus
the existing entity name. Reports are sorted by check ID. Detail values for
`core.version` are `<version>; <channel>` and successful disk checks are
`available_bytes:<integer>`; other details are stable codes.

GET returns HTTP 429 when both run slots are occupied. POST returns HTTP 409
for an unknown check ID or occupied slots. Errors use
`{"ok":false,"error":"..."}`. Responses are `no-store`. Core/security checks
remain accessible with the vault locked; private integration checks are omitted
and Brain reports `skip/vault_locked` until unlock.

| Check ID | Observation and actionable results |
| --- | --- |
| `core.version` | Loom build version and selected update channel; no remote update download. |
| `core.data_writable` | Effective access to the data directory, without creating a file; `data_not_writable`. |
| `core.disk_space` | Available bytes; warning below 512 MiB (`disk_space_low`), unknown is `skip/disk_space_unknown`. |
| `security.vault` | `vault_locked`, `vault_unlocked`, or `vault_not_configured`. |
| `brain.primary_writable` | Primary Brain source configuration and writable directory metadata; never reads Brain note contents. |
| `engine.reachable` | Read-only health/models and optional properties. `engine_unreachable`, `engine_unreachable_or_unloaded`, `engine_outdated`, `engine_reachable`. A router with no loaded model can be healthy through `/v1/models`. Local build mismatch compares reported build info with the selected installed prebuilt version; linked node version compares with this Loom. Unreported engine build versions are not invented. |
| `machine.<id>` | Each paired machine's node info, modules, handshake and Loom version: `machine_unreachable_or_incompatible`, `node_outdated`, `node_reachable_version_match`. |
| `agent.<id>` | Each connected/enabled agent's executable, cached handshake and tested version: `agent_not_installed`, `handshake_not_observed`, `handshake_failed`, `capability_probe_failed`, `agent_version_unknown`, `agent_version_untested`, `agent_installed_handshake_cached`. Native cached records identify their actual executable, not an unused ACP launcher. |
| `mcp.<server>` | Ping an already-connected MCP session only: `mcp_disabled`, `mcp_not_observed`, `mcp_unreachable`, `mcp_reachable`. No connection launch or tool call. |
| `notifications.ntfy`, `.webhook`, `.push` | Enabled flag and last delivery error: `channel_disabled`, `notification_delivery_failed`, `no_delivery_error_observed`. No test delivery. |
| `security.password` | Whether a browser access password is configured. |
| `security.lan` | Fail `lan_without_password` if the configured listener is exposed without a password; a legacy API key does not count as a browser password. |
| `security.push_https` | `push_disabled`, `push_requires_https`, `push_https`. |
| `security.tokens_age` | Metadata ages of the local node token, sealed provider/notification credentials and paired node link timestamps. Warn `token_rotation_due` after 90 days; absent metadata is `token_age_unknown`, unreadable metadata is `token_metadata_unavailable`. OS keychain/native account ages are unknown; no secret values are read for this check. |

The runner and feature state have fake transport/unit coverage. Actual CLI
compatibility, hardware, installed services and phone delivery still require
explicit real-platform acceptance; Doctor does not turn cached observations
into an assertion that a paid live turn passed.

## Opt-in redacted bundle

`POST /api/doctor/bundle` with `{}` and an unlocked vault returns:

```text
Content-Type: application/zip
Content-Disposition: attachment; filename="loom-diagnostics.zip"
Cache-Control: no-store
```

The request runs Doctor with a ten-second overall context. On failure it returns
HTTP 503 and `{"ok":false,"error":"diagnostic bundle unavailable"}`. It does
not upload, email or send the archive anywhere. The user chooses whether to
attach it to a public issue.

Included files:

| File | Contents |
| --- | --- |
| `versions.json` | Loom/channel, Go version, OS/architecture. |
| `doctor.json` | Current checks. |
| `compatibility.json` | Cached compatibility records; free-text warnings omitted. |
| `failed-probes.json` | Cached failed handshake/feature metadata; no protocol frames or error bodies. |
| `config.json` | Sanitized config, provider metadata and MCP configuration. |
| `events.json` | Recent event `type` and `at` only; no titles, summaries or request text. |
| `logs/engine.log`, `logs/node.log`, `logs/build.log` | Last 100 lines of known local service logs, reduced to severity and an omission marker. Missing files produce empty logs. |
| `logs/agents.log` | Bounded cached install/update log tails, with the same omission. Native session/transcript logs are excluded. |
| `manifest.json` | Every included file, its byte count where available, and redaction categories. |

Each source is bounded to 128 KiB, with at most 64 files and 2 MiB of aggregate
source output. No traversal of arbitrary data directories, subprocess log
collection, SSH reads or configured arbitrary file paths occurs. Databases,
discussion text, prompts, Brain content, environment and raw protocol frames
are excluded. Arbitrary log bodies are entirely omitted because they can contain
unlabelled private text. JSON credential/content fields, known secrets and
common encodings, emails, home paths and URL credentials/path/query/fragment
are redacted. URLs retain only scheme/host. Known local secrets are used solely
to scrub output and are never included as an archive source.

The manifest shape is:

```json
{
  "version":1,
  "files":[
    {"path":"config.json","bytes":123,"redacted":["credential_field","home_path","known_secret","private_content","url_credentials_path_query"]},
    {"path":"manifest.json","redacted":[]}
  ],
  "excluded":["prompts","discussion_text","brain_content","protocol_frames","unstructured_log_bodies","databases","environment"]
}
```

`bytes` is omitted for zero-length files and the self-describing manifest.
Other possible redaction categories are `credential`, `email`, `private_key`,
`url`, and log categories `log_content`, `prompts`, `credentials`, `paths`.
Tests inject known secrets into every source and inspect every uncompressed ZIP
member, including the manifest.

## Feature-level degraded mode: exact UI fields

Runtime descriptors (`GET /api/runtimes`), agent probes
(`GET /api/runtimes/{id}/probe` → `probe`), agent installations
(`GET /api/agents/installations` → `installations`), machine records and node
info responses can include:

```json
{"degraded":[{"capability":"resume","reason":"handshake_failed","since":1791540000000}]}
```

The field is optional when empty. `since` is Unix milliseconds and stays stable
across repeated failures. Failed capabilities are removed from descriptor
`capabilities`, their corresponding `features` booleans become false, and
machine `modules` exclude failed modules. `/api/engine/node` additionally returns
`degraded` for the selected engine (including `[]` when empty).

Cached probes/compatibility records may carry explicit feature observations:

```json
{"capability_checks":[{"capability":"models","ok":false,"reason":"catalog_incompatible"}]}
```

A failed agent catalog handshake disables catalog/history/resume operations;
it does not declare an installed chat protocol broken. Explicit feature checks
from a successful native handshake followed by a catalog failure disable only
`models`; native resume/history capabilities remain available. Explicit checks
disable only the named feature. A successful refreshed probe restores features
that are no longer failed. Node observations disable missing/unreachable modules
and restore them on the next successful Doctor or existing health-monitor probe.
Engine reachability failure disables `chat`/`stream` generation while leaving
installation/management available; the next successful Doctor probe restores
them. A merely untested version is a compatibility warning, not evidence of a
specific broken feature. State is bounded and process-local; cached agent checks
can be reapplied by Doctor after restart.

Transitions publish through the existing event bus:

```json
{"id":12,"at":1791540000000,"type":"capability.degraded","title":"","status":"degraded","owner":"agent:codex","capability":"resume","reason":"handshake_failed"}
```

Restoration changes `type` to `capability.restored` and `status` to `restored`.
Owners are `agent:<runtime_id>`, `machine:<machine_id>`, `engine:local`, or an
opaque `engine:<destination_digest>` for a linked engine (no URL). Notification
rules accept both types and exclude them by default. Existing Events/Tasks SSE
and notification settings remain the delivery surface; no parallel bus exists.
