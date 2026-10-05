# Web search

Open **Settings → Internet** to enable web access and select a search provider.
Search configuration belongs to the control plane, independently of the machine
running inference. Saving a provider does not send a query. Enter a query and
press **Test search** to check the saved connection without model generation.

| Provider | Configuration | Request |
| --- | --- | --- |
| DuckDuckGo | Default, no key | Served HTML search; may be blocked or challenged upstream |
| SearXNG | Instance URL, optional reverse-proxy Bearer token | JSON search on your own instance |
| Brave Search | Search API key | Official Web Search API |
| Tavily | Search API key | Basic search, no generated answer or raw page content |

Only the selected provider receives queries. Loom does not silently fall back
or switch providers on error. API searches can incur provider charges, including
an explicit connection test; consult the provider's current plan.

## Connect SearXNG

1. Use an instance address reachable **from the Loom control plane**. A Docker
   container name only works when Loom shares that network. Otherwise expose the
   container's port to the intended host/network and use that address.
2. In the instance's `settings.yml`, include `json` alongside existing
   `search.formats` (normally `html`). Restart SearXNG after configuration changes.
3. Select SearXNG and enter the base URL or `/search` endpoint. HTTPS is supported;
   HTTP is accepted for local-network instances. Credentials/query parameters
   must not be embedded in the URL. Enter a proxy token separately, if needed.
4. Save, then test a query. A 403 can mean JSON format is disabled or the instance
   access rules reject the caller. Loom reports the error rather than returning
   an empty successful result.

See the [SearXNG search API](https://docs.searxng.org/dev/search_api.html).
Brave uses its [Web Search API](https://api-dashboard.search.brave.com/app/documentation/web-search/get-started);
Tavily uses its [Search API](https://docs.tavily.com/documentation/api-reference/endpoint/search).

## Search and page reading

For direct local models, enabled web access provides `web_search` plus
`web_open`, `web_read` and `web_grep`. The model needs tool-call support. Search
uses the selected provider; opening a page uses the separately selected reader:
Go for served HTML, or Crawl4AI for JavaScript rendering. A missing/unreachable
Crawl4AI reader no longer disables independent search or blocks turn preparation.
Page-reading failures remain explicit; Go cannot execute JavaScript.

For cloud Chat Completions models, enabled web access provides **only
`web_search`**. The provider/model must support streamed function calls. Unsupported
models return an explicit error; disable web access to use their ordinary text
connector. The cloud connector does not provide URL fetches, host files, shell,
MCP or private-memory tools. Search snippets are untrusted data, not instructions.
Tool events are displayed in the discussion. Calls/results and search timeouts
are bounded; accumulated token usage covers all reported inference rounds.

Harnesses keep their native web tools and account permissions. Bench runs do not
inherit web search; comparisons still suppress tools and project context.

## Credentials and access

Search API keys are stored server-side in the private encrypted credential store
under `LOOM_HOME/secrets/providers`, separately from cloud provider records. GET
responses report only whether a key is set. Blank input retains a saved key;
**Remove** followed by save deletes it. Keys never enter model arguments or
browser localStorage. Back up the installation's encryption key and sealed files
together privately. Encryption does not protect a compromised host account.

Authenticated APIs are `GET/POST /api/internet/search` (configuration) and
`POST /api/internet/search/test` with a `query`. Search test returns bounded
results without a model call. The optional vault must be unlocked for configuration
or tests. Provider error bodies are not echoed to the interface.

## Upgrade and rollback

Existing installations retain DuckDuckGo by default and their existing web-access
switch. Cloud turns now receive the narrowly scoped search tool only when that
switch is enabled. No search credentials are inferred from cloud API keys.
Back up `LOOM_HOME` before upgrades. Preserve new sealed credentials on rollback;
older versions do not understand the selected provider and continue their old
search path. Disable web access before rolling back if that behavior is unwanted.
