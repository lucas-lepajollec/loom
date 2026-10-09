package loom

import (
	"net/http"
	"strings"
)

const chatDefaultChoiceKey = "chat.default_choice"

// Local library choices can load on demand; cloud/harness choices need a live
// connection. Hidden choices are not initial selector choices.
func availableInitialChoice(id string, choices []ModelChoice) *ModelChoice {
	for _, c := range choices {
		if c.ID == id && c.Enabled && (c.Kind == "local" || c.Ready) {
			return &c
		}
	}
	return nil
}

func initialDiscussionChoice(projectID, global string, choices []ModelChoice) *ModelChoice {
	if p, ok := getProject(projectID); ok && p.DefaultChoice != "" {
		return availableInitialChoice(p.DefaultChoice, choices)
	}
	return availableInitialChoice(global, choices)
}

// Settings are stored alongside the other config keys. Reads never bind or
// change an existing discussion. Empty default_choice restores normal selection.
func handleChatSettings(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodPost {
		var req struct {
			DefaultChoice *string `json:"default_choice"`
		}
		if !workspaceDecode(w, r, &req) {
			return
		}
		if req.DefaultChoice != nil {
			id := strings.TrimSpace(*req.DefaultChoice)
			if len(id) > 400 || (id != "" && availableInitialChoice(id, modelCatalog(workspaceSessions.providers())) == nil) {
				sendJSON(w, 400, map[string]any{"ok": false, "error": "default_choice must be an available selector choice ID"})
				return
			}
			if err := SetConfigKey(chatDefaultChoiceKey, id); err != nil {
				sendJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
				return
			}
		}
	} else if !workspaceMethod(w, r, http.MethodGet) {
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "default_choice": ReadConfig()[chatDefaultChoiceKey]})
}
