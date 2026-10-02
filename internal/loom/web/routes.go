package web

import "net/http"

// API returns the protected route registrar. The application supplies its key
// reader and optional per-route wrapper; authentication remains outermost.
func API(mux *http.ServeMux, keyHash func() (string, error), wrap func(string, http.HandlerFunc) http.HandlerFunc) func(string, http.HandlerFunc) {
	return func(path string, handler http.HandlerFunc) {
		if wrap != nil {
			handler = wrap(path, handler)
		}
		mux.HandleFunc(path, RequireAuth(handler, keyHash))
	}
}
