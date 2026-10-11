package loom

import "net/http"

// Bench ignores chat-picker visibility. No native command, prompt or login is
// started by reading this catalog; account access is verified at execution.
type benchChoice struct {
	ModelChoice
	Supported bool   `json:"supported"`
	Reason    string `json:"reason,omitempty"`
}

func handleBenchCatalog(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !workspaceMethod(w, r, http.MethodGet) || !usageVaultAccess(w) {
		return
	}
	choices := []benchChoice{}
	for _, c := range modelCatalogSources(workspaceSessions.providers(), false) {
		if c.Kind == "local" {
			continue
		} // Library comes from the selected engine.
		item := benchChoice{ModelChoice: c, Supported: c.Ready}
		if c.Kind == "cloud" && !c.Ready {
			item.Reason = "provider_disconnected"
		}
		choices = append(choices, item)
	}
	sendJSON(w, 200, map[string]any{"ok": true, "models": choices})
}
