package loom

import (
	"net/http"
)

func handleWorkspace(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodGet) {
		return
	}
	sendJSON(w, 200, map[string]any{
		"ok": true, "chat": map[string]string{"default_choice": ReadConfig()[chatDefaultChoiceKey]}, "runtimes": runtimeCatalog(), "projects": listProjects(),
		"capabilities": listCapabilities(), "active_project": conv.currentProject(),
		"providers": workspaceSessions.providers(), "sessions": workspaceSessions.list(),
		"models": modelCatalog(workspaceSessions.providers()),
	})
}

func handleCapabilitySave(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var c Capability
	if !workspaceDecode(w, r, &c) {
		return
	}
	saved, err := saveCapability(c)
	if err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "capability": saved})
}

func handleCapabilityDelete(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	workspaceMu.Lock()
	defer workspaceMu.Unlock()
	if err := deleteCapability(req.ID); err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true})
}
