package loom

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

func TestDiscussionContextConfigurePreservesHistoryAndRoute(t *testing.T) {
	testHome(t)
	m := newRuntimeSessions()
	skill, _ := saveCapability(Capability{Name: "Review", Instructions: "Check edge cases."})
	project, _ := saveProjectContext(ChatProject{Name: "Shared", Instructions: "Be precise.", Directory: t.TempDir(), CapabilityIDs: []string{skill.ID}})
	s, _ := m.create("", "", false)
	s.Messages = []Message{{Role: "user", Content: "Original question"}, {Role: "assistant", Content: "Original reply"}}
	s.Turns = []RuntimeTurnRecord{{MessageIndex: 1, RuntimeID: "llama.cpp", Model: "original"}}
	putStoreJSON(bkRuntimeSessions, s.ID, s)
	before := cloneRuntimeSession(s)
	updated, err := m.configureDiscussion(s.ID, "A named discussion", project.ID, "Only short examples.", discussionContext(s).Revision, false)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != s.ID || updated.Model != s.Model || updated.RuntimeID != s.RuntimeID || !reflect.DeepEqual(updated.Messages, before.Messages) || !reflect.DeepEqual(updated.Turns, before.Turns) {
		t.Fatal("configuration changed history, identity or route")
	}
	p := prepareDiscussion(updated, "  A draft  ")
	if p.Problem != "" || !p.DraftAdded || len(p.Messages) != 4 || p.HistoryCount != 2 || p.Messages[3].Content != "A draft" {
		t.Fatalf("bad preview: %+v", p)
	}
	if !strings.Contains(p.Context.System, skill.Instructions) || !strings.Contains(p.Context.System, updated.Instructions) || strings.Contains(p.Context.System, project.Directory) {
		t.Fatal("context missing selected instructions or leaking folder")
	}
	if !reflect.DeepEqual(updated.Messages, before.Messages) {
		t.Fatal("preview mutated history")
	}
	if _, err := m.configureDiscussion(s.ID, "Stale", "", "", discussionContext(before).Revision, false); err == nil {
		t.Fatal("stale config accepted")
	}
	if _, err := m.configureDiscussion(s.ID, "Valid", "missing", "", p.Context.Revision, false); err == nil {
		t.Fatal("unknown project accepted")
	}
	if _, err := m.configureDiscussion(s.ID, "Valid", project.ID, strings.Repeat("é", 6001), p.Context.Revision, false); err == nil {
		t.Fatal("oversized instructions accepted")
	}
	persisted, _ := newRuntimeSessions().get(s.ID)
	if persisted.Instructions != updated.Instructions || !persisted.CustomTitle {
		t.Fatal("configuration not durable")
	}
	if err := deleteProject(project.ID); err != nil {
		t.Fatal(err)
	}
	missing := prepareDiscussion(persisted, "Still here")
	if missing.Problem == "" {
		t.Fatal("deleted project silently ignored")
	}
	detached, err := m.configureDiscussion(s.ID, persisted.Title, "", persisted.Instructions, missing.Context.Revision, false)
	if err != nil || len(detached.Messages) != 2 {
		t.Fatalf("cannot recover existing thread: %v", err)
	}
	if next := prepareDiscussion(detached, "Hello"); next.Problem != "" || strings.Contains(next.Context.System, skill.Instructions) || !strings.Contains(next.Context.System, detached.Instructions) {
		t.Fatal("detachment lost discussion instructions or kept old project context")
	}
}

func TestDiscussionPreviewMatchesWireAndRejectsStaleContext(t *testing.T) {
	testHome(t)
	m := newRuntimeSessions()
	var calls atomic.Int32
	requests := make(chan []Message, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var body struct {
			Messages []Message `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		requests <- body.Messages
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseChunk("Answer")+"data: [DONE]\n\n")
	}))
	defer srv.Close()
	provider, _ := m.saveProvider(CloudProvider{Name: "Fixture", Endpoint: srv.URL, Model: "fixture"}, "fake-key")
	skill, _ := saveCapability(Capability{Name: "Shared", Instructions: "Original instruction"})
	project, _ := saveProjectContext(ChatProject{Name: "Work", CapabilityIDs: []string{skill.ID}})
	s, _ := m.create(project.ID, provider.ID, true)
	if _, err := m.configureDiscussion(s.ID, "My title", project.ID, "Additional context", discussionContext(s).Revision, false); err == nil {
		t.Fatal("cloud context changed without consent")
	}
	s, err := m.configureDiscussion(s.ID, "My title", project.ID, "Additional context", discussionContext(s).Revision, true)
	if err != nil {
		t.Fatal(err)
	}
	preview := prepareDiscussion(s, "Test draft")
	skill.Instructions = "Changed in another tab"
	if _, err := saveCapability(skill); err != nil {
		t.Fatal(err)
	}
	if err := m.start(s.ID, "stale-request", "Test draft", preview.Context.Revision); err == nil {
		t.Fatal("stale context sent")
	}
	if calls.Load() != 0 {
		t.Fatal("stale request contacted provider")
	}
	unchanged, _ := m.get(s.ID)
	if len(unchanged.Messages) != 0 || len(unchanged.RequestIDs) != 0 {
		t.Fatal("rejected send changed history")
	}
	preview = prepareDiscussion(unchanged, "Test draft")
	if err := m.start(s.ID, "stale-request", "Test draft", preview.Context.Revision); err != nil {
		t.Fatal(err)
	}
	finished := awaitCloudFinished(t, m, s.ID)
	if !reflect.DeepEqual(<-requests, preview.Messages) {
		t.Fatal("wire prompt differs from preview")
	}
	if finished.Title != "My title" || len(finished.Turns) != 1 || finished.Turns[0].InputBytes != preview.TextBytes || finished.Turns[0].ContextRevision != preview.Context.Revision {
		t.Fatal("lost explicit title or prompt provenance")
	}
	if err := m.start(s.ID, "stale-request", "Test draft", "stale-again"); err != nil || calls.Load() != 1 {
		t.Fatal("accepted request was not idempotent")
	}
	if err := m.start(s.ID, "new-request", "Next", preview.Context.Revision); err == nil {
		t.Fatal("stale history accepted")
	}
	if _, err := m.configureDiscussion(s.ID, "Stale", "", "", preview.Context.Revision, true); err == nil {
		t.Fatal("stale editor overwrote updated conversation")
	}
	current := prepareDiscussion(finished, "Next")
	if _, err := m.selectModel(s.ID, "local:missing", false); err == nil {
		t.Fatal("bad model accepted")
	}
	if calls.Load() != 1 || current.Context.Revision == preview.Context.Revision {
		t.Fatal("unexpected request or unchanged revision")
	}
}

func TestDiscussionConfigureRejectsActiveRun(t *testing.T) {
	testHome(t)
	m := newRuntimeSessions()
	s, _ := m.create("", "", false)
	m.runs[s.ID] = &runtimeRun{session: s}
	if _, err := m.configureDiscussion(s.ID, "Changed", "", "", discussionContext(s).Revision, false); err == nil {
		t.Fatal("active run overwritten")
	}
}

func TestDiscussionPreviewAPIReadOnlyAndAuthenticated(t *testing.T) {
	testHome(t)
	old := workspaceSessions
	workspaceSessions = newRuntimeSessions()
	t.Cleanup(func() { workspaceSessions = old })
	s, _ := workspaceSessions.create("", "", false)
	before := append([]byte{}, getBytes(bkRuntimeSessions, s.ID)...)
	storeWebKey("preview-test-key")
	mux := newWebMux()
	for _, path := range []string{"/api/runtime/sessions/configure", "/api/runtime/sessions/preview"} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", path, strings.NewReader("{}")))
		if w.Code != 401 {
			t.Fatalf("unprotected %s: %d", path, w.Code)
		}
	}
	request := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer preview-test-key")
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	w := request("POST", "/api/runtime/sessions/preview", fmt.Sprintf(`{"id":%q,"text":"private draft"}`, s.ID))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "private draft") || strings.Contains(w.Body.String(), "preview-test-key") {
		t.Fatalf("bad preview: %d", w.Code)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("private preview may be cached")
	}
	if !bytes.Equal(before, getBytes(bkRuntimeSessions, s.ID)) {
		t.Fatal("preview persisted a draft")
	}
	if w = request("GET", "/api/runtime/sessions/preview?id="+s.ID, ""); w.Code != 200 || strings.Contains(w.Body.String(), "private draft") {
		t.Fatal("GET preview retained draft")
	}
	if w = request("POST", "/api/runtime/sessions/preview", `{"id":"x","extra":true}`); w.Code != 400 {
		t.Fatal("unknown fields accepted")
	}
	if w = request("POST", "/api/runtime/sessions/configure", fmt.Sprintf(`{"id":%q,"title":"New","project_id":"","instructions":"","context_revision":"stale"}`, s.ID)); w.Code != 409 {
		t.Fatal("stale update accepted")
	}
	if w = request("POST", "/api/runtime/sessions/send", fmt.Sprintf(`{"id":%q,"request_id":"test-request","text":"draft"}`, s.ID)); w.Code != 409 {
		t.Fatal("missing revision accepted")
	}
	if !bytes.Equal(before, getBytes(bkRuntimeSessions, s.ID)) {
		t.Fatal("rejected operation changed record")
	}
}
