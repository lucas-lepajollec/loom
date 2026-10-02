package web

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthenticationBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, hash, bearer, query string
		readError, e2e            bool
		status                    int
	}{
		{"open", "", "", "", false, false, 204},
		{"valid", HashKey("secret"), "Bearer secret", "", false, false, 204},
		{"trimmed", HashKey("secret"), "Bearer  secret \t", "", false, false, 204},
		{"missing", HashKey("secret"), "", "", false, false, 401},
		{"wrong", HashKey("secret"), "Bearer wrong", "", false, false, 401},
		{"case sensitive", HashKey("secret"), "bearer secret", "", false, false, 401},
		{"query rejected", HashKey("secret"), "", "?key=secret", false, false, 401},
		{"read fails closed", "", "", "", true, false, 503},
		{"e2e skips read", HashKey("secret"), "", "", true, true, 204},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reads, calls := 0, 0
			h := RequireAuth(func(w http.ResponseWriter, _ *http.Request) { calls++; w.WriteHeader(204) }, func() (string, error) {
				reads++
				if tc.readError {
					return tc.hash, errors.New("private storage details")
				}
				return tc.hash, nil
			})
			r := httptest.NewRequest("GET", "/api/test"+tc.query, nil)
			r.Header.Set("Authorization", tc.bearer)
			r.Header.Set("X-E2E-Authed", "true") // HTTP headers cannot mark the context.
			if tc.e2e {
				r = MarkE2EAuthed(r)
			}
			w := httptest.NewRecorder()
			h(w, r)
			if w.Code != tc.status || (calls == 1) != (tc.status == 204) || (reads == 0) != tc.e2e {
				t.Fatalf("auth boundary changed: code=%d calls=%d reads=%d", w.Code, calls, reads)
			}
			if tc.status == 401 && (w.Header().Get("WWW-Authenticate") != `Bearer realm="loom"` || w.Body.String() != "{\"error\":\"non autorisé\"}\n") {
				t.Fatalf("401 wire changed: %v %q", w.Header(), w.Body.String())
			}
			if tc.status == 503 && w.Body.String() != "{\"error\":\"configuration illisible — réessaie dans un instant\"}\n" {
				t.Fatalf("503 wire changed: %q", w.Body.String())
			}
		})
	}
}

func TestAPIAuthPrecedesApplicationWrapperAndRereadsKey(t *testing.T) {
	mux := http.NewServeMux()
	hash := HashKey("first")
	var calls []string
	api := API(mux, func() (string, error) { return hash, nil }, func(path string, next http.HandlerFunc) http.HandlerFunc {
		if path != "/api/test/{id}" {
			t.Fatal(path)
		}
		return func(w http.ResponseWriter, r *http.Request) {
			calls = append(calls, "wrapper")
			next(w, r)
		}
	})
	api("/api/test/{id}", func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.PathValue("id"))
		w.WriteHeader(204)
	})
	for i, token := range []string{"first", "first", "second"} {
		if i == 1 {
			hash = HashKey("second")
		}
		r := httptest.NewRequest("POST", "/api/test/42", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		want := 204
		if i == 1 {
			want = 401
		}
		if w.Code != want {
			t.Fatalf("rotated key: %d", w.Code)
		}
	}
	if len(calls) != 4 || calls[0] != "wrapper" || calls[1] != "42" || calls[2] != "wrapper" || calls[3] != "42" {
		t.Fatal(calls)
	}
}

func TestTransportOriginChecks(t *testing.T) {
	h := ProtectOrigin(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
	for _, tc := range []struct {
		method, origin, fetchSite string
		status                    int
	}{
		{"POST", "https://loom.example", "", 204},
		{"POST", "https://foreign.example", "", 403},
		{"POST", "", "cross-site", 403},
		{"GET", "https://foreign.example", "", 204},
	} {
		r := httptest.NewRequest(tc.method, "https://loom.example/mcp/brain", nil)
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("Sec-Fetch-Site", tc.fetchSite)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("origin %s %s: %d", tc.method, tc.origin, w.Code)
		}
	}
	// Upgrade rejection happens without opening sockets or granting a ticket.
	r := httptest.NewRequest("GET", "https://loom.example/api/terminals/ws", nil)
	r.Header.Set("Connection", "Upgrade")
	r.Header.Set("Upgrade", "websocket")
	r.Header.Set("Sec-WebSocket-Version", "13")
	r.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	r.Header.Set("Origin", "https://foreign.example")
	w := httptest.NewRecorder()
	conn, err := AcceptWebSocket(w, r, 1<<20)
	if conn != nil || err == nil || w.Code != 403 {
		t.Fatalf("websocket origin: %v %v %d", conn, err, w.Code)
	}
}
