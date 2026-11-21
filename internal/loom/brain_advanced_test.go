package loom

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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
