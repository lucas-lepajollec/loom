package loom

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/brain"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func memoryHTTPRequest(t *testing.T, handler http.HandlerFunc, method, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "/api/brain/memory?scope=global", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler(w, req)
	return w
}
func TestMarkdownMemoryHTTPMCPAndEncryptedFallback(t *testing.T) {
	for _, primary := range []bool{false, true} {
		t.Run(fmt.Sprint(primary), func(t *testing.T) {
			service := continuityBase(t)
			dir := filepath.Join(service.storage.dir, "loom-memory")
			if primary {
				dir = primaryMemoryVault(t, service)
			}
			if err := SetConfigKey("MEM_ENCRYPTED", "on"); err != nil {
				t.Fatal(err)
			}
			setMemDEK(make([]byte, 32))
			t.Cleanup(clearMemDEK)
			w := memoryHTTPRequest(t, service.memoryHTTP, "POST", `{"scope":"global","name":"Profile","description":"User preferences","type":"user","text":"Prefers concise answers"}`)
			if w.Code != 200 {
				t.Fatal(w.Code, w.Body)
			}
			var file brain.MemoryFile
			json.Unmarshal(w.Body.Bytes(), &file)
			raw, err := os.ReadFile(filepath.Join(dir, "Memory", file.File))
			if err != nil || looksEncrypted(raw) == primary {
				t.Fatal("wrong codec", err)
			}
			w = memoryHTTPRequest(t, service.memoryHTTP, "GET", "")
			if w.Code != 200 || !strings.Contains(w.Body.String(), "MEMORY") && !strings.Contains(w.Body.String(), "Profile") {
				t.Fatal(w.Code, w.Body)
			}
			server := brain.MCPServer(service)
			client := mcp.NewClient(&mcp.Implementation{Name: "fixture", Version: "1"}, nil)
			a, b := mcp.NewInMemoryTransports()
			serverSession, err := server.Connect(t.Context(), a, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer serverSession.Close()
			clientSession, err := client.Connect(t.Context(), b, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer clientSession.Close()
			for _, call := range []struct {
				name string
				args any
			}{{"memory_index", brain.MemoryIndexRequest{}}, {"memory_read", brain.MemoryRead{Scope: "global", File: file.File}}, {"memory_write", brain.MemoryWrite{Scope: "global", File: file.File, Name: "Profile", Description: "User preferences", Type: "user", Text: "Updated by MCP"}}, {"memory_delete", brain.MemoryRead{Scope: "global", File: file.File}}} {
				result, err := clientSession.CallTool(t.Context(), &mcp.CallToolParams{Name: call.name, Arguments: call.args})
				if err != nil || result.IsError {
					t.Fatal(call.name, result, err)
				}
			}
			result, err := clientSession.CallTool(t.Context(), &mcp.CallToolParams{Name: "memory_read", Arguments: brain.MemoryRead{Scope: "global", File: "../escape.md"}})
			if err != nil || !result.IsError {
				t.Fatal("unsafe MCP read", err)
			}
			clearMemDEK()
			w = memoryHTTPRequest(t, service.memoryHTTP, "GET", "")
			if w.Code != 423 {
				t.Fatal("locked", w.Code, w.Body)
			}
			setMemDEK(make([]byte, 32))
		})
	}
}
func consolidationFixture(t *testing.T) (*brainService, RuntimeSession) {
	t.Helper()
	service := continuityBase(t)
	p := CloudProvider{ID: "provider", Endpoint: "https://chosen.example/v1", Model: "model"}
	putStoreJSON(bkProviders, p.ID, p)
	workspaceSessions.keys[p.ID] = "selected-key"
	session := RuntimeSession{ID: "memory-discussion", Title: "Test", RuntimeID: "openai-compatible", ProviderID: p.ID, Endpoint: p.Endpoint, Model: p.Model, CreatedAt: time.Now().Add(-time.Hour).UnixMilli(), UpdatedAt: time.Now().Add(-6 * time.Minute).UnixMilli(), Messages: []Message{um("Please prefer concise answers."), am("I will."), {Role: "tool", Content: "PRIVATE TOOL"}}}
	if err := putStoreJSON(bkRuntimeSessions, session.ID, session); err != nil {
		t.Fatal(err)
	}
	return service, session
}
func TestMarkdownConsolidationOneCallProvenanceCheckpointAndDuplicates(t *testing.T) {
	service, session := consolidationFixture(t)
	count := 0
	brainFakeModelClient(t, func(w http.ResponseWriter, r *http.Request) {
		count++
		if r.URL.String() != session.Endpoint+"/chat/completions" || r.Header.Get("Authorization") != "Bearer selected-key" {
			t.Error("wrong route")
		}
		var input struct {
			Model    string
			Messages []Message
		}
		json.NewDecoder(r.Body).Decode(&input)
		body := renderTranscript(input.Messages)
		if !strings.Contains(body, "Prefer updating an existing file to creating a near-duplicate") || strings.Contains(body, "PRIVATE TOOL") {
			t.Error("bad prompt")
		}
		continuityReply(w, `[{"op":"create","scope":"global","name":"Writing style","description":"Answer length","type":"user","text":"Prefers concise answers."}]`)
	})
	status, err := service.runMemoryConsolidation(t.Context(), session.ID)
	if err != nil || count != 1 || len(status.LastOperations) != 1 {
		t.Fatal(status, count, err)
	}
	file, err := service.MemoryRead(brain.MemoryRead{Scope: "global", File: status.LastOperations[0].File})
	if err != nil || file.Metadata["discussion_id"] != session.ID || file.Metadata["date"] == nil {
		t.Fatal(file, err)
	}
	if _, err = service.runMemoryConsolidation(t.Context(), session.ID); err != nil || count != 1 {
		t.Fatal("checkpoint", count, err)
	}
	session.Messages = append(session.Messages, um("Still concise please."), am("Yes."))
	putStoreJSON(bkRuntimeSessions, session.ID, session)
	status, err = service.runMemoryConsolidation(t.Context(), session.ID)
	if err != nil || count != 2 {
		t.Fatal(status, count, err)
	}
	store, _ := service.memoryStore()
	files, _ := store.List("global")
	if len(files.Items) != 1 {
		t.Fatal("duplicate", files.Items)
	}
	persisted := newBrainService(LoomHome())
	w := memoryHTTPRequest(t, persisted.memoryStatusHTTP, "GET", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), session.ID) {
		t.Fatal(w.Body)
	}
}
func TestMarkdownConsolidationInvalidJSONNoRetryOrWrites(t *testing.T) {
	for _, reply := range []string{`{"summary":"obsolete"}`, `[{"op":"create","scope":"global","name":"Good","description":"Hook","type":"user","text":"Fact"},{"op":"delete","scope":"global","file":"../escape.md"}]`} {
		t.Run(reply, func(t *testing.T) {
			service, session := consolidationFixture(t)
			count := 0
			brainFakeModelClient(t, func(w http.ResponseWriter, r *http.Request) { count++; continuityReply(w, reply) })
			status, err := service.runMemoryConsolidation(t.Context(), session.ID)
			if err == nil || count != 1 || status.LastError == "" {
				t.Fatal(status, count, err)
			}
			store, _ := service.memoryStore()
			files, _ := store.List("global")
			if len(files.Items) != 0 {
				t.Fatal("partial validation writes", files)
			}
		})
	}
}
func TestMarkdownConsolidationIdleLeaveBusyOffAndConsent(t *testing.T) {
	service, session := consolidationFixture(t)
	count := 0
	brainFakeModelClient(t, func(w http.ResponseWriter, r *http.Request) { count++; continuityReply(w, "[]") })
	session.UpdatedAt = time.Now().UnixMilli()
	putStoreJSON(bkRuntimeSessions, session.ID, session)
	service.consolidatePaused(t.Context(), time.Now())
	if count != 0 {
		t.Fatal("early consolidation")
	}
	service.consolidation.left = map[string]bool{session.ID: true}
	service.consolidatePaused(t.Context(), time.Now())
	if count != 1 {
		t.Fatal("leaving did not consolidate", count)
	}
	session.Messages = append(session.Messages, um("More"))
	putStoreJSON(bkRuntimeSessions, session.ID, session)
	conv.Generating = true
	service.runMemoryConsolidation(t.Context(), session.ID)
	conv.Generating = false
	if count != 1 {
		t.Fatal("ran during generation")
	}
	SetConfigKey("brain.consolidation_model", "off")
	service.runMemoryConsolidation(t.Context(), session.ID)
	if count != 1 {
		t.Fatal("off")
	}
	pair, _ := json.Marshal(memoryModelPair{ProviderID: session.ProviderID, Model: "other"})
	SetConfigKey("brain.consolidation_model", string(pair))
	service.runMemoryConsolidation(t.Context(), session.ID)
	if count != 1 {
		t.Fatal("provider without consent")
	}
	consent, _ := json.Marshal(memoryModelConsent{ProviderID: session.ProviderID, Endpoint: session.Endpoint, Model: "other"})
	SetConfigKey("brain.consolidation_consent", string(consent))
	service.runMemoryConsolidation(t.Context(), session.ID)
	if count != 2 {
		t.Fatal("consented provider", count)
	}
	old := memoryLoadedModel
	memoryLoadedModel = func() string { return "" }
	t.Cleanup(func() { memoryLoadedModel = old })
	SetConfigKey("brain.consolidation_model", "local-loaded")
	session.Messages = append(session.Messages, um("More again"))
	putStoreJSON(bkRuntimeSessions, session.ID, session)
	service.runMemoryConsolidation(t.Context(), session.ID)
	if count != 2 {
		t.Fatal("loaded absent")
	}
}
func TestMarkdownConsolidationRejectsStaleSnapshotAndConcurrentRuns(t *testing.T) {
	service, session := consolidationFixture(t)
	old := memoryModelCall
	t.Cleanup(func() { memoryModelCall = old })
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error)
	memoryModelCall = func(ctx context.Context, endpoint, key, model, input string) (string, error) {
		close(entered)
		select {
		case <-release:
			return `[{"op":"create","scope":"global","name":"Stale","description":"Hook","type":"user","text":"Old fact"}]`, nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	go func() { _, err := service.runMemoryConsolidation(t.Context(), session.ID); done <- err }()
	<-entered
	if _, err := service.runMemoryConsolidation(t.Context(), session.ID); err == nil {
		t.Fatal("concurrent run")
	}
	session.ProjectID = "new-project"
	putStoreJSON(bkRuntimeSessions, session.ID, session)
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	files, err := service.MemoryIndex(brain.MemoryIndexRequest{})
	if err != nil || len(files.Global.Items) != 0 {
		t.Fatal("stale applied", files, err)
	}
}
func TestMarkdownTranscriptHooksMoveAndCacheExclusion(t *testing.T) {
	service := continuityBase(t)
	dir := primaryMemoryVault(t, service)
	p, _ := createProject("Memory work")
	session := RuntimeSession{ID: "transcript-session", Title: "Verbatim title", RuntimeID: "fixture", CreatedAt: time.Now().UnixMilli(), UpdatedAt: time.Now().UnixMilli(), Messages: []Message{um("uniquediscussionword"), am("Unabridged answer")}, Turns: []RuntimeTurnRecord{{MessageIndex: 0, Events: []HarnessEvent{{Name: "read_file", State: "completed"}}}}}
	putStoreJSON(bkRuntimeSessions, session.ID, session)
	service.queueSessionTranscript(session)
	transcriptJobs.Wait()
	service.writeRefresh.Wait()
	store, _ := service.memoryStore()
	old, _ := store.DiscussionPath(session.ID)
	session.ProjectID = p.ID
	putStoreJSON(bkRuntimeSessions, session.ID, session)
	service.queueSessionTranscript(session)
	transcriptJobs.Wait()
	service.writeRefresh.Wait()
	moved, _ := store.DiscussionPath(session.ID)
	if moved == old {
		t.Fatal("did not move")
	}
	if _, err := os.Stat(filepath.Join(dir, old)); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	hits, err := service.Search(brain.SearchRequest{Query: "uniquediscussionword", Sources: []string{"conversations"}, ProjectID: p.ID})
	if err != nil || len(hits) == 0 || hits[0].Path != moved {
		t.Fatal(hits, err)
	}
	cached, _ := os.ReadFile(filepath.Join(service.storage.dir, "index.json"))
	if strings.Contains(string(cached), "uniquediscussionword") {
		t.Fatal("transcript in cache")
	}
	snapshot := nativeTranscript(convArchive{ID: "native", SavedAt: time.Now().UnixMilli(), Log: []LogEvent{{TS: 100, Delta: map[string]any{"user": "Original native"}}, {TS: 200, Delta: map[string]any{"content": "Visible reply", "reasoning_content": "PRIVATE"}}, {TS: 250, Delta: map[string]any{"tool_used": map[string]any{"name": "read_file", "done": true, "result": "PRIVATE TOOL"}}}}}, "model")
	if snapshot.UpdatedAt != 250 || strings.Contains(transcriptBody(snapshot), "PRIVATE") || !reflect.DeepEqual(snapshot.Entries[0], brain.TranscriptEntry{Role: "user", Text: "Original native"}) {
		t.Fatal(snapshot)
	}
}

func TestMarkdownConsolidationUpdateDeleteAndProjectScope(t *testing.T) {
	service, session := consolidationFixture(t)
	p, err := createProject("Consolidation project")
	if err != nil {
		t.Fatal(err)
	}
	session.ProjectID = p.ID
	putStoreJSON(bkRuntimeSessions, session.ID, session)
	global, err := service.MemoryWrite(brain.MemoryWrite{Scope: "global", Name: "Old pointer", Description: "Obsolete", Type: "reference", Text: "Old reference"})
	if err != nil {
		t.Fatal(err)
	}
	project, err := service.MemoryWrite(brain.MemoryWrite{Scope: "project:" + p.ID, Name: "Decision", Description: "Current approach", Type: "project", Text: "Old decision\n\n**Why:** Old reason\n\n**How to apply:** Old way"})
	if err != nil {
		t.Fatal(err)
	}
	brainFakeModelClient(t, func(w http.ResponseWriter, r *http.Request) {
		var request struct{ Messages []Message }
		json.NewDecoder(r.Body).Decode(&request)
		input := renderTranscript(request.Messages)
		for _, text := range []string{"global MEMORY.md", "project MEMORY.md", global.File, project.File, "Old decision"} {
			if !strings.Contains(input, text) {
				t.Errorf("missing consolidation input %q", text)
			}
		}
		ops := []brain.MemoryOperation{{Op: "delete", Scope: "global", File: global.File}, {Op: "update", Scope: "project", File: project.File, Name: project.Name, Description: project.Description, Type: project.Type, Text: "New decision\n\n**Why:** Confirmed by user\n\n**How to apply:** Use this approach"}}
		raw, _ := json.Marshal(ops)
		continuityReply(w, string(raw))
	})
	status, err := service.runMemoryConsolidation(t.Context(), session.ID)
	if err != nil || len(status.LastOperations) != 2 {
		t.Fatal(status, err)
	}
	indexes, err := service.MemoryIndex(brain.MemoryIndexRequest{ProjectID: p.ID})
	if err != nil || len(indexes.Global.Items) != 0 || strings.Contains(indexes.Global.Index, global.File) || len(indexes.Project.Items) != 1 || !strings.Contains(indexes.Project.Index, project.File) {
		t.Fatal(indexes, err)
	}
	if item := indexes.Project.Items[0]; !strings.HasPrefix(item.Text, "New decision") || item.Metadata["discussion_id"] != session.ID {
		t.Fatal(item)
	}
}

func TestMarkdownConsolidationResidentModelRoutes(t *testing.T) {
	continuityBase(t)
	oldLoaded, oldNode, oldClient := memoryLoadedModel, currentEngineNode(), nodeClient
	t.Cleanup(func() { memoryLoadedModel = oldLoaded; nodeClient = oldClient; setEngineNode(oldNode) })
	setEngineNode(nil)
	SetConfigKey("MODEL", "resident.gguf")
	memoryLoadedModel = func() string { return "resident.gguf" }
	session := RuntimeSession{RuntimeID: "llama.cpp", Model: "resident.gguf"}
	endpoint, _, model, err := memoryDestination(session)
	if err != nil || endpoint != llamaBackendURL().String()+"/v1/chat/completions" || model != "loom" {
		t.Fatal(endpoint, model, err)
	}
	session.Model = "unloaded.gguf"
	if endpoint, _, _, _ = memoryDestination(session); endpoint != "" {
		t.Fatal("selected an unloaded model")
	}
	n := &engineNode{URL: "https://engine.example", V1: "https://engine.example", Direct: true, Router: true, Model: "remote", APIKey: "route-key"}
	setEngineNode(n)
	session.Model = n.Model
	reads := 0
	loaded := false
	nodeClient = &http.Client{Transport: brainFakeModelTransport{http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reads++
		if r.Method != "GET" || r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer route-key" {
			t.Error("unexpected route inspection", r.Method, r.URL)
		}
		state := "unloaded"
		if loaded {
			state = "loaded"
		}
		fmt.Fprintf(w, `{"data":[{"id":"remote","status":{"value":%q}}]}`, state)
	})}}
	if endpoint, _, _, _ = memoryDestination(session); endpoint != "" || reads != 0 {
		t.Fatal("unconsented route")
	}
	SetConfigKey("brain.consolidation_local_consent", n.V1)
	if endpoint, _, _, _ = memoryDestination(session); endpoint != "" || reads != 1 {
		t.Fatal("unloaded linked model")
	}
	loaded = true
	endpoint, key, model, err := memoryDestination(session)
	if err != nil || endpoint != n.V1+"/v1/chat/completions" || key != n.APIKey || model != "remote" {
		t.Fatal(endpoint, key, model, err)
	}
}

func TestMarkdownMigrationEncryptedFallbackAndAcceptedOnly(t *testing.T) {
	service := continuityBase(t)
	if err := os.MkdirAll(service.storage.dir, 0700); err != nil {
		t.Fatal(err)
	}
	SetConfigKey("MEM_ENCRYPTED", "on")
	setMemDEK(make([]byte, 32))
	t.Cleanup(clearMemDEK)
	legacy := brain.NewMemoryStore(brain.MemoryStoreOptions{Dir: service.storage.dir, Base: "loom-memory", Encode: encodeMemContent, Decode: decodeMemContent, Available: brainAvailable})
	profile, err := legacy.Remember(brain.RememberRequest{Class: "semantic", Scope: "global", Text: "Legacy profile", Tags: []string{brain.ProfileTag}, Provenance: brain.MemoryProvenance{Kind: "user"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = legacy.Remember(brain.RememberRequest{Class: "semantic", Scope: "global", Text: "Pending guess", Status: "candidate", Provenance: brain.MemoryProvenance{Kind: "user"}}); err != nil {
		t.Fatal(err)
	}
	if err = service.saveDistilledLocked([]brainDistilledItem{{ID: "accepted", Kind: "fact", Text: "Confirmed fact", Review: "accepted"}, {ID: "pending", Kind: "fact", Text: "Unconfirmed guess", Review: "pending"}}); err != nil {
		t.Fatal(err)
	}
	store, err := service.memoryStore()
	if err != nil {
		t.Fatal(err)
	}
	files, err := store.List("global")
	if err != nil || len(files.Items) != 2 {
		t.Fatal(files, err)
	}
	for _, file := range files.Items {
		if strings.Contains(file.Text, "guess") {
			t.Fatal("pending migrated", file)
		}
		raw, err := os.ReadFile(filepath.Join(service.storage.dir, "loom-memory", "Memory", file.File))
		if err != nil || !looksEncrypted(raw) {
			t.Fatal("fallback memory not encrypted", err)
		}
	}
	old, err := os.ReadFile(filepath.Join(service.storage.dir, "loom-memory", "memory.legacy", "semantic", profile.ID+".md"))
	if err != nil || !looksEncrypted(old) {
		t.Fatal("legacy not preserved", err)
	}
	reportPath := filepath.Join(service.storage.dir, "loom-memory", ".loom", "migration-memory.md")
	report, err := os.ReadFile(reportPath)
	if err != nil || !looksEncrypted(report) {
		t.Fatal("report not encrypted", err)
	}
	fresh := newBrainService(LoomHome())
	if _, err = fresh.MemoryIndex(brain.MemoryIndexRequest{}); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(reportPath)
	if !reflect.DeepEqual(report, after) {
		t.Fatal("migration repeated")
	}
}

func TestMarkdownConsolidationBoundedIncrementalTranscript(t *testing.T) {
	service, session := consolidationFixture(t)
	session.Messages = []Message{um(strings.Repeat("é", 30000)), am("End")}
	putStoreJSON(bkRuntimeSessions, session.ID, session)
	old := memoryModelCall
	t.Cleanup(func() { memoryModelCall = old })
	parts := []string{}
	memoryModelCall = func(_ context.Context, _, _, _, input string) (string, error) {
		_, part, _ := strings.Cut(input, "NEW transcript portion:\n")
		part, _, _ = strings.Cut(part, "\n\nglobal MEMORY.md:")
		if len(part) > 24<<10 {
			t.Error("transcript budget exceeded", len(part))
		}
		parts = append(parts, part)
		return "[]", nil
	}
	for i := 0; i < 4; i++ {
		if _, err := service.runMemoryConsolidation(t.Context(), session.ID); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := strings.Join(parts, ""), transcriptBody(sessionTranscript(session)); got != want || len(parts) != 3 {
		t.Fatal("incremental checkpoint lost or repeated text", len(got), len(want), len(parts))
	}
}

func TestMarkdownTranscriptNativeProjectMoveSurvivesArchiveSync(t *testing.T) {
	service := continuityBase(t)
	p, err := createProject("New home")
	if err != nil {
		t.Fatal(err)
	}
	archive := &convArchive{ID: "native-archive", Title: "Native work", Log: []LogEvent{{TS: time.Now().UnixMilli(), Delta: map[string]any{"user": "Verbatim native question"}}}}
	if err = saveArchive(archive); err != nil {
		t.Fatal(err)
	}
	session := RuntimeSession{ID: "common-native", NativeArchive: archive.ID, RuntimeID: "llama.cpp", Title: archive.Title, CreatedAt: time.Now().UnixMilli(), Messages: []Message{um("Verbatim native question")}}
	putStoreJSON(bkRuntimeSessions, session.ID, session)
	service.queueSessionTranscript(session)
	transcriptJobs.Wait()
	service.writeRefresh.Wait()
	store, _ := service.memoryStore()
	old, _ := store.DiscussionPath(session.ID)
	prepared := discussionContext(session)
	moved, err := workspaceSessions.configureDiscussion(session.ID, session.Title, p.ID, "", prepared.Revision, true)
	if err != nil {
		t.Fatal(err)
	}
	transcriptJobs.Wait()
	service.writeRefresh.Wait()
	path, _ := store.DiscussionPath(session.ID)
	if path == old || !strings.Contains(path, "/new-home/") {
		t.Fatal("native transcript not moved", path)
	}
	updated, ok := loadArchive(archive.ID)
	if !ok || updated.ProjectID != p.ID {
		t.Fatal("native archive project not synchronized", updated)
	}
	workspaceSessions.syncNativeArchive(updated)
	current, ok := workspaceSessions.get(moved.ID)
	if !ok || current.ProjectID != p.ID {
		t.Fatal("archive sync reverted project", current)
	}
}
