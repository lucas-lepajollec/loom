package loom

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/brain"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gopkg.in/yaml.v3"
)

func brainItemRequest(text string) brain.RememberRequest {
	return brain.RememberRequest{Class: "semantic", Scope: "global", Text: text, Provenance: brain.MemoryProvenance{Kind: "user"}}
}
func brainItemPath(dir, base string, item brain.MemoryItem) string {
	return filepath.Join(dir, base, "memory", item.Class, item.ID+".md")
}
func primaryMemoryVault(t *testing.T, s *brainService) string {
	t.Helper()
	dir := t.TempDir()
	e, err := s.get()
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Update(brain.Source{ID: "primary", Label: "Vault", Path: dir, Kind: "context", Permission: "write", Primary: true}); err != nil {
		t.Fatal(err)
	}
	return dir
}
func TestBrainItemsStorageAndEncryption(t *testing.T) {
	for _, tc := range []struct {
		name               string
		primary, encrypted bool
	}{{"fallback", false, false}, {"encrypted-fallback", false, true}, {"vault", true, false}, {"plain-vault-with-encryption", true, true}} {
		t.Run(tc.name, func(t *testing.T) {
			testHome(t)
			s := theBrain()
			dir, base := s.storage.dir, "loom-memory"
			if tc.primary {
				dir, base = primaryMemoryVault(t, s), ".loom"
			}
			if tc.encrypted {
				if err := SetConfigKey("MEM_ENCRYPTED", "on"); err != nil {
					t.Fatal(err)
				}
				setMemDEK(make([]byte, 32))
				t.Cleanup(clearMemDEK)
			}
			item, err := s.Remember(brainItemRequest("owned durable fact"))
			if err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{brainItemPath(dir, base, item), filepath.Join(dir, base, "brain.yaml")} {
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if looksEncrypted(raw) != (tc.encrypted && !tc.primary) {
					t.Fatalf("wrong codec: %s", path)
				}
				plain, err := decodeMemContent(raw)
				if err != nil {
					t.Fatal(err)
				}
				if filepath.Ext(path) == ".md" && !strings.Contains(string(plain), "owned durable fact") {
					t.Fatal("missing body")
				}
			}
			restarted := newBrainService(LoomHome())
			result, err := restarted.ListMemory(brain.MemoryFilter{})
			if err != nil || len(result.Items) != 1 || result.Items[0].ID != item.ID {
				t.Fatalf("restart: %+v %v", result, err)
			}
			if err := s.TouchMemory([]string{item.ID}); err != nil {
				t.Fatal(err)
			}
			result, err = s.ListMemory(brain.MemoryFilter{})
			if err != nil || result.Items[0].LastUsedAt == 0 {
				t.Fatalf("touch: %+v %v", result, err)
			}
			if tc.encrypted {
				clearMemDEK()
				if _, err := s.ListMemory(brain.MemoryFilter{}); !errors.Is(err, errMemLocked) {
					t.Fatalf("locked cached list: %v", err)
				}
				if _, err := s.Remember(brainItemRequest("locked")); !errors.Is(err, errMemLocked) {
					t.Fatalf("locked write: %v", err)
				}
				if _, err := s.UpdateMemory(brain.UpdateMemoryRequest{ID: item.ID}); !errors.Is(err, errMemLocked) {
					t.Fatalf("locked update: %v", err)
				}
				if _, err := s.ForgetMemory(brain.ForgetMemoryRequest{ID: item.ID}); !errors.Is(err, errMemLocked) {
					t.Fatalf("locked forget: %v", err)
				}
				if err := s.TouchMemory([]string{item.ID}); !errors.Is(err, errMemLocked) {
					t.Fatalf("locked touch: %v", err)
				}
			}
		})
	}
}
func TestBrainItemsDistilledImportIdempotent(t *testing.T) {
	for _, primary := range []bool{false, true} {
		name := "fallback"
		if primary {
			name = "vault"
		}
		t.Run(name, func(t *testing.T) {
			testHome(t)
			s := theBrain()
			dir, base := s.storage.dir, "loom-memory"
			if primary {
				dir, base = primaryMemoryVault(t, s), ".loom"
			}
			items := []brainDistilledItem{}
			for _, tc := range []struct{ kind, review string }{{"decision", "accepted"}, {"fact", "accepted"}, {"preference", "accepted"}, {"todo", ""}, {"fact", "pending"}, {"todo", "rejected"}} {
				items = append(items, brainDistilledItem{ID: tc.kind + tc.review, Kind: tc.kind, Text: tc.kind + " " + tc.review, Review: tc.review, Source: brainDistilledSource{DiscussionID: "original", MessageIndex: 0}, Date: time.UnixMilli(123456789)})
			}
			if err := s.saveDistilledLocked(items); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(s.storage.dir, "distilled.json")
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				result, err := newBrainService(LoomHome()).ListMemory(brain.MemoryFilter{Status: "all"})
				if err != nil || len(result.Items) != 4 {
					t.Fatalf("import #%d: %+v %v", i, result, err)
				}
				for _, item := range result.Items {
					if item.Provenance.Kind != "distilled" || item.Provenance.DiscussionID != "original" || item.Provenance.MessageIndex == nil || *item.Provenance.MessageIndex != 0 || item.CreatedAt != 123456789 {
						t.Fatalf("provenance: %+v", item)
					}
					class := "semantic"
					status := "active"
					if strings.HasPrefix(item.Text, "decision") {
						class = "episodic"
					}
					if strings.HasPrefix(item.Text, "todo") {
						class = "working"
						status = "uncertain"
					}
					if item.Class != class || item.Status != status {
						t.Fatalf("mapping: %+v", item)
					}
				}
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("distilled.json modified")
			}
			raw, err := os.ReadFile(filepath.Join(dir, base, "brain.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			var metadata map[string]any
			if err := yaml.Unmarshal(raw, &metadata); err != nil || metadata["imported_distilled"] != true {
				t.Fatalf("marker: %s %v", raw, err)
			}
			// A later accepted candidate does not restart this one-time import.
			items = append(items, brainDistilledItem{ID: "later", Kind: "fact", Text: "later accepted", Review: "accepted", Date: time.Now()})
			if err := s.saveDistilledLocked(items); err != nil {
				t.Fatal(err)
			}
			result, err := newBrainService(LoomHome()).ListMemory(brain.MemoryFilter{Status: "all"})
			if err != nil || len(result.Items) != 4 {
				t.Fatalf("one-time: %+v %v", result, err)
			}
		})
	}
}
func TestBrainItemsHTTPAndMCP(t *testing.T) {
	testHome(t)
	if err := storeWebKey("items-key"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	registerBrainRoutes(mux, nil)
	call := func(method, url, body, key string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, url, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if key != "" {
			r.Header.Set("Authorization", "Bearer "+key)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	for _, route := range []string{"items", "items/update", "items/forget"} {
		t.Run("auth-"+route, func(t *testing.T) {
			for _, key := range []string{"", "wrong"} {
				if w := call("POST", "/api/brain/"+route, `{}`, key); w.Code != 401 {
					t.Fatalf("unprotected: %d", w.Code)
				}
			}
		})
	}
	for _, tc := range []struct {
		name, method, url, body string
		code                    int
	}{
		{"method", "DELETE", "/api/brain/items", "", 405},
		{"update-method", "GET", "/api/brain/items/update", "", 405},
		{"forget-method", "GET", "/api/brain/items/forget", "", 405},
		{"unknown-fields", "POST", "/api/brain/items", `{"unknown":true}`, 400},
		{"invalid-class", "POST", "/api/brain/items", `{"class":"bogus","scope":"global","text":"x","provenance":{"kind":"user"}}`, 400},
		{"invalid-limit", "GET", "/api/brain/items?limit=oops", "", 400},
		{"negative-limit", "GET", "/api/brain/items?limit=-1", "", 400},
		{"invalid-scope", "GET", "/api/brain/items?scope=project:", "", 400},
		{"invalid-status", "GET", "/api/brain/items?status=bogus", "", 400},
		{"missing-update", "POST", "/api/brain/items/update", `{"id":"missing","patch":{}}`, 400},
		{"missing-forget", "POST", "/api/brain/items/forget", `{"id":"missing"}`, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := call(tc.method, tc.url, tc.body, "items-key")
			if w.Code != tc.code || !strings.Contains(w.Body.String(), `"ok":false`) {
				t.Fatalf("%d %s", w.Code, w.Body)
			}
		})
	}
	w := call("POST", "/api/brain/items", `{"class":"semantic","scope":"project:a","text":"HTTP fact","importance":0,"confidence":0,"provenance":{"kind":"user"}}`, "items-key")
	var created brain.MemoryResult
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil || w.Code != 200 || !created.OK || created.Item.Importance != 0 || created.Item.Confidence != 0 {
		t.Fatalf("remember: %s %v", w.Body, err)
	}
	call("POST", "/api/brain/items", `{"class":"reflex","scope":"global","text":"Global rule","provenance":{"kind":"user"}}`, "items-key")
	w = call("GET", "/api/brain/items?scopes=project:a&classes=semantic,reflex", "", "items-key")
	var list brain.MemoryList
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || !list.OK || len(list.Items) != 2 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("scope list: %s %v", w.Body, err)
	}
	update, _ := json.Marshal(brain.UpdateMemoryRequest{ID: created.Item.ID, Patch: brain.MemoryPatch{Text: ptrMemoryText("Updated HTTP fact")}, Supersede: true})
	w = call("POST", "/api/brain/items/update", string(update), "items-key")
	var updated brain.MemoryResult
	if err := json.Unmarshal(w.Body.Bytes(), &updated); err != nil || !updated.OK || updated.Item.ID == created.Item.ID {
		t.Fatalf("update: %s %v", w.Body, err)
	}
	forget, _ := json.Marshal(brain.ForgetMemoryRequest{ID: updated.Item.ID})
	w = call("POST", "/api/brain/items/forget", string(forget), "items-key")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"expired"`) {
		t.Fatalf("forget: %s", w.Body)
	}
	// The HTTP response reports malformed files while retaining valid results.
	malformed := filepath.Join(theBrain().storage.dir, "loom-memory", "memory", "semantic", "broken.md")
	if err := os.WriteFile(malformed, []byte("broken frontmatter"), 0o600); err != nil {
		t.Fatal(err)
	}
	w = call("GET", "/api/brain/items", "", "items-key")
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || list.Malformed != 1 {
		t.Fatalf("malformed HTTP: %s %v", w.Body, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "memory-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: "http://localhost/mcp/brain", HTTPClient: &http.Client{Transport: brainMuxTransport{mux, "items-key"}}, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]*mcp.Tool{}
	for _, tool := range tools.Tools {
		found[tool.Name] = tool
	}
	for _, name := range []string{"remember", "update_memory", "forget_memory", "list_memory"} {
		if found[name] == nil || found[name].InputSchema == nil {
			t.Fatalf("missing tool/schema: %s", name)
		}
	}
	if !found["list_memory"].Annotations.ReadOnlyHint || found["remember"].Annotations.ReadOnlyHint {
		t.Fatal("wrong annotations")
	}
	candidateReq := brainItemRequest("MCP pending candidate")
	candidateReq.Status = "candidate"
	pending, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "remember", Arguments: candidateReq})
	if err != nil || pending.IsError {
		t.Fatalf("MCP candidate creation: %+v %v", pending, err)
	}
	for _, status := range []string{"", "candidate", "all"} {
		listed, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "list_memory", Arguments: brain.MemoryFilter{Status: status, Query: candidateReq.Text}})
		if err != nil || listed.IsError {
			t.Fatalf("MCP candidate filter: %+v %v", listed, err)
		}
		raw, _ := json.Marshal(listed.StructuredContent)
		var list brain.MemoryList
		if err := json.Unmarshal(raw, &list); err != nil {
			t.Fatal(err)
		}
		want := 1
		if status == "" {
			want = 0
		}
		if len(list.Items) != want {
			t.Fatalf("MCP %q candidate visibility: %+v", status, list)
		}
	}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "remember", Arguments: brainItemRequest("MCP durable fact")})
	if err != nil || result.IsError {
		t.Fatalf("MCP remember: %+v %v", result, err)
	}
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var remembered brain.MemoryResult
	if err := json.Unmarshal(raw, &remembered); err != nil || !remembered.OK || remembered.Item.Text != "MCP durable fact" {
		t.Fatalf("MCP result: %s %v", raw, err)
	}
	for _, tc := range []struct {
		name string
		args any
	}{{"list_memory", brain.MemoryFilter{Query: "MCP durable"}}, {"update_memory", brain.UpdateMemoryRequest{ID: remembered.Item.ID, Patch: brain.MemoryPatch{Text: ptrMemoryText("MCP revised")}}}, {"forget_memory", brain.ForgetMemoryRequest{ID: remembered.Item.ID}}} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tc.name, Arguments: tc.args})
			if err != nil || result.IsError {
				t.Fatalf("tool: %+v %v", result, err)
			}
		})
	}
	// A lock blocks even an already connected MCP session with cached items.
	if err := SetConfigKey("MEM_ENCRYPTED", "on"); err != nil {
		t.Fatal(err)
	}
	clearMemDEK()
	result, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "list_memory", Arguments: brain.MemoryFilter{}})
	if err != nil || !result.IsError {
		t.Fatalf("locked MCP: %+v %v", result, err)
	}
	w = call("GET", "/api/brain/items", "", "items-key")
	if w.Code != 423 {
		t.Fatalf("locked HTTP: %d %s", w.Code, w.Body)
	}
}
func ptrMemoryText(s string) *string { return &s }
func TestBrainManagedMemoryRejectsGenericWrites(t *testing.T) {
	testHome(t)
	s := theBrain()
	dir := primaryMemoryVault(t, s)
	for _, path := range []string{".loom/memory/semantic/file.md", ".loom/brain.yaml.md", "sub/../.loom/item.md"} {
		t.Run(path, func(t *testing.T) {
			if err := s.WriteSecondBrain(brain.WriteRequest{File: path, Content: "direct agent edit"}); err == nil {
				t.Fatal("generic write changed managed memory")
			}
		})
	}
	if _, err := os.Stat(filepath.Join(dir, ".loom")); !os.IsNotExist(err) {
		t.Fatalf("generic write created .loom: %v", err)
	}
}
