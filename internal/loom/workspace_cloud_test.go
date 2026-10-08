package loom

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCloudEndpointValidation(t *testing.T) {
	for _, endpoint := range []string{"file:///etc/passwd", "http://example.com/v1", "https://user:password@example.com/v1", "https://example.com/v1?key=test", "https://example.com/v1#fragment", "https:///v1"} {
		if _, err := validateCloudEndpoint(endpoint); err == nil {
			t.Errorf("accepted %s", endpoint)
		}
	}
	for _, endpoint := range []string{"https://example.com/v1/", "http://127.0.0.1:9876/v1", "http://[::1]:9876/v1"} {
		if _, err := validateCloudEndpoint(endpoint); err != nil {
			t.Errorf("rejected %s: %v", endpoint, err)
		}
	}
}

func TestCloudAdapterStreamingUsageAndErrors(t *testing.T) {
	for _, tc := range []struct {
		name, body, contentType string
		status                  int
		wantError               bool
	}{
		{"complete", sseChunk("Hello") + "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":2,\"total_tokens\":9}}\n\ndata: [DONE]\n\n", "text/event-stream", 200, false},
		{"partial", sseChunk("Partial"), "text/event-stream", 200, true},
		{"invalid", "data: garbage\n\n", "text/event-stream", 200, true},
		{"upstream error", "data: {\"error\":{\"message\":\"private-key-value\"}}\n\n", "text/event-stream", 200, true},
		{"empty", "data: [DONE]\n\n", "text/event-stream", 200, true},
		{"not SSE", "{}", "application/json", 200, true},
		{"unauthorized", "private-key-value", "text/plain", 401, true},
		{"unsupported tools", "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{}]}}]}\n\ndata: [DONE]\n\n", "text/event-stream", 200, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer test-key" {
					t.Error("wrong endpoint or auth")
				}
				var req map[string]any
				if json.NewDecoder(r.Body).Decode(&req) != nil || req["model"] != "test-model" || req["stream"] != true {
					t.Error("wrong request")
				}
				if _, ok := req["tools"]; ok {
					t.Error("local tools leaked")
				}
				w.Header().Set("Content-Type", tc.contentType)
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer srv.Close()
			adapter := cloudRuntimeAdapter{provider: CloudProvider{Endpoint: srv.URL + "/v1", Model: "test-model"}, key: "test-key"}
			var content string
			var usage *RuntimeUsage
			result, err := adapter.Run(context.Background(), RuntimeTurn{Messages: []Message{{Role: "user", Content: "Hi"}}}, func(e StreamEvent) bool {
				content += e.Content
				if e.Usage != nil {
					usage = e.Usage
				}
				return true
			})
			if (err != nil) != tc.wantError {
				t.Fatalf("err=%v", err)
			}
			if err != nil && strings.Contains(err.Error(), "private-key-value") {
				t.Fatal("upstream secret leaked")
			}
			if !tc.wantError && (content != "Hello" || len(result) != 1 || usage == nil || usage.Total != 9) {
				t.Fatalf("bad stream: %q %+v %+v", content, result, usage)
			}
		})
	}
}

func TestCloudAdapterRejectsRedirect(t *testing.T) {
	var reached atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached.Store(true) }))
	defer target.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 307) }))
	defer srv.Close()
	_, err := (cloudRuntimeAdapter{provider: CloudProvider{Endpoint: srv.URL, Model: "test"}, key: "test"}).Run(context.Background(), RuntimeTurn{}, func(StreamEvent) bool { return true })
	if err == nil || reached.Load() {
		t.Fatal("followed redirect")
	}
}

func awaitCloudFinished(t *testing.T, m *runtimeSessions, id string) RuntimeSession {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s, ok := m.get(id)
		if ok && s.Status != "running" {
			return s
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("session did not finish")
	return RuntimeSession{}
}

func TestCloudSessionsIsolationConsentPersistenceAndReplay(t *testing.T) {
	testHome(t)
	m := newRuntimeSessions()
	var calls atomic.Int32
	requests := make(chan []Message, 5)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var body struct {
			Messages []Message `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		requests <- body.Messages
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseChunk("Response")+"data: [DONE]\n\n")
	}))
	defer srv.Close()
	p, err := m.saveProvider(CloudProvider{Name: "Test", Endpoint: srv.URL + "/v1", Model: "fixture"}, "test-session-secret")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(getBytes(bkProviders, p.ID)), "test-session-secret") {
		t.Fatal("secret persisted")
	}
	if !p.Ready {
		t.Fatal("missing ready state")
	}
	changed := p
	changed.Endpoint = "https://another.example/v1"
	if _, err := m.saveProvider(changed, ""); err == nil {
		t.Fatal("silently rerouted existing provider")
	}
	project, err := saveProjectContext(ChatProject{Name: "Shared", Instructions: "Explicit project context"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.create(project.ID, p.ID, false); err == nil {
		t.Fatal("consent bypass")
	}
	a, err := m.create(project.ID, p.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.create("", p.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.start(a.ID, "request-one", "First question"); err != nil {
		t.Fatal(err)
	}
	finished := awaitCloudFinished(t, m, a.ID)
	if finished.Status != "complete" || len(finished.Messages) != 2 {
		t.Fatalf("bad result: %+v", finished)
	}
	first := <-requests
	if len(first) != 2 || !strings.Contains(first[0].Content.(string), "Explicit project context") {
		t.Fatalf("missing project context: %+v", first)
	}
	if strings.Contains(fmt.Sprint(finished.Messages), "Explicit project context") {
		t.Fatal("system context persisted in history")
	}
	untouched, _ := m.get(b.ID)
	if len(untouched.Messages) != 0 {
		t.Fatal("session histories mixed")
	}
	if err := m.start(a.ID, "request-two", "Next question"); err != nil {
		t.Fatal(err)
	}
	awaitCloudFinished(t, m, a.ID)
	<-requests
	if err := m.start(a.ID, "request-one", "First question"); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatal("replayed request charged again")
	}
	if err := m.start(b.ID, "personal-request", "Personal"); err != nil {
		t.Fatal(err)
	}
	awaitCloudFinished(t, m, b.ID)
	personal := <-requests
	if len(personal) != 1 || personal[0].Role != "user" {
		t.Fatalf("project context leaked into personal chat: %+v", personal)
	}
	restarted := newRuntimeSessions()
	if len(restarted.list()) != 2 {
		t.Fatal("sessions not persisted")
	}
	if restarted.providers()[0].Ready {
		t.Fatal("credential survived restart")
	}
	if err := restarted.start(a.ID, "after-restart", "No secret"); err == nil {
		t.Fatal("ran without credential")
	}
	m.disconnect(p.ID)
	if m.providers()[0].Ready {
		t.Fatal("disconnect retained secret")
	}
}

func TestCloudSessionsCancelBusyAndRestart(t *testing.T) {
	testHome(t)
	m := newRuntimeSessions()
	started := make(chan struct{})
	cancelled := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseChunk("Partial"))
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
		close(cancelled)
	}))
	defer srv.Close()
	p, _ := m.saveProvider(CloudProvider{Name: "Cancel", Endpoint: srv.URL, Model: "fixture"}, "test")
	s, _ := m.create("", p.ID, true)
	if err := m.start(s.ID, "request-start", "Hello"); err != nil {
		t.Fatal(err)
	}
	<-started
	if err := m.start(s.ID, "request-conflict", "Other"); err == nil {
		t.Fatal("concurrent turn accepted")
	}
	if err := m.remove(s.ID); err == nil {
		t.Fatal("deleted running session")
	}
	recovered, ok := newRuntimeSessions().get(s.ID)
	if !ok || recovered.Status != "interrupted" {
		t.Fatal("restart not marked interrupted")
	}
	if err := m.stop(s.ID); err != nil {
		t.Fatal(err)
	}
	finished := awaitCloudFinished(t, m, s.ID)
	if finished.Status != "cancelled" {
		t.Fatalf("got %s", finished.Status)
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("upstream request not cancelled")
	}
	if err := m.remove(s.ID); err != nil {
		t.Fatal(err)
	}
}

func TestCloudHTTPAuthAndValidation(t *testing.T) {
	testHome(t)
	old := workspaceSessions
	workspaceSessions = newRuntimeSessions()
	defer func() { workspaceSessions = old }()
	storeWebKey("test-cloud-key")
	mux := newWebMux()
	for _, path := range []string{"/api/providers", "/api/providers/save", "/api/providers/disconnect", "/api/runtime/sessions", "/api/runtime/sessions/create", "/api/runtime/sessions/send", "/api/runtime/sessions/stop", "/api/runtime/sessions/delete"} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", path, strings.NewReader("{}")))
		if w.Code != 401 {
			t.Errorf("unauthenticated %s = %d", path, w.Code)
		}
	}
	for _, tc := range []struct {
		body, ct string
		status   int
	}{
		{`{"name":"Test","endpoint":"https://example.com/v1","model":"fixture","key":"test-secret"}`, "text/plain", 415},
		{`{"name":"Test","endpoint":"http://example.com/v1","model":"fixture","key":"test-secret"}`, "application/json", 400},
		{`{"name":"Test","endpoint":"https://example.com/v1","model":"fixture","key":"test-secret","extra":true}`, "application/json", 400},
		{`{"name":"Test","endpoint":"https://example.com/v1","model":"fixture","key":"test-secret"}`, "application/json", 200},
	} {
		r := httptest.NewRequest("POST", "/api/providers/save", strings.NewReader(tc.body))
		r.Header.Set("Authorization", "Bearer test-cloud-key")
		r.Header.Set("Content-Type", tc.ct)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Errorf("got %d want %d", w.Code, tc.status)
		}
		if strings.Contains(w.Body.String(), "test-secret") {
			t.Fatal("API echoed secret")
		}
	}
}
