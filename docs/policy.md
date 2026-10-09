# Central capability policy (phase 4b backend)

`internal/loom/policy` is the shared `allow / confirm / deny` evaluator. It owns
rules and a bounded audit, not executors, credentials or prompts. Application
boundaries supply the current project, agent, machine and destination metadata.
The existing discussion pipeline, native protocols, Tasks and notifications own
execution and interactions. This release supplies backend contracts; Settings ›
Policy is a separate UI change.

## Rules and defaults

A rule has a unique `id`, `scope` (`global`, `project`, `agent`, `machine`), optional
`scope_id` (required except for global), a capability `subject`, and a `decision`.
Subjects accept exact names, `*`, namespace wildcards such as `agent.*`, and MCP
server wildcards such as `mcp.tool:github/*`.

Explicit rules beat migrated defaults. Among rules of the same class, scope
priority is project → agent → machine → global, then exact subject → namespace
wildcard → `*`. The last rule wins a remaining tie. Conditions are conjunctive;
a rule whose conditions do not match is skipped. Place a broad confirm/deny rule
before a conditional allow rule when the intended default is restrictive.

Conditions:

| JSON field | Meaning |
| --- | --- |
| `inside_workdir` | Require a structured path inside the real workdir. Symlink escapes, unresolved roots and missing path metadata do not qualify. Existing ACP filesystem confinement still applies. |
| `command_prefixes` | Any listed prefix must match a simple command at a word boundary. Compound shell commands, substitutions, pipes and redirects do not qualify. This is a conservative match, not a shell sandbox. |
| `max_cost` | Observed monthly spend ceiling, in the provider's reported currency. At/above the ceiling the matching rule denies. Missing usage skips this condition; it never becomes zero. |
| `provider_id`, `agent_id`, `endpoint`, `model`, `discussion_id` | Exact destination bindings. |
| `operation` | Exact boundary binding; `data.send_provider` uses `select` for fresh destination/context selection and `send` for already-consented sends. Migrated route grants cover only `send`. |
| `legacy_permission` | `ask`, `edits` or `full`; used by migrated defaults to follow existing discussion settings. |

The first unlocked access saves one vault-protected version-1 policy record.
Migration adds conditional rules for existing ACP permission settings, exact
discussion route grants and saved consolidation consents. It does not move or
export credentials. The document's server-owned `migrated` marker prevents
repeating migration after edits. Rules are limited to 512; route migration is
bounded, and unrepresented routes retain the existing execution defaults.

Without a matching explicit rule, existing behavior remains:

| Boundary / subject | Default |
| --- | --- |
| ACP `agent.command`, `agent.file_write`, `agent.file_read`, `agent.search`, `agent.think`, `agent.fetch`, `agent.tool` | `ask` confirms; `edits` auto-allows one-shot read/search/edit/think/fetch; `full` auto-allows offered one-shot approvals. Deletes/moves still ask under edits. Native sandbox escalation and Claude plan transitions always retain explicit confirmation. |
| Native tool approval | Keep the upstream's offered interaction; a launch's full permission does not auto-answer a new native request. |
| `agent.permission_full`, `agent.filesystem_full` | Fresh explicit consent when enabling full permission or filesystem access. |
| ACP filesystem reads/writes already allowed by the client contract | Allow within the existing scoped filesystem boundary. Explicit rules can restrict them. |
| `data.send_provider` | Fresh destination/context selection confirms; already-consented turn sends allow. Every Loom-owned cloud, external engine, Brain model and compaction send is checked again before sending. |
| `notification.summary` | Allow only on the existing opt-in summary delivery path. Summaries remain omitted when the setting is off. |
| `memory.consolidate` | Allow on an explicitly selected provider pair or an already-selected, consented consolidation destination. Missing/disconnected destinations and nonresident local models retain their existing no-generation behavior. |
| `mcp.tool:<server>/<tool>` | Allow through existing authenticated gateway/tool selection. The Loom built-ins use server `loom`. |
| `node.install`, `node.update`, `node.terminal` | Allow existing explicitly requested actions. Lifecycle automation also checks the same install/update policy. |
| `spend.provider` | Allow; an optional rule can impose a cap when a fresh, credential-matching cached provider monthly observation exists. |

Spend checks do not fetch balances, start generation, predict the next response's
price or reinterpret retained/manual token estimates as account spend. Cached
observations expire with the existing Usage balance cache. Concurrent calls can
exceed a ceiling before a provider reports new usage; this is an observation
gate, not a billing reservation system.

An engine turn binds its destination, model and credential before confirmation.
Switching the linked engine while an approval waits does not redirect that
turn's transcript; subsequent turns use the newly selected engine.

Restrictive tool policies keep the native approval channel enabled. A native
protocol without a reply channel, or an opaque bypass mode that suppresses
requests, is refused with a visible reason when it cannot enforce that policy.
Persistent approval answers are reduced to one-shot choices under a restrictive
tool policy. Loom does not change native global permissions or claim control of
tools the native protocol never exposes. Native account/catalog connection and
sign-in remain explicit.

Loom's per-run ACP gateway tokens bind calls to a live discussion and expire
with that run. A manually/global configured native gateway token has no trusted
discussion/agent identity, so only global/local-machine rules can apply there;
caller-supplied arguments never establish an agent or project scope.

## Exact Settings › Policy API

These routes use the existing authenticated control API, require an unlocked
vault and send `Cache-Control: no-store`. POSTs require JSON content type. Optional
fields below are omitted when absent; `conditions` is returned as `{}` when empty.

`GET /api/policy` returns the document directly. `POST /api/policy` replaces the
whole rule array and returns the saved document in the same shape:

```json
{
  "version": 1,
  "migrated": true,
  "rules": [
    {
      "id": "project-command",
      "scope": "project",
      "scope_id": "project-id",
      "subject": "agent.command",
      "decision": "confirm",
      "conditions": {"inside_workdir": true},
      "migrated": false
    },
    {
      "id": "provider-monthly-cap",
      "scope": "global",
      "subject": "spend.provider",
      "decision": "allow",
      "conditions": {"provider_id": "provider-id", "max_cost": 20}
    }
  ]
}
```

The per-rule `migrated` field is optional and omitted when false. Document
`migrated` is forced to true on save. Invalid documents return HTTP 400;
unreadable/corrupt policy fails closed and returns HTTP 503 on GET/save failure.

`POST /api/policy/evaluate` is a dry run: it never starts a tool, sends data or
opens an interaction. All optional input fields are shown here:

```json
{
  "subject": "agent.file_write",
  "project_id": "project-id",
  "agent_id": "codex",
  "machine_id": "local",
  "provider_id": "provider-id",
  "endpoint": "https://provider.example/v1",
  "model": "native-model-id",
  "discussion_id": "discussion-id",
  "operation": "select",
  "permission": "edits",
  "workdir": "/project",
  "path": "src/example.go",
  "command": "go test ./...",
  "cost": 12.5
}
```

```json
{"decision":"allow","rule_id":"legacy-edits-agent.file_write","reason":"rule"}
```

`rule_id` is omitted for an application default. `reason` is `rule`,
`legacy_default`, `cost_limit`, `invalid_cost` or `policy_unavailable`.
For dry runs the default is confirm, except already-consented data `send`, existing node/MCP/spend actions and
opted-in summaries. `permission` supplies the legacy permission projection; an
existing `discussion_id` fills missing permission/workdir metadata. The other
scope and destination fields must describe the operation being previewed.
Actual execution supplies its trusted boundary default; the UI cannot set it.

`GET /api/policy/audit` returns oldest-first entries from a process-local ring
of the last 512 evaluations/answers (including dry runs):

```json
{
  "entries": [
    {"id":1,"at":1791540000000,"subject":"agent.command","decision":"confirm","rule_id":"project-command","reason":"rule","dry_run":false}
  ]
}
```

No discussion title/text, prompt, command, path, endpoint, credential or cost is
copied into audit entries. Answer reasons additionally include
`interaction_answer`, `interaction_grant`, `explicit_consent` and
`native_safety_confirmation`. Restart clears the audit ring.

## Confirm → InteractionRequest → answer

An explicit confirm rule opens the existing canonical request in the active
discussion. Operations outside a turn create a metadata-only `policy` task in
the same session/Tasks journal; these do not appear in discussion lists and do
not create a second executor. They persist their request/outcome metadata.

```json
{
  "id":"policy:request-id",
  "kind":"approval",
  "method":"policy.confirm",
  "approval_kind":"tool",
  "message":"node.install",
  "payload":{"subject":"node.install","machine_id":"local"},
  "options":[{"id":"allow_once","label":"Allow once"},{"id":"deny","label":"Deny"}]
}
```

Use the existing request endpoint (also used by chat and Tasks):

`payload` contains `subject` plus optional `project_id`, `agent_id`, `machine_id`,
`provider_id`, and `model` so the UI can name the target. It contains no prompt,
credential, command, endpoint or private path.

```text
POST /api/workspace/sessions/{session_id}/requests/{request_id}
```

```json
{"decision":"allow_once"}
```

The response is `{"ok":true}`. `deny` declines and `cancel` cancels. Pending
ACP tool approvals retain their offered native options/request method. Task
waiting events reuse the existing single-use phone action tokens; summaries of
policy requests are excluded to prevent recursive summary approvals. A deny
saved while an approval is pending wins when the answer is delivered.

Legacy APIs still refuse immediately without their required `consent:true`, now
with an asynchronous canonical request and this error envelope:

```json
{
  "ok":false,
  "error":"confirm this capability (consent:true or answer the policy request and retry)",
  "code":"policy_confirmation_required",
  "interaction":{"session_id":"policy-task-id","request_id":"policy:request-id"}
}
```

After the task finishes successfully, explicitly retry the same operation. The
grant is one-use, expires after five minutes and is not portable or persisted.
No transcript is automatically sent. Explicit `consent:true` satisfies only the
legacy consent gate; it cannot override a deny or an explicit confirm rule.
Cancellation, vault lock, shutdown and restart never grant permission. A
confirmation can wait up to 24 hours, within the existing task/request bounds.
