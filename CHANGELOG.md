# Changelog

Notable changes to Loom are documented here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and releases use [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- Support for local `.env.local` dynamic dev configuration.
- Real-time task activity & background job indicator in UI.

### Added

- Initial private-development version of Loom as a workstation control plane and test bench for llama.cpp.
- Single-process ownership model for external `llama-server`.
- OpenAI-compatible `/v1` proxy with dynamic model loading, slot-level continuous batching, and real-time token throughput metrics.
- Server dashboard with live slots inspection, active/pending request queue, and configurable request history limit.
- Model library with local GGUF catalog, three-tier parameter editing (Essentials, Advanced, Expert), and direct Hugging Face Hub integration.
- Hardware test bench for automated prompt testing and raw prefill/decode throughput measurement.
- Local-first privacy architecture with data isolated in `$LOOM_HOME` and zero telemetry.

No version has been tagged or published yet. Move these notes into a versioned section only when a release is actually shipped.
