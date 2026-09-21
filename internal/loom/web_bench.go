package loom

import (
	"encoding/json"
	"net/http"
)

func handleBenchTests(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		var req struct {
			Name      string `json:"name"`
			Prompt    string `json:"prompt"`
			MaxTokens int    `json:"max_tokens"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		t, err := saveCustomBenchTest(req.Name, req.Prompt, req.MaxTokens)
		if err != nil {
			sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		sendJSON(w, 200, map[string]any{"ok": true, "test": t, "tests": listBenchTests()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "tests": listBenchTests()})
}

func handleBenchTestsDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if err := deleteCustomBenchTest(req.ID); err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "tests": listBenchTests()})
}

func handleBenchQueue(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		var req struct {
			TestID string      `json:"test_id"`
			Models []benchPick `json:"models"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		j, err := startBenchQueue(req.TestID, req.Models)
		if err != nil {
			sendJSON(w, 409, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		sendJSON(w, 200, map[string]any{"ok": true, "job": j})
		return
	}
	j := loadBenchJob()
	sendJSON(w, 200, map[string]any{"ok": true, "job": j, "busy": benchBusy.Load()})
}

func handleBenchQueueCancel(w http.ResponseWriter, r *http.Request) {
	j := cancelBenchQueue()
	sendJSON(w, 200, map[string]any{"ok": true, "job": j})
}

func handleBenchRuns(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, 200, map[string]any{"ok": true, "runs": loadBenchRuns()})
}
