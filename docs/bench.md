# Bench

Bench compares local inference engines and configured cloud Chat Completions
models with the same prompt. Each test runs without project context, memory,
MCP or tools. Results are observations, not a standardized ranking.

Choose local models/presets or supported cloud catalog entries. Cloud choices
are independent of discussion-picker visibility. Disconnected or unsupported
providers remain unavailable with a reason. Confirm external sharing before
launching cloud tests; these requests can consume API credits.

There is no native coding-account benchmark mode. Existing historical benchmark
records stay readable; new runs use Local or Cloud only.

## Queue and results

`GET /api/bench/catalog` returns available model choices. `/api/bench/tests`
manages saved prompts; `/api/bench/queue` starts or observes a bounded sequential
queue. `/api/bench/queue/cancel` cancels the captured job. Local rows can run on
a linked engine node; cloud credentials remain on the main Loom. The node's
own benchmark endpoint rejects external choices.

Keep model, destination, parameters and consent captured for each run. Results
retain the response and any reported token counts, first-token latency, output
rate and duration. llama.cpp native timings remain separate from measured
request duration and API token rates. Missing metrics stay unknown.

## Verification

Deterministic HTTP/runtime/UI fixtures exercise consent, unavailable choices,
queue cancellation, full response rendering and narrow-screen metrics. Tests
never run a paid model automatically. Actual GPU/provider throughput requires
an explicit run with the installed engine or connected provider.
