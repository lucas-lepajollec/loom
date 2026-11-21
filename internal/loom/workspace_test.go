package loom

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Seed the old on-disk representation to exercise migration behavior.
func legacyProjectFixture(p ChatProject) (ChatProject, error) {
	p.ID = newSessionID()
	return p, putStoreJSON(bkProjects, p.ID, p)
}

func TestWorkspaceHTTPBoundary(t *testing.T) {
	testHome(t)
	if err := storeWebKey("test-workspace-key"); err != nil {
		t.Fatal(err)
	}
	mux := newWebMux()
	for _, tc := range []struct {
		path, method, body, contentType, auth string
		want                                  int
	}{
		{"/api/workspace", "GET", "", "", "", 401},
		{"/api/workspace", "GET", "", "", "Bearer test-workspace-key", 200},
		{"/api/workspace", "POST", "{}", "application/json", "Bearer test-workspace-key", 405},
		{"/api/capabilities/save", "GET", "", "", "Bearer test-workspace-key", 405},
		{"/api/capabilities/save", "POST", `{"name":"Review","instructions":"Check."}`, "application/json", "", 401},
		{"/api/capabilities/save", "POST", `{"name":"Review","instructions":"Check."}`, "text/plain", "Bearer test-workspace-key", 415},
		{"/api/capabilities/save", "POST", `{"name":"Review","instructions":"Check.","unexpected":true}`, "application/json", "Bearer test-workspace-key", 400},
		{"/api/capabilities/save", "POST", `{"name":"Review","instructions":"Check."} {}`, "application/json", "Bearer test-workspace-key", 400},
		{"/api/capabilities/save", "POST", `{"name":"Review","instructions":"Check."}`, "application/json", "Bearer test-workspace-key", 200},
	} {
		r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		r.Header.Set("Content-Type", tc.contentType)
		r.Header.Set("Authorization", tc.auth)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Errorf("%s %s: got %d want %d: %s", tc.method, tc.path, w.Code, tc.want, w.Body.String())
		}
	}
	if len(listCapabilities()) != 1 {
		t.Fatal("invalid requests wrote capabilities")
	}
}

func TestLocalRuntimeDelegatesStreamingAndProjectContext(t *testing.T) {
	testHome(t)
	requests := make(chan []Message, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		requests <- req.Messages
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(sseChunk("Local response") + "data: [DONE]\n\n"))
	}))
	defer srv.Close()
	port := strings.TrimPrefix(srv.URL, "http://127.0.0.1:")
	if err := SetConfigKey("PORT", port); err != nil {
		t.Fatal(err)
	}
	var answer strings.Builder
	_, err := localChatRuntime().Run(context.Background(), RuntimeTurn{Messages: withProjectContext([]Message{{Role: "user", Content: "Hello"}}, "Project context."), Temperature: 0.7}, func(ev StreamEvent) bool { answer.WriteString(ev.Content); return true })
	if err != nil {
		t.Fatal(err)
	}
	received := <-requests
	if answer.String() != "Local response" || len(received) != 2 || received[0].Content != "Project context." {
		t.Fatalf("delegation failed: %q %+v", answer.String(), received)
	}
}
