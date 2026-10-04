package loom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/brain"
	"github.com/lucas-lepajollec/loom/internal/loom/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	bolt "go.etcd.io/bbolt"
)

// A real Streamable HTTP MCP client driven through the mux, without sockets.
type brainMuxTransport struct {
	mux http.Handler
	key string
}

func TestBrainRecordsBatchingStopAndReadOnlyArchives(t *testing.T) {
	testHome(t)
	if err := store.Update(dbPath(), bkChatHist, func(b *bolt.Bucket) error {
		for i := 0; i < 130; i++ {
			id := fmt.Sprintf("brain-%03d", i)
			a := convArchive{ID: id, Title: "Stored", Log: []LogEvent{{Delta: map[string]any{"user": "visibleword", "thinking": "hiddenword"}}}}
			data, err := json.Marshal(a)
			if err != nil {
				return err
			}
			if err = b.Put([]byte(id), data); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	count := 0
	complete, err := brainRecords(context.Background(), bkChatHist, func(id string, data []byte) bool {
		count++
		// A callback can use storage: the batch transaction must be closed.
		_ = getBytes(bkChatMeta, id)
		return true
	})
	if err != nil || !complete || count != 130 {
		t.Fatalf("batches: %d %v %v", count, complete, err)
	}
	count = 0
	complete, err = brainRecords(context.Background(), bkChatHist, func(string, []byte) bool { count++; return false })
	if err != nil || complete || count != 1 {
		t.Fatalf("early stop: %d %v %v", count, complete, err)
	}
	if err = brainConversations(context.Background(), func(d brain.Document) bool {
		if strings.Contains(d.Text, "hiddenword") {
			t.Fatal("reasoning indexed")
		}
		return true
	}); err != nil {
		t.Fatal(err)
	}
	if len(allKV(bkChatMeta)) != 0 {
		t.Fatal("Brain migrated archive metadata during a read")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = brainRecords(ctx, bkChatHist, func(string, []byte) bool { t.Fatal("cancelled scan emitted text"); return true }); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
}

func (t brainMuxTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+t.key)
	w := httptest.NewRecorder()
	t.mux.ServeHTTP(w, r)
	return w.Result(), nil
}
func TestBrainHTTPAuthCRUDPackAndStreamableMCP(t *testing.T) {
	testHome(t)
	if err := storeWebKey("brain-test-key"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	registerBrainRoutes(mux, nil)
	call := func(method, url, body, key string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, url, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		return w
	}
	for _, route := range []string{"/api/brain/sources", "/api/brain/search", "/api/brain/read", "/api/brain/pack", "/api/brain/reindex", "/mcp/brain"} {
		for _, key := range []string{"", "wrong"} {
			if w := call("GET", route, "", key); w.Code != 401 {
				t.Fatalf("unprotected %s: %d", route, w.Code)
			}
		}
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte("# Déploiement\nUnicorn context"), 0600); err != nil {
		t.Fatal(err)
	}
	definition := map[string]any{"action": "add", "id": "project", "label": "Project", "path": dir, "kind": "context", "permission": "write", "primary": true, "include": []string{"**/*.md"}}
	body, _ := json.Marshal(definition)
	w := call("POST", "/api/brain/sources", string(body), "brain-test-key")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	w = call("POST", "/api/brain/reindex", "", "brain-test-key")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"ok":true`) {
		t.Fatal(w.Body.String())
	}
	w = call("GET", "/api/brain/search?query=deploiement&sources=project", "", "brain-test-key")
	var result brain.SearchResult
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || len(result.Hits) != 1 {
		t.Fatalf("search: %s %v", w.Body, err)
	}
	w = call("GET", "/api/brain/read?chunk_id="+result.Hits[0].ChunkID, "", "brain-test-key")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Unicorn") {
		t.Fatal(w.Body.String())
	}
	w = call("POST", "/api/brain/pack", `{"query":"unicorn","budget_tokens":100,"sources":["project"]}`, "brain-test-key")
	var pack brain.Pack
	if err := json.Unmarshal(w.Body.Bytes(), &pack); err != nil || pack.TokensUsed > 100 || len(pack.Citations) != 1 {
		t.Fatalf("pack: %s %v", w.Body, err)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("context cacheable")
	}
	w = call("POST", "/api/brain/sources", `{"action":"relabel","id":"project","label":"Renamed"}`, "brain-test-key")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Renamed") {
		t.Fatal(w.Body.String())
	}
	// Relabelling preserves the index immediately.
	w = call("GET", "/api/brain/search?query=unicorn&sources=project", "", "brain-test-key")
	if !strings.Contains(w.Body.String(), "notes.md") {
		t.Fatal("relabel discarded index")
	}
	for _, request := range [][3]string{{"POST", "/api/brain/pack", `{"query":"unicorn","budget_tokens":8001}`}, {"POST", "/api/brain/sources", `{"action":"remove","id":"memory"}`}, {"GET", "/api/brain/search?limit=oops", ""}, {"POST", "/api/brain/pack", `{"unknown":true}`}} {
		if w := call(request[0], request[1], request[2], "brain-test-key"); w.Code != 400 {
			t.Fatalf("invalid request: %d %s", w.Code, w.Body)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "http-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: "http://localhost/mcp/brain", HTTPClient: &http.Client{Transport: brainMuxTransport{mux, "brain-test-key"}}, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	tools, err := session.ListTools(ctx, nil)
	if err != nil || len(tools.Tools) != 5 {
		t.Fatalf("streamable tools: %v %v", tools, err)
	}
	tool, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "brain_pack", Arguments: brain.PackRequest{Query: "unicorn", Sources: []string{"project"}}})
	if err != nil || tool.IsError {
		t.Fatalf("streamable pack: %+v %v", tool, err)
	}
	tool, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "brain_write", Arguments: brain.WriteRequest{File: "durable/decision.md", Content: "# Decision\nKeep it durable.\n"}})
	if err != nil || tool.IsError {
		t.Fatalf("streamable write: %+v %v", tool, err)
	}
	if b, readErr := os.ReadFile(filepath.Join(dir, "durable", "decision.md")); readErr != nil || !strings.Contains(string(b), "Keep it durable") {
		t.Fatalf("streamable write file: %q %v", b, readErr)
	}
	w = call("POST", "/api/brain/sources", `{"action":"remove","id":"project"}`, "brain-test-key")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	w = call("GET", "/api/brain/search?query=unicorn", "", "brain-test-key")
	if strings.Contains(w.Body.String(), "notes.md") {
		t.Fatal("removed source exposed")
	}
}

func TestBrainLoomStorageBuiltinsAndVaultLock(t *testing.T) {
	testHome(t)
	if err := os.MkdirAll(memoryDir(), 0700); err != nil {
		t.Fatal(err)
	}
	if err := writeMemFile("brain-page.md", []byte("# Memo\nprivatebrainword")); err != nil {
		t.Fatal(err)
	}
	workspaceSessions = newRuntimeSessions()
	t.Cleanup(func() { workspaceSessions = newRuntimeSessions() })
	session := RuntimeSession{ID: "brain-session", Title: "Transcript", Messages: []Message{{Role: "system", Content: "hiddensystemword"}, {Role: "user", Content: "brainuserword"}, {Role: "assistant", Content: "brainassistantword"}, {Role: "tool", Content: "hiddentoolword"}}}
	if err := putStoreJSON(bkRuntimeSessions, session.ID, session); err != nil {
		t.Fatal(err)
	}
	storage := brainStorage{filepath.Join(LoomHome(), "brain")}
	e, err := brain.New(brain.Options{Storage: storage, Memory: brainMemory, Conversations: brainConversations, Available: brainAvailable})
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"privatebrainword", "brainuserword", "brainassistantword"} {
		hits, err := e.Search(brain.SearchRequest{Query: q})
		if err != nil || len(hits) != 1 {
			t.Fatalf("builtin %s: %v %v", q, hits, err)
		}
	}
	hits, err := e.Search(brain.SearchRequest{Query: "hiddensystemword hiddentoolword"})
	if err != nil || len(hits) != 0 {
		t.Fatal("private runtime state indexed")
	}
	b, err := os.ReadFile(filepath.Join(storage.dir, "index.json"))
	if err != nil || strings.Contains(string(b), "brainword") {
		t.Fatal("builtin plaintext persisted")
	}
	dir := t.TempDir()
	if err = os.WriteFile(filepath.Join(dir, "context.md"), []byte("filesystembrainword"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = e.Update(brain.Source{ID: "context", Label: "Context", Path: dir, Kind: "context"}); err != nil {
		t.Fatal(err)
	}
	if err = SetConfigKey("MEM_ENCRYPTED", "on"); err != nil {
		t.Fatal(err)
	}
	setMemDEK(make([]byte, 32))
	t.Cleanup(clearMemDEK)
	if err = e.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	b, err = os.ReadFile(filepath.Join(storage.dir, "index.json"))
	if err != nil || !looksEncrypted(b) || strings.Contains(string(b), "filesystembrainword") {
		t.Fatal("file cache not encrypted")
	}
	clearMemDEK()
	if _, err = e.Search(brain.SearchRequest{Query: "filesystembrainword"}); err == nil {
		t.Fatal("locked cached context exposed")
	}
	if _, err = brain.New(brain.Options{Storage: storage}); err == nil {
		t.Fatal("encrypted cache read without key")
	}
}
