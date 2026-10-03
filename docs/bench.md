# Bench

Bench compares a saved text test across local engines, cloud APIs and supported
native-account models. It is an observation tool, not a standardized model
quality score. Responses are available under each result; the last 15 runs are
retained. Select up to 16 models per run.

Create a test with a name, prompt and requested maximum output tokens (1–4096).
The built-in performance test uses a varied passage of approximately 2000
tokens and requests 300 output tokens. Tokenizers and native model limits differ.
Each current Loom executor receives the same user prompt and neutral system
instruction, `Answer the user request directly.` No discussion history,
project folder, Brain context, Loom skills or MCP is supplied.

## Sources

- **Local engine:** managed GGUF/presets on the selected engine, including a
  linked engine node. A directly linked inference server receives the selected
  model ID without changing its saved model or the control plane's configuration;
  Loom does not apply local presets to direct servers.
- **Cloud API:** configured Chat Completions-compatible provider/model IDs,
  independent of their visibility in the discussion picker. Reconnect a provider
  in Cloud if its key is unavailable. Native Anthropic/Responses APIs are not
  silently translated here.
- **Native account:** connected native catalogs from Harnesses. A connection or
  catalog is not proof of a paid subscription or access to every model. Only
  implemented modes that suppress tools are selectable. Authentication and model
  authorization remain with the native CLI; Loom neither extracts OAuth keys
  nor pretends an account subscription is a general provider API.

Cloud and native-account runs require explicit confirmation before sending the
test. They can consume API credits, subscription quota or provider extra usage.
Catalog reads and polling never generate a reply. Quotas are native observations,
not a guarantee that the next request will be accepted.

## Native model-only support

**Claude Code:** local Linux/macOS/Windows launchers and saved POSIX SSH machines
are supported. The installed CLI must advertise the required flags. Preflight
uses native `auth status --json` and requires a `claude.ai` login, without storing
its account metadata. Run mode uses:

- a fresh empty temporary working directory, never the saved harness workspace;
- `--safe-mode` and `--restricted`, empty setting sources and disabled hooks;
- `--tools ""`, `--disallowedTools "*"`, strict empty MCP, disabled skills/slash
  commands and browser integration;
- a replacement neutral system prompt, selected native model, no session
  persistence, one turn, and a requested output budget via `CLAUDE_CODE_MAX_OUTPUT_TOKENS`;
- structured streaming output, no permission bypass or approvals.

The initialization event must announce an empty tool/MCP catalog. A missing
catalog or tool event fails the measurement; Loom does not execute or answer it.
An old CLI missing these controls is refused before generation. Native account
credentials stay in the CLI's own storage and managed policy remains native.
This removes agent tools/context, but does not make CLI startup, server policy,
reasoning, sampling defaults or caches identical to a raw cloud API.

Do not substitute `--bare`: the inspected CLI disables OAuth/keychain reads in
that mode. **Codex, Antigravity, Pi, OpenCode, Hermes and custom ACP adapters**
remain non-executable in this Bench account mode until Loom has an implemented,
verified suppression contract. Telling an agent “do not use tools” or denying
approvals is insufficient. Their ordinary discussions remain available.
Remote Windows SSH account benchmarks are not supported yet.

Native controls are documented in the [Claude CLI reference](https://code.claude.com/docs/en/cli-reference)
and [environment variables](https://code.claude.com/docs/en/env-vars).
Antigravity's [headless mode](https://www.antigravity.google/docs/cli/headless/)
is an agent execution mode; its SDK's tool configuration requires a separate
API integration and is not used to bypass native-account authentication.

## Measurements

Missing metrics display as unknown, never zero or an invented estimate.

- Local prefill/decode rates are native engine timings when reported. Compatible
  engines that report only usage do not acquire fabricated phase timings.
- API first-text latency is observed at the first nonempty streamed content.
  Output rate uses reported output tokens divided by elapsed time after that
  first text. Provider token counts may include reasoning; it is not native
  decode speed. No rate is shown when output usage is absent.
- Native CLI latency starts with process launch and ends at the first text
  delta; it stays unknown on result-only output. Its output rate is reported
  output tokens divided by total elapsed time, including startup and reasoning.
  Reported cache input tokens are included in native input totals. The native
  resolved model ID is retained when announced.

Warm caches, reasoning defaults, model aliases and remote service load can
change results. Older engine nodes keep their original prompt pipeline and may
return only a short response preview. A single run cannot establish overall
model quality or an unbiased speed ranking.

## Queue ownership and APIs

The main Loom owns mixed queues, custom tests and history. Only local selections
are sent to its captured engine node; cloud keys and native-account access stay
on the main. Changing the selected engine refuses remaining local rows rather
than dispatching them to a different machine. Node custom tests are temporary
and deleted after execution; an unreachable node can retain one. Node history
also retains its own local runs.

Updated nodes advertise scoped cancellation. The main sends
`X-Loom-Bench-Job` to cancel only the job it created, atomically checked by the
node. An older node lacking this capability is **not automatically cancelled**;
stop it on the node or update it. Cancellation does not refund consumed quota.
After a main-process restart, unfinished rows are marked interrupted and saved
responses are preserved. Loom does not automatically resume quota-consuming
requests; a legacy remote node may still need a manual stop.

Protected same-origin APIs:

- `GET /api/bench/catalog`: configured external choices, supported/reason state.
- `GET/POST /api/bench/tests`, `POST /api/bench/tests/delete`.
- `GET/POST /api/bench/queue`, `POST /api/bench/queue/cancel`.
- `GET /api/bench/runs`, optionally `?id=<job>` for one saved run.
- Queue/runs reads support `?compact=1`, retaining previews/metrics without
  repeating full response text during polling.

The visible page polls queue state every 1.5 seconds and catalog state every
10 seconds, pauses while hidden and serializes each poller. Responses are
bounded to 64 KiB per selection and escaped in the UI. Raw native diagnostics,
credentials and account metadata are not persisted with benchmark results.

Synthetic protocol/HTTP/UI tests cover consent, suppression/refusal, account
method checks, cancellation, mixed node/API queues and absent usage. Real paid
account/model access, remote SSH teardown and native Windows launchers require
acceptance on the actual supported platforms.
