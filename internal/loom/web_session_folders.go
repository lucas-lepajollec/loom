package loom

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
)

// Codeman sessions live on the Codeman server; Loom only remembers which of
// its folders (projects) each one is filed under, so agent sessions and
// discussions share the same folders in the sidebar.
const agentFoldersKey = "agent_session_folders"

var agentFoldersMu sync.Mutex

func loadAgentFolders() map[string]string {
	out := map[string]string{}
	getStoreJSON(bkState, agentFoldersKey, &out)
	return out
}

// handleAgentFolders:
//
//	GET  → {ok, folders: {sessionID: folderID}}
//	POST {id, folder} → files the session under folder ("" removes it)
func handleAgentFolders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		agentFoldersMu.Lock()
		folders := loadAgentFolders()
		agentFoldersMu.Unlock()
		sendJSON(w, 200, map[string]any{"ok": true, "folders": folders})
		return
	}
	var body struct {
		ID     string `json:"id"`
		Folder string `json:"folder"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	id, folder := strings.TrimSpace(body.ID), strings.TrimSpace(body.Folder)
	if id == "" || len(id) > 200 {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "session id required"})
		return
	}
	if folder != "" {
		if _, ok := getProject(folder); !ok {
			sendJSON(w, 404, map[string]any{"ok": false, "error": "folder not found"})
			return
		}
	}
	agentFoldersMu.Lock()
	defer agentFoldersMu.Unlock()
	folders := loadAgentFolders()
	if folder == "" {
		delete(folders, id)
	} else {
		folders[id] = folder
	}
	if err := putStoreJSON(bkState, agentFoldersKey, folders); err != nil {
		sendJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "folders": folders})
}
