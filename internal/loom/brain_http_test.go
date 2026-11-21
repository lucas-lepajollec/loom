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
	if err = theBrain().discussionDocuments(context.Background(), func(d brain.Document) bool {
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
func TestBrainAuthAndStreamableMCP(t *testing.T) {
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
	for _, route := range []string{"/api/brain/memory/settings", "/mcp/brain", "/mcp/loom"} {
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
	e, err := theBrain().get()
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Update(brain.Source{ID: "project", Label: "Project", Path: dir, Kind: "context", Permission: "write", Primary: true}); err != nil {
		t.Fatal(err)
	}
	if err := e.Refresh(t.Context()); err != nil {
		t.Fatal(err)
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
	if err != nil || len(tools.Tools) != 12 {
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

}

func TestBrainLoomStorageBuiltinsAndVaultLock(t *testing.T) {
	testHome(t)
	service := theBrain()
	if _, err := service.MemoryWrite(brain.MemoryWrite{Scope: "global", Name: "Memo", Description: "Private recall", Type: "reference", Text: "privatebrainword"}); err != nil {
		t.Fatal(err)
	}
	workspaceSessions = newRuntimeSessions()
	t.Cleanup(func() { workspaceSessions = newRuntimeSessions() })
	session := RuntimeSession{ID: "brain-session", Title: "Transcript", Messages: []Message{{Role: "system", Content: "hiddensystemword"}, {Role: "user", Content: "brainuserword"}, {Role: "assistant", Content: "brainassistantword"}, {Role: "tool", Content: "hiddentoolword"}}}
	if err := putStoreJSON(bkRuntimeSessions, session.ID, session); err != nil {
		t.Fatal(err)
	}
	storage := service.storage
	e, err := service.get()
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
