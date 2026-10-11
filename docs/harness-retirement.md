# Removing coding harness integration

This source change is unreleased. Loom now executes discussion turns through
Local engines and Cloud Chat Completions providers only. ACP, app-server, native
RPC/HTTP agent adapters, installation/account/catalog/quota workflows, remote
native session imports and agent UI screens are removed.

## Existing data

Existing discussion transcripts, historical tool/plan/display events, projects,
Brain sources, model files, engine presets and cloud keys are preserved. Old
agent discussions open as archives; select a Local or Cloud model before sending.
Only portable text and explicitly selected Loom context continue. Native runtime
memory, permissions, tools and account state are not transferred.

Old integration endpoints return authenticated HTTP410 rather than a successful
HTML fallback. An engine node no longer advertises or executes a harness module.
The deprecated `--no-harness` init flag is accepted for old installation scripts;
harness execution is always absent regardless of the flag.

## Before upgrading

1. Stop the relevant Loom process and make a private backup of its data root.
2. Record installed engine paths, model directories and services. Keep models.
3. Build/test this source or use a published release that includes this change.
4. Restart and verify Local/Cloud selection, engine/node access and Brain.

Loom upgrades do not uninstall unrelated native CLIs, delete account histories,
rewrite external client configurations or install an alternative coding app.
Any native CLI retirement is a separate explicit machine-maintenance action.
Recovery uses the previous binary and private pre-upgrade backup. Do not restore
or expose credentials through public files, screenshots or issue reports.

## External coding applications

[T3 Code](https://github.com/pingdotgg/t3code) is an independent option. Its
[official installation guide](https://github.com/pingdotgg/t3code/blob/main/docs/user/install.md)
and background service documentation govern setup and supported providers.
Install and authenticate agents through their supported workflow. No T3 package,
account or private machine configuration is embedded in Loom.

Generic external clients can use Loom's `/v1` inference API with its inference
key, or `/mcp/loom` with a dedicated MCP token and manual client configuration.
These credentials cannot substitute for control-plane authorization.
