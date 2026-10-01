package loom

import (
	"encoding/json"
	"io"
	"mime"
	"net/http"
)

func workspaceMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == method {
		return true
	}
	w.Header().Set("Allow", method)
	sendJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false, "error": "méthode non autorisée"})
	return false
}

func workspaceDecode(w http.ResponseWriter, r *http.Request, target any) bool {
	mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mediaType != "application/json" {
		sendJSON(w, 415, map[string]any{"ok": false, "error": "Content-Type application/json requis"})
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 128<<10)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "requête invalide ou trop volumineuse"})
		return false
	}
	if err := d.Decode(new(any)); err != io.EOF {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "une seule requête JSON attendue"})
		return false
	}
	return true
}

func handleWorkspace(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodGet) {
		return
	}
	sendJSON(w, 200, map[string]any{
		"ok": true, "runtimes": runtimeCatalog(), "projects": listProjects(),
		"capabilities": listCapabilities(), "active_project": conv.currentProject(),
		"providers": workspaceSessions.providers(), "sessions": workspaceSessions.list(),
		"models": modelCatalog(workspaceSessions.providers()), "harness_profiles": harnessProfiles(),
	})
}

func handleProjectContext(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var p ChatProject
	if !workspaceDecode(w, r, &p) {
		return
	}
	saved, err := saveProjectContext(p)
	if err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "project": saved})
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
	go syncSkillSinks() // skills distributed to harnesses follow Loom's copy
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
	var c Capability
	if !getStoreJSON(bkCapabilities, req.ID, &c) {
		sendJSON(w, 404, map[string]any{"ok": false, "error": "capacité introuvable"})
		return
	}
	if err := putBytes(bkCapabilities, req.ID, nil); err != nil {
		sendJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	go syncSkillSinks()
	sendJSON(w, 200, map[string]any{"ok": true})
}
