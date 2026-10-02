# Cloud provider balances

Loom observes provider account balances independently of retained Loom token
counts, manual-price estimates and [native harness usage](harness-usage.md).
No discussion, context or generation request is sent. Reads happen only when
these HTTP routes are requested; there is no background polling.

## HTTP API

`GET /api/usage/providers` returns `{ok:true,providers:[...]}` for saved
connections with a key in the current workspace session, sorted by name.
Disconnected connections are omitted. Each provider runs concurrently with a
10-second budget, including both OpenRouter reads. A provider failure stays in
its own result and does not fail the list.

`POST /api/usage/providers/refresh` accepts `{"provider_id":"saved-id"}` and
returns `{ok:true,provider:{...}}`, bypassing that provider's cache. Unknown IDs
return 404; disconnected providers return 409. Invalid JSON or extra fields
return 400; wrong HTTP methods return 405.

Both routes use the existing control-key middleware (`Authorization: Bearer …`
when configured), strict JSON decoding and `Cache-Control: no-store`. An encrypted
vault must be unlocked; otherwise they return 423 with an unlock instruction.
Vault access is checked again after reading so relocking cannot disclose or cache
an account result. Provider credentials come only from `workspaceSessions.keys`;
these readers never consult the OS keychain themselves. They never return, log
or persist keys or upstream diagnostics. Only fixed errors and HTTP status codes
are returned, and redirects are refused.

The in-memory cache lasts five minutes, including failures. It belongs to the
workspace session, checks the saved endpoint and a non-public key fingerprint,
and shares in-flight reads for the same connection. A key rotation triggers a
new read. Refresh joins an already-running read rather than issuing duplicates.

## Result fields

Every row contains `provider_id`, `name`, nullable `currency`, `balance`,
`granted`, `used`, `limit`, `limit_remaining`, `period_usage:{day,week,month}`,
nullable `free_tier`, `source`, `fetched_at` (Unix seconds), `error` (empty on
success), and `supported`. Numeric values are amounts in the reported currency,
not tokens or percentages. Missing values remain `null`; a reported zero is zero.
`supported` identifies an implemented read API, not proof that the key is valid
or entitled to every endpoint. Errors can coexist with successful partial data.

| Official endpoint host | Read-only paths | Normalization |
| --- | --- | --- |
| `openrouter.ai` | `/api/v1/key`, `/api/v1/credits` | USD; key limits, remaining limit, daily/weekly/monthly usage and free tier from `/key`. Account balance is `total_credits - total_usage` only when both are supplied. `used` is account `total_usage` when supplied, otherwise key `usage`; `source` identifies the scope. Purchased credits are not labeled as grants. |
| `api.deepseek.com` | `/user/balance` | Currency from the single `balance_infos` entry; `balance` from `total_balance`, `granted` from `granted_balance`. String amounts are parsed without defaulting to zero. Multiple currencies cannot be represented as a single balance and produce an explicit error; they are never summed or silently selected. |
| `api.moonshot.ai`, `api.moonshot.cn` | `/v1/users/me/balance` | USD for `.ai`, CNY for `.cn`; `balance` from `available_balance`, `granted` from `voucher_balance`. The two platforms have separate keys/accounts. |
| `api.siliconflow.com`, `api.siliconflow.cn` | `/v1/user/info` | `balance` from `totalBalance`. The documented shape does not identify currency or define a grant amount, so these remain null. User IDs, email, avatar and other account metadata are discarded. |

Detection uses exact HTTPS hosts, with the default TLS port only. Request paths
are fixed at the configured origin, independent of the chat base path. Custom
proxies and lookalike hosts are unsupported; no provider is inferred from its
connection name. Responses are bounded to 1 MiB.

Mistral, OpenAI, Anthropic, Groq and Gemini have no implemented public balance API
using a normal inference key: their rows return `supported:false`, null amounts,
and an explanatory error without contacting them. Admin-only usage/cost APIs,
private dashboard endpoints and scraping are outside this capability.

## Official references and limitations

Shapes were checked against the official documentation:
[OpenRouter current key](https://openrouter.ai/docs/api/api-reference/api-keys/get-current-api-key),
[OpenRouter credits](https://openrouter.ai/docs/api/api-reference/credits/get-remaining-credits),
[DeepSeek balance](https://api-docs.deepseek.com/api/get-user-balance/),
[Kimi international balance](https://platform.kimi.ai/docs/api/balance),
[Kimi China balance](https://platform.kimi.com/docs/api/balance),
[SiliconFlow user info](https://docs.siliconflow.com/en/api-reference/userinfo/get-user-info),
and [SiliconFlow's official OpenAPI schema for the China host](https://github.com/siliconflow/siliconcloud/blob/main/openapi.yaml).

OpenRouter currently documents `/credits` as requiring a management key. Loom
still attempts both read endpoints with the connection's own key. A refusal of
`/credits` keeps valid `/key` observations, leaves account balance unknown and
reports the endpoint's HTTP error. Loom never requests a stronger credential or
substitutes another provider's key. Monetary and period totals keep their native
scope; these observations are not invoices or routing decisions.

Fixtures use `httptest` recorders and an in-process HTTP transport, so no real
accounts, generation or listening sockets are required for balance tests. Real
provider-account acceptance is separate from synthetic fixture validation.
