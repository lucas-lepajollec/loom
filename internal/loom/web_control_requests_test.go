package loom

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestControlRequestValidationPrecedesLegacyActions(t *testing.T) {
	testHome(t)
	for _, worker := range []bool{false, true} {
		calls := 0
		mux := http.NewServeMux()
		h := func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(204) }
		if worker {
			mux.HandleFunc("/api/mcp/test", nodeAuth(hashWebKey("fixture"), controlRequests("/api/mcp/test", h)))
		} else {
			webAPI(mux)("/api/mcp/test", h)
		}
		for _, body := range []string{`{"name":`, `{} {}`, strings.Repeat(" ", (1<<20)+1)} {
			r := localTestRequest("POST", "/api/mcp/test", strings.NewReader(body))
			if worker {
				r.Header.Set("Authorization", "Bearer fixture")
			}
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			if (w.Code != 400 && w.Code != 413) || calls != 0 {
				t.Fatal("invalid request executed action", w.Code)
			}
		}
		r := localTestRequest("POST", "/api/mcp/test", strings.NewReader(`{"name":"fixture"}`))
		if worker {
			r.Header.Set("Authorization", "Bearer fixture")
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != 204 || calls != 1 {
			t.Fatal("valid request stopped working", w.Code)
		}
	}
	// The legacy dual read/write route must keep the inspector's read path.
	mux := http.NewServeMux()
	webAPI(mux)("/api/naked/remember", handleNakedRemember)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, localTestRequest("GET", "/api/naked/remember?model=fixture.gguf", nil))
	if w.Code != 200 {
		t.Fatal("remembered model inspection blocked", w.Code)
	}
	// Request body caps do not shrink the existing base64 file chunk workflow.
	called := false
	controlRequests("/api/chat/upload", func(w http.ResponseWriter, r *http.Request) { called = true; _, _ = io.Copy(io.Discard, r.Body) })(httptest.NewRecorder(), localTestRequest("POST", "/api/chat/upload", strings.NewReader(`{"data":"`+strings.Repeat("x", 2<<20)+`"}`)))
	if !called {
		t.Fatal("upload exception missing")
	}
}
