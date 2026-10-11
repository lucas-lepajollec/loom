# Workspaces

Workspaces are named folders on Loom's machine or a saved remote machine. Set
a default in Settings or during first-run setup; projects inherit it unless
another workspace is chosen. Additional saved folders remain selectable.

A project stores its title, workspace/machine, default Local/Cloud model and
context. Its own discussions supply project memory; users do not choose an
arbitrary list of conversation IDs. A workspace is also a terminal starting
folder, not a coding-agent runtime or a filesystem sandbox.

`GET /api/workspaces` lists saved workspaces. `POST /api/workspaces` creates,
updates or removes a workspace using its ID, path, machine and explicit action.
Remote folder browsing goes through the authenticated saved machine or node;
local browsing uses `/api/fs/dirs`. Unknown or unreachable machines fail visibly.

Generic model tools retain Loom's existing permissions and central policy.
Do not infer OS-level isolation from a workspace selection. Explicit sensitive
operations still require their applicable confirmation.

Coding harness accounts, permissions, launch modes and native sessions are
retired. Existing discussion text remains readable; see [migration](harness-retirement.md).
