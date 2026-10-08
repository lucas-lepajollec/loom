# OpenCode HTTP contract

`openapi.json` is the unedited output of **OpenCode 1.18.33**'s built-in
`opencode generate`, which calls the same `Server.openapi()` as `/doc`.
The sandbox denied the loopback listener, so this is CLI-generated, not a
live HTTP capture. It contains no account or session state.

Refresh with an installed candidate release, no prompts:

```sh
opencode --version
opencode generate > internal/loom/runtime/opencodehttp/schema/openapi.json
# In a socket-enabled environment, compare with authenticated GET /doc from
# opencode serve --hostname 127.0.0.1 --port 0 and an ephemeral server password.
```

Review the events, session/prompt/abort, provider, permission and question
schemas; update VERSION, TestedVersion and fixtures together. The legacy
permission.updated alias is accepted; this release emits permission.asked.
Version 2 permission/question events remain visible raw diagnostics and fail
explicitly until their distinct reply contracts are supported.
