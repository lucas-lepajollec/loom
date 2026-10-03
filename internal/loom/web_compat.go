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
	return func(path string, handler http.HandlerFunc) {
		if path == "/api/chat" || path == "/api/discussion/events" {
			handler = revocableControlStream(handler)
		}
		if controlActionRequiresPost(path) {
			next := handler
			handler = func(w http.ResponseWriter, r *http.Request) {
				if workspaceMethod(w, r, http.MethodPost) {
					next(w, r)
				}
			}
		}
		mux.HandleFunc(path, requireWebAuth(controlRequests(path, nodeAware(path, handler))))
	}
}

// Legacy control handlers predate method checks. GET must stay inert: an
// application on another origin can load a URL with ambient same-site cookies,
// whereas POST passes through the browser origin protection. Dual read/write
// routes retain their own method dispatch; modern handlers validate internally.
func controlActionRequiresPost(path string) bool {
	switch path {
	case "/api/start", "/api/stop", "/api/restart", "/api/unload",
		"/api/switch", "/api/load-model", "/api/apply",
		"/api/preset/save", "/api/preset/delete", "/api/models/delete",
		"/api/models/download/cancel", "/api/engines/vllm/download/cancel",
		"/api/llamacpp/check", "/api/llamacpp/install", "/api/llamacpp/install-custom",
		"/api/llamacpp/uninstall-custom", "/api/llamacpp/update", "/api/llamacpp/prebuilt",
		"/api/llamacpp/prebuilt/check", "/api/llamacpp/use", "/api/llamacpp/job/dismiss",
		"/api/mem/lock", "/api/skills/toggle", "/api/bench", "/api/bench/queue/cancel",
		"/api/chat/stop", "/api/chat/reset", "/api/chat/compact", "/api/chat/history/clear":
		return true
	case "/api/reasoning", "/api/presets/order", "/api/agent/toggle", "/api/tools/toggle", "/api/agent/compact",
		"/api/mcp/save", "/api/mcp/delete", "/api/mcp/toggle", "/api/mcp/tool", "/api/mcp/test",
		"/api/mem/save", "/api/mem/delete", "/api/mem/encrypt", "/api/mem/decrypt", "/api/mem/unlock", "/api/mem/addkey":
		return true
	case "/api/chat/send", "/api/chat/upload", "/api/chat/history/restore", "/api/chat/history/delete",
		"/api/chat/history/rename", "/api/chat/history/fav", "/api/chat/history/move",
		"/api/projects/rename", "/api/projects/delete", "/api/bench/tests/delete":
		return true
	}
	return false
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
	return requireBrowserOrWebKey(next)
}
func sseHeartbeat(w http.ResponseWriter, flusher http.Flusher) (*sync.Mutex, func()) {
	return web.SSEHeartbeat(w, flusher)
}
