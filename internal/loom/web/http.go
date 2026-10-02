// Package web contains HTTP plumbing with explicitly supplied application inputs.
package web

import (
	"encoding/json"
	"io"
	"mime"
	"net/http"
)

// SendJSON preserves the control API's content type and encoder wire format.
func SendJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func RequireMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == method {
		return true
	}
	w.Header().Set("Allow", method)
	SendJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false, "error": "method not allowed"})
	return false
}

func DecodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mediaType != "application/json" {
		SendJSON(w, 415, map[string]any{"ok": false, "error": "Content-Type application/json required"})
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 128<<10)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		SendJSON(w, 400, map[string]any{"ok": false, "error": "invalid or oversized request"})
		return false
	}
	if err := d.Decode(new(any)); err != io.EOF {
		SendJSON(w, 400, map[string]any{"ok": false, "error": "expected a single JSON request"})
		return false
	}
	return true
}
