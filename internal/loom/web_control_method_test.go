package loom

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLegacyControlActionsCannotRunThroughBrowserGETOrCrossOriginPOST(t *testing.T) {
	testHome(t)
	for _, path := range []string{"/api/start", "/api/stop", "/api/restart", "/api/unload", "/api/mem/lock", "/api/chat/reset", "/api/chat/stop", "/api/chat/history/clear", "/api/bench", "/api/llamacpp/update", "/api/mem/decrypt", "/api/mem/encrypt", "/api/mcp/test", "/api/agent/toggle", "/api/tools/toggle"} {
		t.Run(path, func(t *testing.T) {
			calls := 0
			mux := http.NewServeMux()
			webAPI(mux)(path, func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(204) })
			for _, method := range []string{"GET", "HEAD", "OPTIONS"} {
				r := localTestRequest(method, "http://127.0.0.1"+path, nil)
				r.Header.Set("Sec-Fetch-Site", "same-site")
				w := httptest.NewRecorder()
				mux.ServeHTTP(w, r)
				if w.Code != 405 || calls != 0 {
					t.Fatal("safe request executed a control action")
				}
			}
			r := localTestRequest("POST", "http://127.0.0.1"+path, nil)
			r.Header.Set("Origin", "http://127.0.0.1:5173")
			r.Header.Set("Sec-Fetch-Site", "same-site")
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			if w.Code != 403 || calls != 0 {
				t.Fatal("preview origin executed a control action")
			}
			r = localTestRequest("POST", "http://127.0.0.1"+path, nil)
			r.Header.Set("Origin", "http://127.0.0.1")
			r.Header.Set("Sec-Fetch-Site", "same-origin")
			w = httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			if w.Code != 204 || calls != 1 {
				t.Fatal("same-origin action stopped working")
			}
		})
	}
}
