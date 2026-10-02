# Repository hygiene report

Historical record of the 2026-10-01 documentation and build-metadata pass.
This report is not the current product inventory; use the [roadmap](../ROADMAP.md),
[README](../../README.md) and [contributor guide](../../CONTRIBUTING.md).

## Changes from that pass

- Documented direct embedding of `ui/next` and the real Makefile targets.
- Removed release-workflow calls to the deleted UI assembler.
- Clarified llama.cpp router loading, temporary API settings and optional
  OS keychain storage.
- Added ignore patterns for local configuration, process IDs and runtime data.
- Removed private-repository assumptions from the security policy.
- Identified outdated migration and ACP notes for a subsequent documentation pass.

The 2026-10-02 public documentation pass replaces obsolete screenshots, updates
workspace/agent guidance and removes deployment-specific release instructions.

## References retained for maintainer review

- The [ACP v1 schema](acp-schema/schema.json) is an intentional protocol reference.
- The [unstable ACP v2 schema](acp-schema/v2/schema.unstable.json) has no current
  implementation binding; decide whether to keep it as a future reference.
- `staticcheck.conf` configures an optional checker even though current CI does
  not invoke it. Lack of a CI invocation alone is not a reason to remove it.
- This historical report can be archived or removed if maintainers prefer
  contributor documentation to contain only active guides.

## Release acceptance still required

Before publication, verify real Linux/macOS/Windows installer and update flows,
OS keychain behavior, native Windows ConPTY, GPU fitting and supported provider
accounts. Confirm supported releases and availability of private vulnerability
reporting. Cross-compilation and protocol fixtures do not establish those results.

The earlier pass recorded successful build, vet and UI checks, but its restricted
execution environment prevented a complete Go test run. Those results apply only
to that pass. Every later change needs its own validation record.
