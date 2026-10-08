package loom

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/brain"
)

func consolidationRequest(t *testing.T, handler http.HandlerFunc, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler(w, r)
	return w
}
func TestBrainConsolidationSettingsAndCollection(t *testing.T) {
	testHome(t)
	s := theBrain()
	call := func(method, body string) *httptest.ResponseRecorder {
		return consolidationRequest(t, s.consolidationHTTP, method, "/api/brain/consolidation", body)
	}
	if w := call("GET", ""); w.Code != 200 || strings.TrimSpace(w.Body.String()) != `{"auto_candidates":true}` {
		t.Fatalf("default: %d %s", w.Code, w.Body)
	}
	for _, body := range []string{`{}`, `{"auto_candidates":null}`, `{"auto_candidates":"false"}`, `{"auto_candidates":false,"extra":1}`} {
		if w := call("POST", body); w.Code != 400 {
			t.Fatalf("invalid settings: %s %d", body, w.Code)
		}
	}
	session := RuntimeSession{ID: "discussion", RuntimeID: "agent-one", ProjectID: "project-one"}
	if w := call("POST", `{"auto_candidates":false}`); w.Code != 200 {
		t.Fatal(w.Body)
	}
	collectDiscussionCandidates(session, 3, "Remember our release procedure.")
	s.candidateWrites.Wait()
	list, err := s.ListMemory(brain.MemoryFilter{Status: "candidate"})
	if err != nil || len(list.Items) != 0 {
		t.Fatalf("disabled: %+v %v", list, err)
	}
	if w := consolidationRequest(t, newBrainService(LoomHome()).consolidationHTTP, "GET", "/api/brain/consolidation", ""); !strings.Contains(w.Body.String(), `"auto_candidates":false`) {
		t.Fatal("opt-out not persisted")
	}
	call("POST", `{"auto_candidates":true}`)
	collectDiscussionCandidates(session, 3, "Remember our release procedure.")
	s.candidateWrites.Wait()
	list, err = s.ListMemory(brain.MemoryFilter{Status: "candidate"})
	if err != nil || len(list.Items) != 1 {
		t.Fatalf("enabled: %+v %v", list, err)
	}
	item := list.Items[0]
	if item.Scope != "project:project-one" || item.Provenance.Kind != "discussion" || item.Provenance.DiscussionID != session.ID || item.Provenance.Agent != session.RuntimeID || item.Provenance.MessageIndex == nil || *item.Provenance.MessageIndex != 3 {
		t.Fatalf("metadata: %+v", item)
	}
	if err := s.collectCandidates(RuntimeSession{ID: "unbound"}, 0, "I prefer simple reports."); err != nil {
		t.Fatal(err)
	}
	list, _ = s.ListMemory(brain.MemoryFilter{Status: "candidate", Query: "simple reports"})
	if len(list.Items) != 1 || list.Items[0].Scope != "global" {
		t.Fatalf("global scope: %+v", list)
	}
	if err := SetConfigKey("MEM_ENCRYPTED", "on"); err != nil {
		t.Fatal(err)
	}
	clearMemDEK()
	if err := s.collectCandidates(session, 4, "Never create memories while locked."); !errors.Is(err, errMemLocked) {
		t.Fatalf("locked: %v", err)
	}
	if w := call("POST", `{"auto_candidates":false}`); w.Code != 423 {
		t.Fatalf("locked settings: %d %s", w.Code, w.Body)
	}
	SetConfigKey("MEM_ENCRYPTED", "off")
	list, _ = s.ListMemory(brain.MemoryFilter{Status: "candidate"})
	if len(list.Items) != 2 {
		t.Fatal("locked collection wrote memory")
	}
}
func TestDiscussionSendCollectsOnlyAcceptedUserText(t *testing.T) {
	testHome(t)
	s := theBrain()
	m := newRuntimeSessions()
	adapter := memoryContextAdapter{id: "candidate-fixture", run: func(turn RuntimeTurn, emit ChatCallback) {
		emit(StreamEvent{Content: "Remember this assistant statement."})
	}}
	isolateRuntimeRegistry(t, adapter)
	session := RuntimeSession{ID: "candidate-send", RuntimeID: adapter.id, Title: "Test", Status: "idle", Instructions: "Always follow this system instruction.", Messages: []Message{{Role: "user", Content: "Old text"}, {Role: "assistant", Content: "Old answer"}}}
	if err := putStoreJSON(bkRuntimeSessions, session.ID, session); err != nil {
		t.Fatal(err)
	}
	text := "Je préfère les réponses courtes."
	preview := prepareDiscussion(session, text)
	if err := m.start(session.ID, "rejected-request", text, "stale"); err == nil {
		t.Fatal("stale send accepted")
	}
	s.candidateWrites.Wait()
	list, err := s.ListMemory(brain.MemoryFilter{Status: "candidate"})
	if err != nil || len(list.Items) != 0 {
		t.Fatal("preview/rejected send collected candidates")
	}
	if err := m.start(session.ID, "accepted-request", text, preview.Context.Revision); err != nil {
		t.Fatal(err)
	}
	awaitCloudFinished(t, m, session.ID)
	s.candidateWrites.Wait()
	list, err = s.ListMemory(brain.MemoryFilter{Status: "candidate"})
	if err != nil || len(list.Items) != 1 || list.Items[0].Text != text || *list.Items[0].Provenance.MessageIndex != 2 {
		t.Fatalf("user text/provenance: %+v %v", list, err)
	}
	if err := m.start(session.ID, "accepted-request", text, "stale"); err != nil {
		t.Fatal(err)
	}
	s.candidateWrites.Wait()
	list, _ = s.ListMemory(brain.MemoryFilter{Status: "candidate"})
	if len(list.Items) != 1 {
		t.Fatal("idempotent send duplicated candidates")
	}
}
func TestBrainConsolidateHTTP(t *testing.T) {
	testHome(t)
	oldConv := conv
	conv = newTestConv()
	t.Cleanup(func() { conv = oldConv })
	oldNode := currentEngineNode()
	if err := setEngineNode(&engineNode{URL: "https://chat.example", V1: "https://chat.example", Direct: true, Model: "chosen-model", APIKey: "engine-secret"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = setEngineNode(oldNode) })
	session := RuntimeSession{ID: "selected", RuntimeID: "fixture-agent", ProjectID: "project-one", UpdatedAt: time.Now().UnixMilli(), Messages: []Message{{Role: "system", Content: "hidden system text"}, {Role: "user", Content: "Choose local backups"}, {Role: "tool", Content: "hidden tool text"}, {Role: "assistant", Content: "Public answer"}}}
	if err := putStoreJSON(bkRuntimeSessions, session.ID, session); err != nil {
		t.Fatal(err)
	}
	calls := 0
	brainFakeModelClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		body, _ := io.ReadAll(r.Body)
		if r.URL.String() != "https://chat.example/v1/chat/completions" || !strings.Contains(string(body), `"model":"chosen-model"`) || r.Header.Get("Authorization") != "Bearer engine-secret" || strings.Contains(string(body), "hidden") {
			t.Fatal("wrong selected engine or private text shared")
		}
		raw := `{"items":[{"kind":"decision","text":"Use local backups","message_index":1},{"kind":"fact","text":"Backups run nightly","message_index":1},{"kind":"preference","text":"Prefer concise reports","message_index":3},{"kind":"todo","text":"Validate backup restore","message_index":1},{"kind":"fact","text":"EXISTING  FACT!","message_index":1}]}`
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": raw}}}})
	})
	s := newBrainService(LoomHome())
	primaryMemoryVault(t, s)
	if _, err := s.Remember(brainItemRequest("Existing fact.")); err != nil {
		t.Fatal(err)
	}
	call := func(body string) *httptest.ResponseRecorder {
		return consolidationRequest(t, s.consolidateHTTP, "POST", "/api/brain/consolidate", body)
	}
	for _, body := range []string{`{}`, `{"discussion_id":"selected"}`, `{"discussion_id":"missing","consent":true}`, `{"discussion_id":"selected","since":"2026-01-01","consent":true}`} {
		if w := call(body); w.Code != 400 || calls != 0 {
			t.Fatalf("invalid selection/consent: %d %s calls %d", w.Code, w.Body, calls)
		}
	}
	w := call(`{"discussion_id":"selected","consent":true}`)
	var result struct {
		OK    bool               `json:"ok"`
		Items []brain.MemoryItem `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || w.Code != 200 || !result.OK || len(result.Items) != 4 || calls != 1 {
		t.Fatalf("%d %s %v", w.Code, w.Body, err)
	}
	classes := []string{"episodic", "semantic", "semantic", "working"}
	for i, item := range result.Items {
		if item.Class != classes[i] || item.Status != "candidate" || item.Scope != "project:project-one" || item.Provenance.Kind != "distilled" || item.Provenance.DiscussionID != "selected" || item.Provenance.MessageIndex == nil || item.Provenance.Agent != session.RuntimeID {
			t.Fatalf("mapping/provenance: %+v", item)
		}
		if _, err := os.Stat(brainItemPath(s.itemsDir, ".loom", item)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(s.storage.dir, "distilled.json")); !os.IsNotExist(err) {
		t.Fatalf("legacy distilled store written: %v", err)
	}
	list, err := s.ListMemory(brain.MemoryFilter{})
	if err != nil || len(list.Items) != 1 {
		t.Fatal("candidate in default list")
	}
	if pack := brain.SelectMemory(result.Items, session.ProjectID, session.RuntimeID, "backup restore", "", brain.DefaultMemoryBudgets()); len(pack.Items) != 0 {
		t.Fatal("unreviewed model output in context")
	}
	w = call(`{"discussion_id":"selected","consent":true}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"items":[]`) {
		t.Fatalf("dedupe: %d %s", w.Code, w.Body)
	}
	active := "active"
	if _, err := s.UpdateMemory(brain.UpdateMemoryRequest{ID: result.Items[0].ID, Patch: brain.MemoryPatch{Status: &active}}); err != nil {
		t.Fatal(err)
	}
	list, _ = s.ListMemory(brain.MemoryFilter{})
	if len(list.Items) != 2 {
		t.Fatal("accept failed")
	}
	store, err := s.memoryStore()
	if err != nil {
		t.Fatal(err)
	}
	drafts := []brain.CandidateDraft{}
	for i := 0; i < 50; i++ {
		drafts = append(drafts, brain.CandidateDraft{Class: "semantic", Text: strings.Repeat("x", i+1)})
	}
	if _, err := store.AddCandidates(drafts, "global", brain.MemoryProvenance{Kind: "discussion"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ForgetMemory(brain.ForgetMemoryRequest{ID: result.Items[0].ID}); err != nil {
		t.Fatal(err)
	}
	w = call(`{"discussion_id":"selected","consent":true}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"items":[]`) {
		t.Fatalf("pending cap: %d %s", w.Code, w.Body)
	}
}
func TestBrainConsolidationRoutesAuth(t *testing.T) {
	testHome(t)
	if err := storeWebKey("control"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	registerBrainRoutes(mux, nil)
	for _, path := range []string{"consolidate", "consolidation"} {
		w := consolidationRequest(t, mux.ServeHTTP, "POST", "/api/brain/"+path, `{}`)
		if w.Code != 401 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("unprotected %s: %d", path, w.Code)
		}
	}
	r := httptest.NewRequest("GET", "/api/brain/consolidation", nil)
	r.Header.Set("Authorization", "Bearer control")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"auto_candidates":true`) {
		t.Fatalf("authenticated config: %d %s", w.Code, w.Body)
	}
}
func TestBrainConsolidateLocalEngineWithoutConsent(t *testing.T) {
	testHome(t)
	oldConv := conv
	conv = newTestConv()
	t.Cleanup(func() { conv = oldConv })
	oldNode := currentEngineNode()
	if err := setEngineNode(&engineNode{URL: "http://localhost", V1: "http://localhost", Direct: true, Model: "local-model"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = setEngineNode(oldNode) })
	if err := putStoreJSON(bkRuntimeSessions, "local", RuntimeSession{ID: "local", Messages: []Message{{Role: "user", Content: "Public local text"}}}); err != nil {
		t.Fatal(err)
	}
	brainFakeModelClient(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"choices":[{"message":{"content":"{\"items\":[]}"}}]}`)
	})
	w := consolidationRequest(t, theBrain().consolidateHTTP, "POST", "/api/brain/consolidate", `{"discussion_id":"local"}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"items":[]`) {
		t.Fatalf("local consent: %d %s", w.Code, w.Body)
	}
	if err := SetConfigKey("MEM_ENCRYPTED", "on"); err != nil {
		t.Fatal(err)
	}
	clearMemDEK()
	w = consolidationRequest(t, theBrain().consolidateHTTP, "POST", "/api/brain/consolidate", `{"discussion_id":"local"}`)
	if w.Code != 423 {
		t.Fatalf("locked consolidate: %d %s", w.Code, w.Body)
	}
}
func TestNativeCandidateDiscussionAndPortableIndex(t *testing.T) {
	testHome(t)
	oldSessions := workspaceSessions
	workspaceSessions = newRuntimeSessions()
	t.Cleanup(func() { workspaceSessions = oldSessions })
	session := RuntimeSession{ID: "bound", NativeArchive: "native", ProjectID: "project", RuntimeID: "llama.cpp"}
	if err := putStoreJSON(bkRuntimeSessions, session.ID, session); err != nil {
		t.Fatal(err)
	}
	log := []LogEvent{{Delta: map[string]any{"user": "First"}}, {Delta: map[string]any{"thinking": "private"}}, {Delta: map[string]any{"content": "Answer "}}, {Delta: map[string]any{"content": "continued"}}, {Delta: map[string]any{"tool": "private"}}}
	index := nativePortableMessageCount(log, nil)
	if index != len(archivePortableText(&convArchive{Log: log})) || index != 2 {
		t.Fatalf("portable index: %d", index)
	}
	collectNativeCandidates("native", "project", index, "Never publish without tests.")
	s := theBrain()
	s.candidateWrites.Wait()
	list, err := s.ListMemory(brain.MemoryFilter{Status: "candidate"})
	if err != nil || len(list.Items) != 1 || list.Items[0].Provenance.DiscussionID != "bound" || *list.Items[0].Provenance.MessageIndex != 2 {
		t.Fatalf("native provenance: %+v %v", list, err)
	}
	if _, err := s.get(); err != nil {
		t.Fatal(err)
	}
	if err := s.engine.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	hits, err := s.Search(brain.SearchRequest{Query: "publish"})
	if err != nil || len(hits) != 0 {
		t.Fatal("candidate searchable through vault index")
	}
}
