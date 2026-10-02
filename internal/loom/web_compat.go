package loom

import (
	"embed"
	"net/http"
	"sync"

	"github.com/lucas-lepajollec/loom/internal/loom/web"
)

// Historical HTTP names delegate to the leaf. Embedding stays beside ui/;
// key persistence, route policy and application handlers remain in Loom.

//go:embed ui/marked.min.js ui/sw.js ui/manifest.webmanifest ui/offline.html ui/fonts
var uiFS embed.FS

//go:embed ui/next
var nextFS embed.FS

var pwaIcons = sync.OnceValue(func() map[string][]byte {
	icons := map[string][]byte{}
	for path, size := range map[string]int{"/icons/loom-180.png": 180, "/icons/loom-192.png": 192, "/icons/loom-512.png": 512} {
		icons[path] = loomIcon(size)
	}
	return icons
})

func webAssets() web.Assets                              { return web.Assets{UI: uiFS, NextFS: nextFS, Icons: pwaIcons} }
func handleIndex(w http.ResponseWriter, r *http.Request) { webAssets().Index(w, r) }
func handleNext(w http.ResponseWriter, r *http.Request)  { webAssets().Next(w, r) }
func registerPWAAssets(mux *http.ServeMux)               { webAssets().RegisterPWA(mux) }
func registerWebAssets(mux *http.ServeMux)               { webAssets().Register(mux) }
func webAPI(mux *http.ServeMux) func(string, http.HandlerFunc) {
	return web.API(mux, webKeyHashErr, nodeAware)
}
func sendJSON(w http.ResponseWriter, code int, v any) { web.SendJSON(w, code, v) }
func workspaceMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	return web.RequireMethod(w, r, method)
}
func workspaceDecode(w http.ResponseWriter, r *http.Request, target any) bool {
	return web.DecodeJSON(w, r, target)
}
func hashWebKey(key string) string                  { return web.HashKey(key) }
func checkBearer(r *http.Request, hash string) bool { return web.CheckBearer(r, hash) }
func markE2EAuthed(r *http.Request) *http.Request   { return web.MarkE2EAuthed(r) }
func isE2EAuthed(r *http.Request) bool              { return web.IsE2EAuthed(r) }
func requireWebAuth(next http.HandlerFunc) http.HandlerFunc {
	return web.RequireAuth(next, webKeyHashErr)
}
func sseHeartbeat(w http.ResponseWriter, flusher http.Flusher) (*sync.Mutex, func()) {
	return web.SSEHeartbeat(w, flusher)
}
