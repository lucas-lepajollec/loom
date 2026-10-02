package loom

import (
	"errors"
	"net/http"
)

func handleMCPFile(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodGet) {
		return
	}
	sendJSON(w, 200, mcpFileStatus())
}

// POST {path,label,action:"link"|"unlink"}. GET and POST return the same snapshot.
func handleMCPSources(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet {
		if !workspaceMethod(w, r, http.MethodPost) {
			return
		}
		var req struct {
			Path   string `json:"path"`
			Label  string `json:"label"`
			Action string `json:"action"`
		}
		if !workspaceDecode(w, r, &req) {
			return
		}
		if req.Action != "" && req.Action != "link" && req.Action != "unlink" {
			sendJSON(w, 400, map[string]any{"ok": false, "error": "action attendue : link ou unlink"})
			return
		}
		if err := linkMCPSource(MCPSource{Path: req.Path, Label: req.Label}, req.Action == "unlink"); err != nil {
			sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}
	}
	sources, suggested, err := mcpSourcesSnapshot()
	if err != nil {
		sendJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "sources": sources, "suggested": suggested})
}

func handleMCPSourceAdopt(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		Source  string `json:"source"`
		Name    string `json:"name"`
		WithEnv bool   `json:"with_env"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	name, missing, err := adoptLinkedMCP(req.Source, req.Name, req.WithEnv)
	if err != nil {
		code := http.StatusBadRequest
		if errors.Is(err, errMCPAdoptConflict) {
			code = http.StatusConflict
		}
		sendJSON(w, code, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "name": name, "env_to_fill": missing})
}
