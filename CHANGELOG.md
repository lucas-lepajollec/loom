# Changelog

Notable changes to Loom are documented here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and releases use [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- Support for local `.env.local` dynamic dev configuration.
- Real-time task activity & background job indicator in UI.

## [0.1.0] - 2026-09-21

### Added

- Initial standalone release of Loom as a workstation control plane and test bench for llama.cpp.
- Single-process ownership model for external `llama-server`.
- OpenAI-compatible `/v1` proxy with dynamic model loading, slot-level continuous batching, and real-time token throughput metrics.
- Server dashboard with live slots inspection, active/pending request queue, and configurable request history limit.
- Model library with local GGUF catalog, three-tier parameter editing (Essentials, Advanced, Expert), and direct Hugging Face Hub integration.
- Hardware test bench for automated prompt testing and raw prefill/decode throughput measurement.
- Local-first privacy architecture with data isolated in `$LOOM_HOME` and zero telemetry.

[Unreleased]: https://github.com/lucas-lepajollec/loom/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/lucas-lepajollec/loom/releases/tag/v0.1.0
