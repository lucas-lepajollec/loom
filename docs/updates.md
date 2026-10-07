# Updating Loom from the interface

Open **Settings → About → Updates**, then **Check**. Loom checks the latest stable
release from [the official repository](https://github.com/lucas-lepajollec/loom/releases).
When a newer version is available, choose **Install update** and confirm. There
is no repository connection, Git account or token to configure. Updates use
published platform binaries, not the repository's latest source commit. A commit
push alone does not make an installable update available.

Loom downloads the matching OS/architecture binary, verifies the release
SHA-256 manifest and size, keeps the previous binary as `loom.previous` beside
the executable, then replaces the binary. Missing or mismatched checksums stop
the update before replacement. Temporary filenames are unique and concurrent
requests in the running interface are rejected. If the reviewed release changes,
check again rather than silently installing another version.

On Linux with the installed `loom-ui` service and restart permission, the
interface schedules its own restart. The page waits for the new running version
before reloading, and reports when it cannot confirm the restart. The engine
service is not restarted. Ongoing interface requests, including active chat or
terminal connections, may be interrupted: finish important work first. Portable
foreground runs and macOS require a manual restart; Windows uses the existing
application restart helper. Windows/macOS behavior still needs real-device
acceptance for this change.

## Update channels

Settings › About › Updates › **Channel** chooses what this installation follows:

- **Stable** (default): published releases only.
- **Development**: the `edge` pre-release, rebuilt by CI after every change
  merged into `main`. Its version looks like `0.2.10-dev.20261007171200` (next
  patch, then the build time in UTC), so it is always newer than the release it
  starts from and older than the next stable release. Use it to receive fixes
  right away on a test or personal installation; it may contain defects.

The `edge` tag is kept on Forgejo as well (the push mirror removes GitHub-only
tags) and is never moved; the release notes name the commit each build comes
from.

Both channels download official assets of this repository and verify them
against the published `SHA256SUMS.txt`. Switching back to Stable never
downgrades: the installation simply waits for the next stable release.

## System installation permissions on Linux

For a regular, root-owned `/usr/local/bin/loom` installation, the installer sets
up `/usr/local/libexec/loom-update` and a scoped `/etc/sudoers.d/loom-update` rule
for the selected service user. The rule allows **only that helper, with no command
arguments**. The helper runs under a clean environment, checks root ownership,
symlinks and writable permissions before executing the installed Loom, and
accepts only the reviewed official release version on stdin. It cannot receive
a caller's download URL, destination path or shell command. It does not load
local dotenv, user data or harness configuration. A root-side lock prevents
simultaneous helper updates. The sudoers rule is checked with `visudo` before
installation.

Existing installations need the new binary and one administrator setup once:

```sh
sudo loom install
```

Run this using the new version after backing up the existing installation. The
release installer also performs that setup. Afterwards, normal updates can be
started by the authenticated owner from the interface without SSH. A source or
symlink installation never receives root execution permission for user-owned
code; use the portable updater with a writable binary directory, or install the
regular official binary. The interface explains when setup/permissions block
installation. Uninstalling removes the helper and its sudoers rule.

## Data and rollback

Binary updates leave the configured `LOOM_HOME`, models and engine installation
in place. Before upgrading, back up user data while Loom services are stopped;
a binary rollback cannot undo a future data migration. The previous executable
is retained with owner-only permissions. To roll back a standard Linux install,
stop the interface, copy `/usr/local/bin/loom.previous` to a temporary file beside
`loom`, set executable permissions, rename it over `loom`, then restart the
interface. Restore data only from the deliberate backup if the release changed
its schema. Do not overwrite an executable in place while it is running.

`loom update --check` and `loom update` retain the CLI flow. The authenticated
HTTP API uses `GET /api/update` for release/capability metadata and
`POST /api/update/apply` with JSON `{ "version": "X.Y.Z" }` for the version the
owner reviewed. Check errors are reported as errors, not as “up to date.”
