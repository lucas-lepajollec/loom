package loom

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/lucas-lepajollec/loom/internal/loom/brain"
)

func compactFixture(t *testing.T) (*runtimeSessions, RuntimeSession) {
	t.Helper()
	continuityBase(t)
	m := workspaceSessions
	p := CloudProvider{ID: "provider", Name: "Chosen provider", Endpoint: "https://chosen.example/v1", Model: "chosen-model", ContextWindows: map[string]int{"chosen-model": 4096}}
	if err := putStoreJSON(bkProviders, p.ID, p); err != nil {
		t.Fatal(err)
	}
	m.keys[p.ID] = "chosen-key"
	s, err := m.create("", p.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	s.Messages = []Message{um("Keep the goal")}
	for i := 0; i < 14; i++ {
		s.Messages = append(s.Messages, am(strings.Repeat("older findings ", 180)), um("Next step"))
		s.Turns = append(s.Turns, RuntimeTurnRecord{MessageIndex: len(s.Messages) - 2, RuntimeID: s.RuntimeID, Model: s.Model, ACPEvents: []DiscussionEvent{{"type": "text_delta", "text": "original display event"}}})
	}
	s.Messages = append(s.Messages, am("Latest recap"))
	if err := putStoreJSON(bkRuntimeSessions, s.ID, s); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { theBrain().candidateWrites.Wait() })
	return m, s
}
func compactModel(t *testing.T) *atomic.Int32 {
	t.Helper()
	calls := &atomic.Int32{}
	brainFakeModelClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var input struct {
			Model       string
			Messages    []Message
			Stream      bool
			Template    any `json:"chat_template_kwargs"`
			Temperature any `json:"temperature"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Error(err)
		}
		if input.Model != "chosen-model" || input.Stream || r.URL.String() != "https://chosen.example/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer chosen-key" {
			t.Error("wrong compaction destination", r.URL, input.Model)
		}
		if input.Template != nil || input.Temperature != nil {
			t.Error("local-only summary parameters sent to cloud")
		}
		if !strings.Contains(renderTranscript(input.Messages), "older findings") {
			t.Error("summary lost original findings")
		}
		continuityReply(w, "Keep discovered facts. Next: verify restore.")
	})
	return calls
}
func TestWorkspaceContextStateAndWarning(t *testing.T) {
	m, s := compactFixture(t)
	prepared := prepareDiscussion(s, "")
	c := clientSession(s)
	if c.Context.Source != "estimate" || c.Context.Size != 4096 || c.Context.Used != promptTokens(prepared.Messages) || c.ContextWarning {
		t.Fatalf("estimated state: %+v", c.Context)
	}
	if err := WriteConfig(map[string]string{"COMPACT": "off"}); err != nil {
		t.Fatal(err)
	}
	if !clientSession(s).ContextWarning {
		t.Fatal("auto off should warn")
	}
	for _, tc := range []struct {
		used, size int
		command    bool
		auto       string
		warning    bool
	}{
		{849, 1000, false, "on", false}, {850, 1000, false, "on", true}, {850, 1000, true, "on", false}, {850, 1000, true, "off", true}, {900, 0, false, "off", false},
	} {
		if err := WriteConfig(map[string]string{"COMPACT": tc.auto}); err != nil {
			t.Fatal(err)
		}
		s.RuntimeID = "test-acp"
		s.ACPUsage = map[string]any{"context": map[string]any{"used": float64(tc.used), "size": float64(tc.size)}}
		s.Commands = nil
		if tc.command {
			s.Commands = []map[string]any{{"name": "compact"}}
		}
		got := clientSession(s)
		if got.Context.Source != "agent" || got.Context.Used != tc.used || got.Context.Size != tc.size || got.ContextWarning != tc.warning {
			t.Fatalf("%+v got %+v warning %v", tc, got.Context, got.ContextWarning)
		}
	}
	s.RuntimeID, s.Model, s.ACPUsage = "openai-compatible", "unknown", nil
	if got := clientSession(s); got.Context.Size != 0 || got.ContextWarning {
		t.Fatal("unknown model invented a limit", got.Context)
	}
	if listed := m.list(); len(listed) != 1 || listed[0].Context.Used == 0 || listed[0].Messages != nil {
		t.Fatal("list lost context estimate")
	}
}
func TestWorkspaceCompactNowPreservesJournalAndUpdatesHandoff(t *testing.T) {
	m, original := compactFixture(t)
	calls := compactModel(t)
	events := make(chan DiscussionEvent, 8)
	sub := &discussionSubscriber{events: events}
	m.subscribers[original.ID] = map[*discussionSubscriber]bool{sub: true}
	got, changed, err := m.compactNow(context.Background(), original.ID)
	if err != nil || !changed || calls.Load() != 1 {
		t.Fatal(changed, err, calls.Load())
	}
	if !reflect.DeepEqual(got.Messages, original.Messages) || !reflect.DeepEqual(got.Turns, original.Turns) {
		t.Fatal("display journal changed")
	}
	if len(got.PortableMessages) >= len(original.Messages) || !strings.Contains(renderTranscript(got.PortableMessages), compactSummaryPrefix) || got.PortableMessages[0].Content != original.Messages[0].Content {
		t.Fatal("portable compaction", got.PortableMessages)
	}
	if len(got.Compactions) != 1 || got.Compactions[0].Model != "chosen-model" || got.Compactions[0].ProviderID != "provider" || got.Compactions[0].After >= got.Compactions[0].Before {
		t.Fatal("missing provenance", got.Compactions)
	}
	if e := <-events; e["type"] != "compacted" {
		t.Fatal(e)
	}
	list, err := theBrain().ListMemory(brain.MemoryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	state := continuityMemory(list.Items, "working", "task:"+original.ID, "discussion-state", original.ID)
	if !strings.Contains(state.Text, "## Recap") || !strings.Contains(state.Text, "verify restore") {
		t.Fatal("handoff recap", state.Text)
	}
	prepared := prepareDiscussion(got, "pending new request")
	if strings.Count(renderTranscript(prepared.Messages), "pending new request") != 1 || len(prepared.Messages) >= len(original.Messages) {
		t.Fatal("prompt uses display journal")
	}
	client := clientSession(got)
	if client.PortableMessages != nil || len(client.Turns[0].ACPEvents) != 0 {
		t.Fatal("heavy prompt/journal leaked to client")
	}
	replay := clientReplay(got)
	for _, e := range replay {
		if strings.Contains(eText(e), compactSummaryPrefix) {
			t.Fatal("summary entered display replay")
		}
	}
	next, err := m.continueDiscussion(got.ID, "Recap project")
	if err != nil || !strings.Contains(discussionContext(next).System, "verify restore") {
		t.Fatal("compaction recap not carried into successor", err)
	}
	stored, _ := m.get(got.ID)
	if !reflect.DeepEqual(stored.PortableMessages, got.PortableMessages) {
		t.Fatal("compaction not saved")
	}
}
func eText(e DiscussionEvent) string { text, _ := e["text"].(string); return text }
func TestWorkspaceCompactFailureAndNoopPreserveHistory(t *testing.T) {
	m, s := compactFixture(t)
	brainFakeModelClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401); io.WriteString(w, "secret") })
	_, changed, err := m.compactNow(context.Background(), s.ID)
	if err == nil || changed || strings.Contains(err.Error(), "secret") {
		t.Fatal(changed, err)
	}
	stored, _ := m.get(s.ID)
	if !reflect.DeepEqual(stored.Messages, s.Messages) || stored.PortableMessages != nil {
		t.Fatal("failure changed history")
	}
	s.Messages = []Message{um("first"), am("short answer")}
	if err := putStoreJSON(bkRuntimeSessions, s.ID, s); err != nil {
		t.Fatal(err)
	}
	if _, changed, err = m.compactNow(context.Background(), s.ID); err != nil || changed {
		t.Fatal("empty torso", changed, err)
	}
}
func TestWorkspaceAutoCompactionOnOffAndUnknown(t *testing.T) {
	for _, mode := range []string{"on", "off", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			m, s := compactFixture(t)
			if err := WriteConfig(map[string]string{"COMPACT": mode}); err != nil {
				t.Fatal(err)
			}
			if mode == "unknown" {
				var p CloudProvider
				getStoreJSON(bkProviders, s.ProviderID, &p)
				p.ContextWindows = nil
				putStoreJSON(bkProviders, p.ID, p)
			}
			summaries := compactModel(t)
			streamCalls := atomic.Int32{}
			oldTransport := http.DefaultTransport
			http.DefaultTransport = brainFakeModelTransport{http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				streamCalls.Add(1)
				var input struct{ Messages []Message }
				json.NewDecoder(r.Body).Decode(&input)
				transcript := renderTranscript(input.Messages)
				pending := 0
				for _, msg := range input.Messages {
					if msg.Role == "user" && msg.Content == "new pending request" {
						pending++
					}
				}
				if pending != 1 {
					t.Error("pending message changed", pending)
				}
				if strings.Contains(transcript, compactSummaryPrefix) != (mode == "on") {
					t.Error("wrong automatic compaction", mode)
				}
				w.Header().Set("Content-Type", "text/event-stream")
				io.WriteString(w, sseChunk("New answer")+"data: [DONE]\n\n")
			})}
			t.Cleanup(func() { http.DefaultTransport = oldTransport })
			if err := m.start(s.ID, "request-auto", "new pending request"); err != nil {
				t.Fatal(err)
			}
			got := awaitCloudFinished(t, m, s.ID)
			want := int32(0)
			if mode == "on" {
				want = 1
			}
			if summaries.Load() != want || streamCalls.Load() != 1 || got.Status != "complete" {
				t.Fatal(summaries.Load(), streamCalls.Load(), got.Status, got.Error)
			}
			if !reflect.DeepEqual(got.Messages[:len(s.Messages)], s.Messages) {
				t.Fatal("automatic compaction changed journal")
			}
			if mode == "on" && !strings.Contains(renderTranscript(got.PortableMessages), "New answer") {
				t.Fatal("answer missing from portable context")
			}
		})
	}
}
func TestWorkspaceACPCompactAdvertisedAndUnsupported(t *testing.T) {
	continuityBase(t)
	a := fakeACPAdapter(t)
	isolateRuntimeRegistry(t, a)
	m := workspaceSessions
	s := createACPSession(t, m, a, "edits")
	if _, _, err := m.compactNow(t.Context(), s.ID); !errors.Is(err, errCompactUnsupported) {
		t.Fatal("unadvertised command accepted", err)
	}
	if err := m.start(s.ID, "request-first", "fixture"); err != nil {
		t.Fatal(err)
	}
	first := waitACPTurn(t, m, s.ID)
	if !offersCompact(first) || clientSession(first).Context.Source != "agent" || clientSession(first).Context.Used != 24 {
		t.Fatal("ACP state not projected")
	}
	got, _, err := m.compactNow(t.Context(), s.ID)
	if err != nil || got.ID != s.ID {
		t.Fatal(err)
	}
	last := waitACPTurn(t, m, s.ID)
	if last.NativeSessionID != first.NativeSessionID || last.Messages[len(last.Messages)-1].Content != "Context compacted" || clientSession(last).Context.Used != 8 {
		t.Fatal("compact was not an exact prompt turn", last.Messages, last.NativeSessionID, first.NativeSessionID)
	}
}
func TestWorkspaceCompactAndContinueRefuseBusy(t *testing.T) {
	m, s := compactFixture(t)
	m.runs[s.ID] = &runtimeRun{session: s, cancel: func() {}}
	if _, _, err := m.compactNow(t.Context(), s.ID); err == nil {
		t.Fatal("compacted generating turn")
	}
	if _, err := m.continueDiscussion(s.ID, "project"); err == nil {
		t.Fatal("continued generating turn")
	}
	if len(listProjects()) != 0 {
		t.Fatal("busy request created project")
	}
	delete(m.runs, s.ID)
	s.RuntimeID, s.NativeArchive = "llama.cpp", "native"
	putStoreJSON(bkRuntimeSessions, s.ID, s)
	conv.ID, conv.Generating = "native", true
	if _, _, err := m.compactNow(t.Context(), s.ID); err == nil {
		t.Fatal("compacted generating native turn")
	}
}
func TestWorkspaceContinuePinsHandoffAndProject(t *testing.T) {
	for _, projectMode := range []string{"none", "same", "new"} {
		t.Run(projectMode, func(t *testing.T) {
			m, s := compactFixture(t)
			if projectMode == "same" {
				p, err := createProject("Existing")
				if err != nil {
					t.Fatal(err)
				}
				s.ProjectID = p.ID
				putStoreJSON(bkRuntimeSessions, s.ID, s)
			}
			s.Title, s.Workdir, s.Instructions = "Original", "/chosen/workdir", "Keep constraints"
			s.NativeSessionID, s.NativeContext = "private-session", "private-hash"
			s.ACPUsage = map[string]any{"private": true}
			s.Commands = []map[string]any{{"name": "compact"}}
			putStoreJSON(bkRuntimeSessions, s.ID, s)
			name := ""
			if projectMode == "new" {
				name = "New project"
			}
			next, err := m.continueDiscussion(s.ID, name)
			if err != nil {
				t.Fatal(err)
			}
			if next.ID == s.ID || next.ContinuedFrom != s.ID || next.Title != s.Title+" ›" || next.RuntimeID != s.RuntimeID || next.ProviderID != s.ProviderID || next.Model != s.Model || next.Endpoint != s.Endpoint || next.Workdir != s.Workdir || next.Instructions != s.Instructions {
				t.Fatal("continuation route/config", next)
			}
			if len(next.Messages) != 0 || next.NativeSessionID != "" || next.NativeContext != "" || next.ACPUsage != nil || next.Commands != nil {
				t.Fatal("private state copied")
			}
			old, _ := m.get(s.ID)
			if !reflect.DeepEqual(old.Messages, s.Messages) {
				t.Fatal("attachment rewrote journal")
			}
			if projectMode == "same" && next.ProjectID != s.ProjectID || projectMode == "new" && (next.ProjectID == "" || next.ProjectID != old.ProjectID) {
				t.Fatal("project attachment", old.ProjectID, next.ProjectID)
			}
			c := discussionContext(next)
			discussionPinned, projectPinned := false, false
			for _, item := range c.Items {
				if item.Kind == "memory" && item.Scope == "task:"+s.ID {
					discussionPinned = true
				}
				if item.Kind == "memory" && item.Scope == "project:"+next.ProjectID && item.Class == "working" {
					projectPinned = true
				}
			}
			if !discussionPinned || next.ProjectID != "" && !projectPinned {
				t.Fatal("continuation state not pinned", c.Items)
			}
			if c.Budget.Memory.Used > c.Budget.Memory.Available {
				t.Fatal("pinning exceeded budget")
			}
			if projectMode == "new" {
				p, _ := getProject(next.ProjectID)
				if p.Name != name {
					t.Fatal(p)
				}
			}
		})
	}
}
func TestWorkspaceContextEndpoints(t *testing.T) {
	_, s := compactFixture(t)
	compactModel(t)
	for _, tc := range []struct {
		handler http.HandlerFunc
		body    string
		field   string
	}{
		{handleRuntimeSessionCompact, `{"id":"` + s.ID + `"}`, "compacted"},
		{handleRuntimeSessionContinue, `{"id":"` + s.ID + `","project_name":"Continued"}`, "session"},
	} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/", strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		tc.handler(w, req)
		var out map[string]any
		json.Unmarshal(w.Body.Bytes(), &out)
		if w.Code != 200 || out["ok"] != true || out[tc.field] == nil {
			t.Fatal(w.Code, w.Body.String())
		}
		session, _ := out["session"].(map[string]any)
		if session["context"] == nil || session["portable_messages"] != nil {
			t.Fatal("client shape", session)
		}
	}
	s.RuntimeID = "unsupported"
	putStoreJSON(bkRuntimeSessions, s.ID, s)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/", strings.NewReader(`{"id":"`+s.ID+`"}`))
	req.Header.Set("Content-Type", "application/json")
	handleRuntimeSessionCompact(w, req)
	if w.Code != 409 || !strings.Contains(w.Body.String(), `"unsupported":true`) {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestWorkspaceNativeCompactPreservesArchiveAndActivation(t *testing.T) {
	m, s := compactFixture(t)
	s.RuntimeID, s.Model, s.NativeArchive = "llama.cpp", "chosen-model", "native"
	if err := WriteConfig(map[string]string{"MODEL": s.Model, "CTX": "4096"}); err != nil {
		t.Fatal(err)
	}
	if err := putStoreJSON(bkRuntimeSessions, s.ID, s); err != nil {
		t.Fatal(err)
	}
	a := &convArchive{ID: s.NativeArchive, Title: s.Title}
	appendNativeText(a, s.Messages)
	if err := saveArchive(a); err != nil {
		t.Fatal(err)
	}
	conv.ID, conv.Messages, conv.Log, conv.ActiveTitle = s.NativeArchive, a.Messages, a.Log, s.Title
	brainFakeModelClient(t, func(w http.ResponseWriter, r *http.Request) { continuityReply(w, "Native findings retained.") })
	got, changed, err := m.compactNow(t.Context(), s.ID)
	if err != nil || !changed || conv.Generating {
		t.Fatal(changed, err)
	}
	compacted, _ := loadArchive(s.NativeArchive)
	if !reflect.DeepEqual(compacted.Log[:len(a.Log)], a.Log) || !reflect.DeepEqual(got.Messages, s.Messages) || len(compacted.Messages) >= len(a.Messages) || !reflect.DeepEqual(conv.Messages, compacted.Messages) {
		t.Fatal("native compaction lost display/model separation")
	}
	if got.Context != nil {
		t.Fatal("read model persisted")
	}
	if clientSession(got).Context.Size != 4096 {
		t.Fatal("engine context unavailable")
	}
	if _, err = m.activateLocal(s.ID, conv); err != nil {
		t.Fatal(err)
	}
	reopened, _ := loadArchive(s.NativeArchive)
	if !reflect.DeepEqual(reopened.Messages, compacted.Messages) {
		t.Fatal("activation expanded compacted context")
	}
	m.syncNativeArchive(reopened)
	synced, _ := m.get(s.ID)
	if !strings.Contains(renderTranscript(synced.PortableMessages), compactSummaryPrefix) {
		t.Fatal("native sync lost compacted portable context")
	}
	// A retained native archive must not be used for cloud compaction.
	synced.RuntimeID = "openai-compatible"
	putStoreJSON(bkRuntimeSessions, synced.ID, synced)
	if clientSession(synced).Context.Size != 4096 {
		t.Fatal("cloud context did not use provider")
	}
}
func TestWorkspaceProviderCatalogContextWindows(t *testing.T) {
	m, s := compactFixture(t)
	oldTransport := http.DefaultTransport
	http.DefaultTransport = brainFakeModelTransport{http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer chosen-key" {
			t.Error("wrong catalog destination")
		}
		io.WriteString(w, `{"data":[{"id":"chosen-model","context_length":32000},{"id":"other","context_window":64000},{"id":"nested","top_provider":{"context_length":128000}},{"id":"unknown"},{"id":"bad-meta","context_length":"unknown","top_provider":"unknown"}]}`)
	})}
	t.Cleanup(func() { http.DefaultTransport = oldTransport })
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/providers/models", strings.NewReader(`{"id":"provider","consent":true}`))
	req.Header.Set("Content-Type", "application/json")
	handleProviderModels(w, req)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	got := clientSession(s)
	if got.Context.Size != 32000 {
		t.Fatal("discovered window not used", got.Context)
	}
	var p CloudProvider
	if !getStoreJSON(bkProviders, s.ProviderID, &p) || p.ContextWindows["other"] != 64000 || p.ContextWindows["nested"] != 128000 || p.ContextWindows["unknown"] != 0 {
		t.Fatal(p.ContextWindows)
	}
	if _, err := m.saveProvider(CloudProvider{ID: p.ID, Name: p.Name, Model: p.Model, Endpoint: p.Endpoint}, ""); err != nil {
		t.Fatal(err)
	}
	getStoreJSON(bkProviders, p.ID, &p)
	if p.ContextWindows["chosen-model"] != 32000 {
		t.Fatal("provider edit lost catalog metadata")
	}
}
