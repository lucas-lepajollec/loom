package loom

import "net/http"

// Explicit tombstones prevent old clients mistaking the SPA fallback for a
// successful installation, account connection or native transcript import.
func registerRetiredIntegrations(mux *http.ServeMux) {
	api := webAPI(mux)
	for _, path := range []string{"/api/agents/", "/api/harness/", "/api/harness-history", "/api/harness-history/", "/api/runtimes/", "/api/brain/agents", "/api/skills/targets", "/api/usage/native"} {
		api(path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			sendJSON(w, http.StatusGone, map[string]any{"ok": false, "error": "coding harness integrations have been removed; use a Local or Cloud model"})
		})
	}
}
