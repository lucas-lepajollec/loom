package loom

import (
	"errors"
	"net/http"
	"strings"
)

// The page stays in the existing memory store; this is only an explicit
// selection policy, not a second global memory database.
type sharedPreferences struct {
	Page     string `json:"page"`
	External bool   `json:"external"`
}

const sharedPreferencesKey = "shared_preferences"

func globalPreferences(external bool) (string, error) {
	var selected sharedPreferences
	if !getStoreJSON(bkState, sharedPreferencesKey, &selected) || selected.Page == "" || (external && !selected.External) {
		return "", nil
	}
	if err := brainAvailable(); err != nil {
		return "", err
	}
	text := MemContent(selected.Page)
	if text == "" {
		return "", errors.New("the selected shared preference page is missing or unreadable")
	}
	if len(text) > 4000 {
		return "", errors.New("shared preferences exceed 4000 bytes; shorten the selected page")
	}
	return "Shared user preferences:\n" + text, nil
}
func handleSharedPreferences(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "POST" {
		workspaceMethod(w, r, "GET")
		return
	}
	if err := brainAvailable(); err != nil {
		brainResponse(w, nil, err)
		return
	}
	var selected sharedPreferences
	if r.Method == "POST" {
		if !workspaceDecode(w, r, &selected) {
			return
		}
		selected.Page = strings.TrimSpace(selected.Page)
		if selected.Page != "" {
			if _, err := memFileName(selected.Page); err != nil {
				brainResponse(w, nil, err)
				return
			}
			text := MemContent(selected.Page)
			if text == "" || len(text) > 4000 {
				brainResponse(w, nil, errors.New("select a readable preference page of at most 4000 bytes"))
				return
			}
		} else {
			selected.External = false
		}
		if err := putStoreJSON(bkState, sharedPreferencesKey, selected); err != nil {
			brainResponse(w, nil, err)
			return
		}
	} else {
		getStoreJSON(bkState, sharedPreferencesKey, &selected)
	}
	sendJSON(w, 200, map[string]any{"ok": true, "selection": selected})
}
