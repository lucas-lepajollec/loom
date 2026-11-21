# Bench

Bench compares local inference engines and configured cloud Chat Completions
models with the same prompt. Each test runs without project context, memory,
MCP or tools. Results are observations, not a standardized ranking.

Queue requests select local models, presets or a connected cloud provider. Confirm external sharing before
launching cloud tests; these requests can consume API credits.

There is no native coding-account benchmark mode. Existing historical benchmark
records stay readable; new runs use Local or Cloud only.

## Queue and results

Activity can observe and cancel running benchmarks. `/api/bench/tests`
manages saved prompts; `/api/bench/queue` starts or observes a bounded sequential
queue. `/api/bench/queue/cancel` cancels the captured job. Local rows can run on
a linked engine node; cloud credentials remain on the main Loom. The node's
own benchmark endpoint rejects external choices.

Keep model, destination, parameters and consent captured for each run. Results
retain the response and any reported token counts, first-token latency, output
rate and duration. llama.cpp native timings remain separate from measured
request duration and API token rates. Missing metrics stay unknown.

## Verification

Deterministic fixtures exercise consent, queue cancellation and result storage. Tests
never run a paid model automatically. Actual GPU/provider throughput requires
an explicit run with the installed engine or connected provider.
