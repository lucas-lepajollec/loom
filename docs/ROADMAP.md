# Roadmap

## Current consolidation

- Remove coding harness runtime protocols, installers, account and quota flows,
  native catalogs, remote session import and UI screens.
- Preserve legacy discussion history without executing retired runtimes.
- Keep local/cloud models, engines/nodes, Brain, projects, generic tools, skills,
  MCP, terminals, voice and safety controls.
- Validate source builds, mobile/desktop views, deterministic tests and docs.

This source transition is unreleased. Existing installed binaries keep their
previous behavior until explicitly updated. See [migration](harness-retirement.md).

## Next

Dogfood Local/Cloud execution and connected brains. Improve stability, resource
usage, responsive interaction and operational diagnostics based on reproducible
failures. Further graph indexing or advanced routing remains deferred.

No native coding-agent installer or orchestration layer is planned: coding
agents run in Codeman and Loom drives them through its public API. Next steps
there connect agents to Brain through Loom's MCP gateway rather than through a
new agent runtime. See the [November 2026 audit](audits/2026-11-product-audit.md).
