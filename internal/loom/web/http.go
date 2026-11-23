// Package web contains HTTP plumbing with explicitly supplied application inputs.
package web

import (
	"encoding/json"
	"io"
	"mime"
	"net/http"
)

// ApplySecurityHeaders sets baseline security response headers across UI, API
// and static asset responses.
func ApplySecurityHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Permissions-Policy", "camera=(), geolocation=(), payment=(), usb=(), microphone=(self)")
}

// WithSecurityHeaders wraps an http.Handler to attach global security headers
// on every response and HSTS only when serving HTTPS with CA-signed files.
func WithSecurityHeaders(next http.Handler, hsts func(*http.Request) bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ApplySecurityHeaders(w)
		if r.TLS != nil && hsts != nil && hsts(r) {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

// SendJSON preserves the control API's content type and encoder wire format.
func SendJSON(w http.ResponseWriter, code int, v any) {
	ApplySecurityHeaders(w)
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
