package web

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"
)

// HashKey returns the persisted SHA-256 representation of a control key.
func HashKey(k string) string {
	sum := sha256.Sum256([]byte(k))
	return hex.EncodeToString(sum[:])
}

type ctxKey int

const e2eAuthedKey ctxKey = 1

func MarkE2EAuthed(r *http.Request) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), e2eAuthedKey, true))
}

func IsE2EAuthed(r *http.Request) bool {
	v, _ := r.Context().Value(e2eAuthedKey).(bool)
	return v
}

// ProtectOrigin retains the standard same-origin protection used by the MCP
// transport. The application chooses the routes to which it applies.
func ProtectOrigin(next http.Handler) http.Handler {
	return http.NewCrossOriginProtection().Handler(next)
}

// RequireAuth wraps an HTTP handler, rejecting requests that don't present
// the configured Bearer token. When no key is configured the handler is left
// open. The key reader is called for every request; errors close access.
func RequireAuth(next http.HandlerFunc, keyHash func() (string, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Requête arrivée par le tunnel loom.local : déjà authentifiée par l'identité
		// E2E de l'utilisateur (contexte non falsifiable). On n'exige pas la clé.
		if IsE2EAuthed(r) {
			next(w, r)
			return
		}
		hash, err := keyHash()
		if err != nil {
			// On ne sait pas si une clé protège cette API : on ferme.
			SendJSON(w, http.StatusServiceUnavailable,
				map[string]any{"error": "configuration unreadable — try again in a moment"})
			return
		}
		if hash == "" {
			next(w, r)
			return
		}
		if !CheckBearer(r, hash) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="loom"`)
			SendJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
			return
		}
		next(w, r)
	}
}

// CheckBearer reports whether the request carries a Bearer whose EMPREINTE égale
// wantHash. On ne compare jamais la clé en clair (le serveur ne la détient pas) :
// on hache le Bearer présenté et on compare à temps constant. PAS de repli
// ?key=<clé> en query string (fuite dans les logs proxy / l'historique).
func CheckBearer(r *http.Request, wantHash string) bool {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		got := HashKey(strings.TrimSpace(h[len("Bearer "):]))
		if subtle.ConstantTimeCompare([]byte(got), []byte(wantHash)) == 1 {
			return true
		}
	}
	return false
}
