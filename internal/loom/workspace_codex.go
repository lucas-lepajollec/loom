package loom

import "net/http"

// Codex execution is registered by acp_registry.go. The app-server quota
// reader stays in workspace_usage.go; it cannot submit discussion turns.
// The historical connection route no longer reads an app-server catalog.
func handleCodexConnect(w http.ResponseWriter, r *http.Request) {
	r.SetPathValue("id", "codex")
	handleRuntimeConnect(w, r)
}
