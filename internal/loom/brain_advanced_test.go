package loom

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/brain"
)

// Socket-free httptest server transport also runs in restrictive sandboxes.
type brainFakeModelTransport struct{ handler http.Handler }

func (t brainFakeModelTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	w := httptest.NewRecorder()
	t.handler.ServeHTTP(w, r)
	return w.Result(), nil
}
func brainFakeModelClient(t *testing.T, h http.HandlerFunc) {
	t.Helper()
	old := brainModelClient
	brainModelClient = &http.Client{Transport: brainFakeModelTransport{h}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	t.Cleanup(func() { brainModelClient = old })
}
func TestBrainCloudEmbeddingConsentProviderAndValidation(t *testing.T) {
	testHome(t)
	oldDelete := keyringDelete
	keyringDelete = func(string) error { return nil }
	t.Cleanup(func() { keyringDelete = oldDelete })
	p, err := workspaceSessions.saveProvider(CloudProvider{Name: "Embed", Endpoint: "https://embedding.example/v1", Model: "embed-model"}, "private-key")
	if err != nil {
		t.Fatal(err)
	}
	m, err := newBrainSemantic(brainStorage{filepath.Join(LoomHome(), "brain")})
	if err != nil {
		t.Fatal(err)
	}
	req := brainSemanticRequest{Action: "enable", ProviderID: p.ID, Model: "embed-model"}
	if m.configure(req) == nil {
		t.Fatal("cloud selection without consent")
	}
	if m.configCopy().Enabled {
		t.Fatal("failed consent mutated settings")
	}
	req.Consent = true
	if err = m.configure(req); err != nil {
		t.Fatal(err)
	}
	calls := 0
	brainFakeModelClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.String() != p.Endpoint+"/embeddings" || r.Header.Get("Authorization") != "Bearer private-key" {
			t.Fatal("wrong destination or credential")
		}
		var body struct {
			Input []string `json:"input"`
			Model string   `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if len(body.Input) != 2 || body.Input[0] != "note one" || body.Model != "embed-model" {
			t.Fatalf("%+v", body)
		}
		io.WriteString(w, `{"data":[{"index":1,"embedding":[0,1]},{"index":0,"embedding":[1,0]}]}`)
	})
	vecs, err := m.embed(context.Background(), []string{"note one", "note two"}, false)
	if err != nil || vecs[0][0] != 1 || vecs[1][1] != 1 {
		t.Fatalf("%v %v", vecs, err)
	}
	restored, err := newBrainSemantic(m.storage)
	if err != nil || !restored.configCopy().Consent {
		t.Fatalf("consent not stored: %v", err)
	}
	if err = m.configure(brainSemanticRequest{Action: "disable"}); err != nil {
		t.Fatal(err)
	}
	if _, err = m.embed(context.Background(), []string{"private"}, false); err == nil || calls != 1 {
		t.Fatal("disabled source called provider")
	}
	req.Consent = true
	if err = m.configure(req); err != nil {
		t.Fatal(err)
	}
	workspaceSessions.disconnect(p.ID)
	if _, err = m.embed(context.Background(), []string{"private"}, false); err == nil || calls != 1 {
		t.Fatal("disconnected provider called")
	}
}
func TestBrainEmbeddingRejectsMalformedVectors(t *testing.T) {
	testHome(t)
	oldDelete := keyringDelete
	keyringDelete = func(string) error { return nil }
	t.Cleanup(func() { keyringDelete = oldDelete })
	p, err := workspaceSessions.saveProvider(CloudProvider{Name: "Embed", Endpoint: "https://embedding.example/v1", Model: "embed"}, "key")
	if err != nil {
		t.Fatal(err)
	}
	m, _ := newBrainSemantic(brainStorage{filepath.Join(LoomHome(), "brain")})
	if err = m.configure(brainSemanticRequest{Action: "enable", ProviderID: p.ID, Model: "embed", Consent: true}); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{`{"data":[]}`, `{"data":[{"index":2,"embedding":[1]}]}`, `{"data":[{"index":0,"embedding":[0,0]}]}`, `{"data":[{"index":0,"embedding":[1e999]}]}`} {
		brainFakeModelClient(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, body) })
		if _, err = m.embed(context.Background(), []string{"note"}, false); err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
}
func TestBrainDistillationRetryStrictParsingAndProvenance(t *testing.T) {
	d := brainDistillDiscussion{ID: "discussion-one", Date: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), Messages: []Message{{Role: "system", Content: "hidden instruction"}, {Role: "user", Content: "Prefer concise reports"}, {Role: "tool", Content: "hidden tool output"}, {Role: "assistant", Content: "Recorded"}}}
	calls := 0
	brainFakeModelClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer engine-key" {
			t.Fatal("chat route")
		}
		b, _ := io.ReadAll(r.Body)
		if strings.Contains(string(b), "hidden instruction") || strings.Contains(string(b), "hidden tool output") || !strings.Contains(string(b), `"temperature":0.1`) || !strings.Contains(string(b), `"model":"active-model"`) {
			t.Fatal(string(b))
		}
		raw := "bad JSON"
		if calls == 2 {
			raw = `{"items":[{"kind":"preference","text":"Prefers concise reports","message_index":1}]}`
		}
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": raw}}}})
	})
	items, err := brainDistillWithChat(context.Background(), "http://localhost", "engine-key", "active-model", d)
	if err != nil || calls != 2 || len(items) != 1 || items[0].Source.DiscussionID != d.ID || items[0].Source.MessageIndex != 1 || !items[0].Date.Equal(d.Date) {
		t.Fatalf("%+v calls %d %v", items, calls, err)
	}
	for _, raw := range []string{`{"items":[{"kind":"fact","text":"x","message_index":2}]}`, `{"items":[{"kind":"fact","text":"x"}]}`, `{"items":[{"kind":"other","text":"x","message_index":1}]}`, `{"items":[],"extra":true}`, "```json\n{\"items\":[]}\n```", `{"items":[]} {}`, `{}`} {
		if _, err := brainParseDistillation(raw, d, map[int]bool{1: true, 3: true}); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	if _, err := brainParseDistillation(`{"items":[]}`, d, map[int]bool{}); err != nil {
		t.Fatal(err)
	}
}
func TestBrainDistilledHTTPBuiltinDeleteAndAdvancedAuth(t *testing.T) {
	testHome(t)
	if err := storeWebKey("control"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	registerBrainRoutes(mux, nil)
	for _, route := range []string{"semantic", "distill", "distilled", "distilled/delete", "distilled/review"} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/brain/"+route, strings.NewReader(`{}`)))
		if w.Code != 401 {
			t.Fatalf("unprotected %s %d", route, w.Code)
		}
	}
	s := theBrain()
	item := brainDistilledItem{ID: "test-item", Kind: "decision", Text: "Use durableword storage", Source: brainDistilledSource{"discussion", 4}, Date: time.Now().UTC()}
	s.distillMu.Lock()
	err := s.saveDistilledLocked([]brainDistilledItem{item})
	s.distillMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	e, err := s.get()
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	hits, err := s.Search(brain.SearchRequest{Query: "durableword", Sources: []string{"distilled"}})
	if err != nil || len(hits) != 1 {
		t.Fatalf("%+v %v", hits, err)
	}
	if e.Remove("distilled") == nil || e.Relabel("distilled", "other") == nil {
		t.Fatal("builtin writable")
	}
	for _, route := range []string{"distilled", "distilled/delete"} {
		method, body := "GET", ""
		if route == "distilled/delete" {
			method = "POST"
			body = `{"id":"test-item"}`
		}
		req := httptest.NewRequest(method, "/api/brain/"+route, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer control")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
	}
	hits, err = s.Search(brain.SearchRequest{Query: "durableword"})
	if err != nil || len(hits) != 0 {
		t.Fatalf("deleted item searchable: %v %v", hits, err)
	}
}

func TestBrainVectorCheckpointsResumeAndModelInvalidation(t *testing.T) {
	testHome(t)
	storage := brainStorage{filepath.Join(LoomHome(), "brain")}
	m, err := newBrainSemantic(storage)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.configure(brainSemanticRequest{Action: "enable", Model: "nomic"}); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	err = m.saveVectorsLocked()
	if err == nil {
		err = m.checkpointLocked(map[string][]float32{"first": {1, 0}})
	}
	if err == nil {
		err = m.checkpointLocked(map[string][]float32{"second": {0, 1}})
	}
	m.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := newBrainSemantic(storage)
	if err != nil || len(resumed.vectors.Values) != 2 {
		t.Fatalf("checkpoint lost: %v", err)
	}
	// Partial last checkpoint is recoverable without losing completed batches.
	data, err := storage.read("vectors.bin", 256<<20)
	if err != nil {
		t.Fatal(err)
	}
	if err = storage.write("vectors.bin", append(data, 0xff, 0x01)); err != nil {
		t.Fatal(err)
	}
	resumed, err = newBrainSemantic(storage)
	if err != nil || len(resumed.vectors.Values) != 2 || resumed.lastError == "" {
		t.Fatalf("crash resume: %+v %v", resumed, err)
	}
	if err = resumed.configure(brainSemanticRequest{Action: "enable", Model: "bge-small"}); err != nil {
		t.Fatal(err)
	}
	resumed, err = newBrainSemantic(storage)
	if err != nil || len(resumed.vectors.Values) != 0 {
		t.Fatalf("old model vectors retained: %v", err)
	}
}
func TestBrainDistillDiscussionSelectionSinceAndBoundNativeProvenance(t *testing.T) {
	testHome(t)
	oldConv := conv
	conv = newTestConv()
	t.Cleanup(func() { conv = oldConv })
	date := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)
	a := convArchive{ID: "native-source", SavedAt: date.UnixMilli(), Log: []LogEvent{{Delta: map[string]any{"user": "durable choice", "thinking": "hidden"}}, {Delta: map[string]any{"content": "visible answer"}}}}
	if err := putStoreJSON(bkChatHist, a.ID, a); err != nil {
		t.Fatal(err)
	}
	s := RuntimeSession{ID: "discussion-id", NativeArchive: a.ID, UpdatedAt: date.UnixMilli()}
	if err := putStoreJSON(bkRuntimeSessions, s.ID, s); err != nil {
		t.Fatal(err)
	}
	got, err := brainDistillDiscussions(context.Background(), brainDistillRequest{Since: "2026-09-01"})
	if err != nil || len(got) != 1 || got[0].ID != s.ID || len(got[0].Messages) != 2 || got[0].Messages[0].Content != "durable choice" {
		t.Fatalf("%+v %v", got, err)
	}
	got, err = brainDistillDiscussions(context.Background(), brainDistillRequest{DiscussionID: s.ID})
	if err != nil || len(got) != 1 || !got[0].Date.Equal(date) {
		t.Fatalf("%+v %v", got, err)
	}
	got, err = brainDistillDiscussions(context.Background(), brainDistillRequest{Since: "2026-09-06"})
	if err != nil || len(got) != 0 {
		t.Fatalf("%+v %v", got, err)
	}
	for _, req := range []brainDistillRequest{{}, {DiscussionID: s.ID, Since: "2026-09-01"}, {Since: "wrong"}, {DiscussionID: "missing"}} {
		if _, err := brainDistillDiscussions(context.Background(), req); err == nil {
			t.Fatalf("invalid selection %+v", req)
		}
	}
}

func TestBrainDistillHTTPUsesSelectedEngineAndRequiresRemoteConsent(t *testing.T) {
	testHome(t)
	oldConv := conv
	conv = newTestConv()
	t.Cleanup(func() { conv = oldConv })
	oldNode := currentEngineNode()
	if err := setEngineNode(&engineNode{URL: "https://chat.example", V1: "https://chat.example", Direct: true, Model: "chosen-model", APIKey: "engine-secret"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = setEngineNode(oldNode) })
	date := time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)
	if err := putStoreJSON(bkRuntimeSessions, "selected", RuntimeSession{ID: "selected", UpdatedAt: date.UnixMilli(), Messages: []Message{{Role: "user", Content: "Choose local backups"}}}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	brainFakeModelClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		body, _ := io.ReadAll(r.Body)
		if r.URL.String() != "https://chat.example/v1/chat/completions" || !strings.Contains(string(body), `"model":"chosen-model"`) || r.Header.Get("Authorization") != "Bearer engine-secret" {
			t.Fatal("wrong selected engine")
		}
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": `{"items":[{"kind":"decision","text":"Use local backups","message_index":0}]}`}}}})
	})
	s := newBrainService(LoomHome())
	call := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/api/brain/distill", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.distillHTTP(w, req)
		return w
	}
	if w := call(`{"discussion_id":"selected"}`); w.Code != 400 || calls != 0 {
		t.Fatalf("missing remote consent: %d calls %d", w.Code, calls)
	}
	w := call(`{"discussion_id":"selected","consent":true}`)
	if w.Code != 200 || calls != 1 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	s.distillMu.Lock()
	items, err := s.loadDistilledLocked()
	s.distillMu.Unlock()
	if err != nil || len(items) != 1 || items[0].Source.DiscussionID != "selected" || items[0].Source.MessageIndex != 0 || !items[0].Date.Equal(date) {
		t.Fatalf("%+v %v", items, err)
	}
	hits, err := s.Search(brain.SearchRequest{Query: "backups", Sources: []string{"distilled"}})
	if err != nil || len(hits) != 0 || items[0].Review != "pending" {
		t.Fatalf("unreviewed model output indexed: %v %v", hits, err)
	}
	review := func(status, text string) {
		body, _ := json.Marshal(map[string]string{"id": items[0].ID, "review": status, "text": text})
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/brain/distilled/review", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		s.reviewDistilledHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("review: %d %s", w.Code, w.Body)
		}
	}
	review("accepted", "Use reviewed backups")
	hits, err = s.Search(brain.SearchRequest{Query: "reviewed", Sources: []string{"distilled"}})
	if err != nil || len(hits) != 1 {
		t.Fatalf("approved correction not indexed: %v %v", hits, err)
	}
	review("rejected", "Use reviewed backups")
	hits, err = s.Search(brain.SearchRequest{Query: "reviewed", Sources: []string{"distilled"}})
	if err != nil || len(hits) != 0 {
		t.Fatal("rejected suggestion retained in context")
	}
}

func TestAutomaticSemanticRefreshOnlyEmbedsChangedSelectedText(t *testing.T) {
	testHome(t)
	oldDelete := keyringDelete
	keyringDelete = func(string) error { return nil }
	t.Cleanup(func() { keyringDelete = oldDelete })
	oldSessions := workspaceSessions
	workspaceSessions = newRuntimeSessions()
	t.Cleanup(func() { workspaceSessions = oldSessions })
	p, err := workspaceSessions.saveProvider(CloudProvider{Name: "Embed fixture", Endpoint: "https://embedding.example/v1", Model: "embedding"}, "fixture-key")
	if err != nil {
		t.Fatal(err)
	}
	s := newBrainService(LoomHome())
	m, err := s.semanticManager()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.close)
	if err = m.configure(brainSemanticRequest{Action: "enable", ProviderID: p.ID, Model: "embedding", Consent: true, Sources: []string{"conversations"}}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	brainFakeModelClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		var request struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		data := make([]any, len(request.Input))
		for i := range data {
			data[i] = map[string]any{"index": i, "embedding": []int{1, 0}}
		}
		json.NewEncoder(w).Encode(map[string]any{"data": data})
	})
	save := func(text string) {
		session := RuntimeSession{ID: "selected", Messages: []Message{{Role: "user", Content: text}}}
		if err := putStoreJSON(bkRuntimeSessions, "selected", session); err != nil {
			t.Fatal(err)
		}
		store, err := s.memoryStore()
		if err != nil {
			t.Fatal(err)
		}
		if _, err = store.WriteTranscript(sessionTranscript(session)); err != nil {
			t.Fatal(err)
		}
		e, err := s.get()
		if err != nil {
			t.Fatal(err)
		}
		if err = e.Refresh(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	wait := func() {
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			m.mu.Lock()
			running := m.indexing
			lastError := m.lastError
			m.mu.Unlock()
			if !running {
				if lastError != "" {
					t.Fatal(lastError)
				}
				return
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatal("fixture index did not finish")
	}
	save("Initial passage")
	s.refreshSemanticIfSelected()
	if calls != 0 {
		t.Fatal("automatic indexing started without opt-in")
	}
	on := true
	if err = m.configure(brainSemanticRequest{Action: "auto", AutoIndex: &on}); err != nil {
		t.Fatal(err)
	}
	s.refreshSemanticIfSelected()
	wait()
	if calls == 0 {
		t.Fatal("selected missing passage not embedded")
	}
	initialCalls := calls
	s.refreshSemanticIfSelected()
	wait()
	if calls != initialCalls {
		t.Fatal("unchanged index repeated embedding requests")
	}
	save("A changed passage")
	s.refreshSemanticIfSelected()
	wait()
	if calls <= initialCalls {
		t.Fatal("changed passage did not update embeddings")
	}
	changedCalls := calls
	off := false
	if err = m.configure(brainSemanticRequest{Action: "auto", AutoIndex: &off}); err != nil {
		t.Fatal(err)
	}
	save("Another change")
	s.refreshSemanticIfSelected()
	if calls != changedCalls {
		t.Fatal("automatic indexing continued after opt-out")
	}
	m.close()
}

func TestAutomaticSemanticDefaultScopeDoesNotExpandOnLink(t *testing.T) {
	testHome(t)
	brainSvcMu.Lock()
	old := brainSvc
	brainSvc = newBrainService(LoomHome())
	brainSvcMu.Unlock()
	t.Cleanup(func() {
		brainSvcMu.Lock()
		brainSvc = old
		brainSvcMu.Unlock()
	})
	e, err := theBrain().get()
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Update(brain.Source{ID: "personal-vault", Label: "Private", Path: t.TempDir(), Kind: "personal"}); err != nil {
		t.Fatal(err)
	}
	m, err := theBrain().semanticManager()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.close)
	if err = m.configure(brainSemanticRequest{Action: "enable", Model: "nomic"}); err != nil {
		t.Fatal(err)
	}
	on := true
	if err = m.configure(brainSemanticRequest{Action: "auto", AutoIndex: &on}); err != nil {
		t.Fatal(err)
	}
	frozen := m.configCopy().Sources
	if !hasName(frozen, "conversations") || hasName(frozen, "personal-vault") {
		t.Fatal("automatic default scope must freeze non-personal sources")
	}
	if err = e.Update(brain.Source{ID: "later-source", Label: "Later", Path: t.TempDir(), Kind: "context"}); err != nil {
		t.Fatal(err)
	}
	if hasName(m.configCopy().Sources, "later-source") {
		t.Fatal("linking a source silently widened automatic embedding consent")
	}
}
