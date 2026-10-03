package loom

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"time"
)

// Legacy handlers sometimes ignore decoder failures. Validate and bound bodies
// before any action/proxy dispatch, so malformed or oversized requests cannot
// execute with zero-value defaults. Uploads retain their chunked file limit.
func controlRequests(path string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodPost {
			w.Header().Set("Allow", "GET, HEAD, POST")
			sendJSON(w, 405, map[string]any{"ok": false, "error": "method not allowed"})
			return
		}
		if r.Method == http.MethodPost && r.Body != nil {
			limit := int64(1 << 20)
			deadline := 30 * time.Second
			if path == "/api/chat/upload" {
				limit = 2 * uploadChunkMax
				deadline = 2 * time.Minute
			}
			controller := http.NewResponseController(w)
			_ = controller.SetReadDeadline(time.Now().Add(deadline))
			body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
			_ = controller.SetReadDeadline(time.Time{})
			_ = r.Body.Close()
			if err != nil {
				sendJSON(w, 413, map[string]any{"ok": false, "error": "request too large or unreadable"})
				return
			}
			trimmed := bytes.TrimSpace(body)
			if len(body) > 0 && (!json.Valid(body) || len(trimmed) == 0 || trimmed[0] != '{') {
				sendJSON(w, 400, map[string]any{"ok": false, "error": "invalid JSON request"})
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
		}
		next(w, r)
	}
}

// Preserve legacy empty-body actions and accepted fields while rejecting type
// errors. A valid JSON object is insufficient if decoding its fields fails.
func legacyControlDecode(w http.ResponseWriter, r *http.Request, target any) bool {
	err := json.NewDecoder(r.Body).Decode(target)
	if err == nil || err == io.EOF {
		return true
	}
	sendJSON(w, 400, map[string]any{"ok": false, "error": "invalid request fields"})
	return false
}
