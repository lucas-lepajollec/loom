# Releasing Loom

Loom has **no published release yet**. A version in source is not a release. Do not advertise the one-line installers until the corresponding binaries, checksums and platform behavior have been verified.

## Before creating a tag

1. Confirm the intended commit is on the authoritative Forgejo `main` and that its GitHub mirror is identical. Work from a clean checkout; do not create a separate GitHub merge.
2. Choose the version with the maintainer. Set the same version in `internal/loom/run.go` and `cmd/loom/versioninfo.json`, then move user-visible changes from `[Unreleased]` into `## [X.Y.Z]` in `CHANGELOG.md`.
3. Run `go test -short ./...`, `go vet ./...`, `npm --prefix demo run check` and `make build`. Check that UI assembly leaves no diff.
4. Review installation, migration and removal instructions against the tagged source. Back up an existing `LOOM_HOME` before upgrade tests. Test loopback and authenticated LAN behavior with sanitized fixtures.
5. Test real Windows and macOS installations, not only cross-compilation. Confirm the installed binary path, service/process lifecycle, fresh install, update, data preservation and rollback. A missing platform test must be recorded as a release limitation, not silently treated as passing.

## Prepare a draft, then decide whether to publish

1. After approval, create an **annotated** `vX.Y.Z` tag on the verified `main` commit and push it through Forgejo. Check that the exact tag commit reaches GitHub.
2. In GitHub Actions, manually run **Prepare release draft** with that tag. Pushing a tag alone does not publish anything. The workflow checks the version, changelog, tests and demo; builds Linux, Windows and macOS assets; writes `SHA256SUMS.txt`; and creates an **unpublished draft**.
3. Download or inspect each draft artifact. Verify checksums, executable architecture, startup, install/update commands, and platform-specific limitations. Test `install.sh`, `install.ps1` and `loom update` against the candidate release in controlled environments. In particular, the scripts must fail closed if a checksum is missing or wrong.
4. Only then decide whether the draft should become a public release. Confirm repository visibility and the intended audience first: a private GitHub repository cannot serve as a public download source. Publishing requires Lucas's explicit decision.
5. After publication, update site/docs installation copy and links, then test the public flow as a visitor without repository access. Keep `CHANGELOG.md` and the Control Center release record current.

## If verification fails

Leave the draft unpublished. Fix the source on a new commit and repeat the process with a new tag/version; do not silently replace a published binary or retarget an existing release tag. If an upgrade test modifies user data, restore only from the deliberately created backup after checking its path and contents.
