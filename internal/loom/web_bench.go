package loom

import (
	"net/http"
)

func handleBenchTests(w http.ResponseWriter, r *http.Request) {
	if !isEngineWorker() && !usageVaultAccess(w) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		workspaceMethod(w, r, http.MethodGet)
		return
	}
	if r.Method == http.MethodPost {
		var req struct {
			Name      string `json:"name"`
			Prompt    string `json:"prompt"`
			MaxTokens int    `json:"max_tokens"`
		}
		if !workspaceDecode(w, r, &req) {
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
	if !workspaceMethod(w, r, http.MethodPost) || !workspaceDecode(w, r, &req) {
		return
	}
	if err := deleteCustomBenchTest(req.ID); err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "tests": listBenchTests()})
}

func handleBenchQueue(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		workspaceMethod(w, r, http.MethodGet)
		return
	}
	if !isEngineWorker() && !usageVaultAccess(w) {
		return
	}
	if r.Method == http.MethodPost {
		var req struct {
			TestID  string      `json:"test_id"`
			Models  []benchPick `json:"models"`
			Consent bool        `json:"consent"`
		}
		if !workspaceDecode(w, r, &req) {
			return
		}
		if err := benchCheckConsent(req.Models, req.Consent); err != nil {
			sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		j, err := startBenchQueue(req.TestID, req.Models, req.Consent)
		if err != nil {
			sendJSON(w, 409, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		sendJSON(w, 200, map[string]any{"ok": true, "job": j, "scoped_cancel": true})
		return
	}
	j := recoverBenchJob()
	if r.URL.Query().Get("compact") == "1" && j != nil {
		for i := range j.Rows {
			j.Rows[i].Output = ""
		}
	}
	sendJSON(w, 200, map[string]any{"ok": true, "job": j, "busy": benchBusy.Load(), "scoped_cancel": true})
}

func handleBenchQueueCancel(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	jobID := r.Header.Get("X-Loom-Bench-Job")
	j := cancelBenchQueueID(jobID)
	if jobID != "" && (j == nil || j.ID != jobID) {
		sendJSON(w, 409, map[string]any{"ok": false, "error": "benchmark queue changed"})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "job": j, "scoped_cancel": true})
}

func handleBenchRuns(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodGet) {
		return
	}
	if !isEngineWorker() && !usageVaultAccess(w) {
		return
	}
	runs := loadBenchRuns()
	if id := r.URL.Query().Get("id"); id != "" {
		filtered := []benchJob{}
		for _, run := range runs {
			if run.ID == id {
				filtered = append(filtered, run)
				break
			}
		}
		runs = filtered
	}
	if r.URL.Query().Get("compact") == "1" {
		for i := range runs {
			for k := range runs[i].Rows {
				runs[i].Rows[k].Output = ""
			}
		}
	}
	sendJSON(w, 200, map[string]any{"ok": true, "runs": runs})
}
