package loom

import (
	"errors"
	"net/http"
)

func handleProviders(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodGet) {
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "providers": workspaceSessions.providers()})
}
func handleProviderSave(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		ID        string   `json:"id"`
		Name      string   `json:"name"`
		Endpoint  string   `json:"endpoint"`
		Model     string   `json:"model"`
		Models    []string `json:"models"`
		Key       string   `json:"key"`
		UsageMode string   `json:"usage_mode"`
		Remember  *bool    `json:"remember"` // nil keeps the current choice
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	remember := false
	if req.Remember != nil {
		remember = *req.Remember
	} else if req.ID != "" {
		var old CloudProvider
		if getStoreJSON(bkProviders, req.ID, &old) {
			remember = old.Remember
		}
	}
	p, err := workspaceSessions.saveProvider(CloudProvider{ID: req.ID, Name: req.Name, Endpoint: req.Endpoint, Model: req.Model, Models: req.Models, UsageMode: req.UsageMode, Remember: remember}, req.Key)
	if err == nil || errors.Is(err, errNoKeychain) {
		resyncHarnessSources() // Pi/OpenCode list Loom's providers
	}
	if errors.Is(err, errNoKeychain) {
		// Saved and usable; only the "remember" part failed.
		sendJSON(w, 200, map[string]any{"ok": true, "provider": p, "warning": err.Error()})
		return
	}
	if err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "provider": p})
}
func handleProviderDisconnect(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	if err := workspaceSessions.disconnect(req.ID); err != nil {
		sendJSON(w, 409, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	resyncHarnessSources()
	sendJSON(w, 200, map[string]any{"ok": true})
}
func handleRuntimeSessions(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodGet) {
		return
	}
	if id := r.URL.Query().Get("id"); id != "" {
		theBrain().discussionViewed(id)
		s, ok := workspaceSessions.get(id)
		if !ok {
			sendJSON(w, 404, map[string]any{"ok": false, "error": "discussion not found or locked"})
			return
		}
		c := discussionContext(s)
		sendJSON(w, 200, map[string]any{"ok": true, "session": clientSession(s), "shared_context": c.System, "context": c})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "sessions": workspaceSessions.list()})
}
func handleRuntimeSessionCreate(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		ProjectID  string `json:"project_id"`
		ProviderID string `json:"provider_id"`
		Consent    bool   `json:"consent"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	s, err := workspaceSessions.create(req.ProjectID, req.ProviderID, req.Consent)
	if err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "session": clientSession(s)})
}
func handleRuntimeSessionSend(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		ID              string `json:"id"`
		RequestID       string `json:"request_id"`
		Text            string `json:"text"`
		ContextRevision string `json:"context_revision"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	if err := workspaceSessions.start(req.ID, req.RequestID, req.Text, req.ContextRevision); err != nil {
		out := map[string]any{"ok": false, "error": err.Error()}
		if errors.Is(err, errContextChanged) {
			out["code"] = "context_changed"
		}
		sendJSON(w, 409, out)
		return
	}
	sendJSON(w, 202, map[string]any{"ok": true})
}
func handleRuntimeSessionStop(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	if err := workspaceSessions.stop(req.ID); err != nil {
		sendJSON(w, 409, map[string]any{"ok": false, "error": "could not save: unlock storage"})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true})
}
func handleRuntimeSessionDelete(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	if err := workspaceSessions.remove(req.ID); err != nil {
		sendJSON(w, 409, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true})
}

func handleRuntimeSessionSelect(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		ID              string `json:"id"`
		ChoiceID        string `json:"choice_id"`
		Consent         bool   `json:"consent"`
		ReasoningEffort string `json:"reasoning_effort"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	s, err := workspaceSessions.selectModelContext(r.Context(), req.ID, req.ChoiceID, req.Consent, req.ReasoningEffort)
	if err != nil {
		sendJSON(w, 409, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "session": clientSession(s), "context": discussionContext(s)})
}

func handleRuntimeSessionConfigure(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		acpConfiguration
		ID           string `json:"id"`
		Title        string `json:"title"`
		ProjectID    string `json:"project_id"`
		Instructions string `json:"instructions"`
		Revision     string `json:"context_revision"`
		Consent      bool   `json:"consent"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	if req.present() && req.Revision == "" {
		current, ok := workspaceSessions.get(req.ID)
		if !ok {
			sendJSON(w, 404, map[string]any{"ok": false, "error": "discussion not found"})
			return
		}
		// Harness patches preserve discussion text/context. Unchanged legacy
		// fields are harmless; portable edits require the existing revision.
		if req.Title != "" && req.Title != current.Title || req.ProjectID != "" && req.ProjectID != current.ProjectID || req.Instructions != "" && req.Instructions != current.Instructions {
			sendJSON(w, 409, map[string]any{"ok": false, "error": "context_revision required"})
			return
		}
		req.Title, req.ProjectID, req.Instructions = current.Title, current.ProjectID, current.Instructions
		req.Revision = discussionContext(current).Revision
	}
	s, err := workspaceSessions.configureDiscussion(req.ID, req.Title, req.ProjectID, req.Instructions, req.Revision, req.Consent, req.acpConfiguration)
	if err != nil {
		sendJSON(w, 409, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "session": clientSession(s), "context": discussionContext(s)})
}

// Preview is read-only despite POST: drafts stay in the request, never in the
// database or provider logs. It uses precisely the same assembler as start.
func handleRuntimeSessionPreview(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var req struct {
		ID   string `json:"id"`
		Text string `json:"text"`
	}
	if r.Method == http.MethodGet {
		req.ID = r.URL.Query().Get("id")
	} else {
		if !workspaceMethod(w, r, http.MethodPost) || !workspaceDecode(w, r, &req) {
			return
		}
	}
	s, ok := workspaceSessions.get(req.ID)
	if !ok {
		sendJSON(w, 404, map[string]any{"ok": false, "error": "discussion not found or locked"})
		return
	}
	p := prepareDiscussion(s, req.Text)
	sendJSON(w, 200, map[string]any{"ok": true, "preview": p, "runtime_id": s.RuntimeID, "provider_id": s.ProviderID, "provider_name": s.ProviderName, "model": s.Model, "endpoint": s.Endpoint, "reasoning_effort": s.ReasoningEffort, "running": s.Status == "running"})
}
func handleModelChoice(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		ID      string `json:"id"`
		Enabled bool   `json:"enabled"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	found := false
	for _, c := range modelCatalog(workspaceSessions.providers()) {
		if c.ID == req.ID {
			found = true
		}
	}
	if !found {
		sendJSON(w, 404, map[string]any{"ok": false, "error": "model not found"})
		return
	}
	value := "hidden"
	if req.Enabled {
		value = "visible"
	}
	if err := putStr(bkModelChoices, req.ID, value); err != nil {
		sendJSON(w, 500, map[string]any{"ok": false, "error": "could not save"})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true})
}
func handleHarnessProfileSave(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var p HarnessProfile
	if !workspaceDecode(w, r, &p) {
		return
	}
	saved, err := saveHarnessProfile(p)
	if err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "profile": saved})
}
func handleRuntimeSessionImport(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	s, err := workspaceSessions.importArchive(req.ID)
	if err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "session": clientSession(s)})
}
