package loom

import "net/http"

// Bench ignores chat-picker visibility. No native command, prompt or login is
// started by reading this catalog; account access is verified at execution.
type benchChoice struct {
	ModelChoice
	Supported bool   `json:"supported"`
	Reason    string `json:"reason,omitempty"`
}

func benchHarnessReason(c ModelChoice, a acpAgent) string {
	if usageHarnessID(a) != "claude-code" {
		return "no_model_only_mode"
	}
	if a.Remote {
		for _, m := range loadRemoteMachines() {
			if m.ID == a.Machine && lifecycleOS(&m) == "windows" {
				return "remote_windows_unsupported"
			}
		}
	}
	if !c.Ready {
		return "cli_unavailable"
	}
	if c.Via != "" {
		return "use_source_directly"
	}
	if c.Model == "" {
		return "native_model_required"
	}
	return ""
}

func benchHarnessChoice(id string) (ModelChoice, acpAgent, bool) {
	for _, c := range modelCatalogSources(workspaceSessions.providers(), false) {
		if c.ID == id && c.Kind == "harness" {
			if a, ok := acpAgentFor(c.RuntimeID); ok {
				return c, a, true
			}
		}
	}
	return ModelChoice{}, acpAgent{}, false
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
		if c.Kind == "harness" {
			a, ok := acpAgentFor(c.RuntimeID)
			if !ok {
				continue
			}
			item.Reason = benchHarnessReason(c, a)
			item.Supported = item.Reason == ""
		}
		choices = append(choices, item)
	}
	sendJSON(w, 200, map[string]any{"ok": true, "models": choices})
}
