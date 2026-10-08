package loom

import (
	"context"
	"encoding/json"
	"github.com/lucas-lepajollec/loom/internal/loom/brain"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/lucas-lepajollec/loom/internal/loom/discussion"
)

func TestFrozenContextCapturedAtStartAndReused(t *testing.T) {
	testHome(t)
	originalMemory := rememberContextItem(t, "reflex", "global", "Original frozen preference.")
	adapter := memoryContextAdapter{id: "frozen-fixture", run: func(_ RuntimeTurn, emit ChatCallback) { emit(StreamEvent{Content: "Answer"}) }}
	isolateRuntimeRegistry(t, adapter)
	m := newRuntimeSessions()
	s := RuntimeSession{ID: "frozen-start", RuntimeID: adapter.id, Title: "Test", Messages: []Message{}, Status: "idle"}
	if err := putStoreJSON(bkRuntimeSessions, s.ID, s); err != nil {
		t.Fatal(err)
	}
	preview := prepareDiscussion(s, "First query")
	stored, _ := m.get(s.ID)
	if stored.FrozenRevision != "" {
		t.Fatal("preview captured a snapshot")
	}
	if err := m.start(s.ID, "first-frozen-request", "First query", preview.Context.Revision); err != nil {
		t.Fatal(err)
	}
	first := awaitFrozenTurn(t, m, s.ID)
	if first.FrozenRevision == "" || first.FrozenContext != preview.Context.System {
		t.Fatal("first turn did not capture preview")
	}
	changedText := "New mutable preference."
	if _, err := theBrain().UpdateMemory(brain.UpdateMemoryRequest{ID: originalMemory.ID, Patch: brain.MemoryPatch{Text: &changedText}}); err != nil {
		t.Fatal(err)
	}
	if err := m.start(s.ID, "second-frozen-request", "Different query"); err != nil {
		t.Fatal(err)
	}
	second := awaitFrozenTurn(t, m, s.ID)
	if second.FrozenContext != first.FrozenContext || second.FrozenRevision != first.FrozenRevision {
		t.Fatal("mutable memory/query changed snapshot")
	}
	if strings.Contains(second.FrozenContext, "New mutable") {
		t.Fatal("snapshot grew")
	}
	if len(second.Turns[1].ContextItems) == 0 || !second.Turns[1].ContextItems[0].Frozen {
		t.Fatal("turn inspector lost frozen flag")
	}
}

func awaitFrozenTurn(t *testing.T, m *runtimeSessions, id string) RuntimeSession {
	t.Helper()
	awaitCloudFinished(t, m, id)
	s, _ := m.get(id)
	if s.Status != "complete" {
		t.Fatalf("turn: %s %s", s.Status, s.Error)
	}
	return s
}

func TestFrozenContextStaticInvalidation(t *testing.T) {
	testHome(t)
	p, err := createProject("First project")
	if err != nil {
		t.Fatal(err)
	}
	other, err := createProject("Other project")
	if err != nil {
		t.Fatal(err)
	}
	s := RuntimeSession{ID: "static", RuntimeID: "openai-compatible", ProjectID: p.ID, Instructions: "Original", PortableMessages: []Message{{Role: "user", Content: "history"}}}
	s.FrozenSnapshot = discussionContextFor(s, "first").Snapshot
	baseline := s.FrozenRevision
	cases := map[string]func(*RuntimeSession){
		"project":          func(s *RuntimeSession) { s.ProjectID = other.ID },
		"instructions":     func(s *RuntimeSession) { s.Instructions = "Updated" },
		"runtime":          func(s *RuntimeSession) { s.RuntimeID = "llama.cpp" },
		"provider":         func(s *RuntimeSession) { s.ProviderID = "other" },
		"model":            func(s *RuntimeSession) { s.Model = "other" },
		"compaction":       func(s *RuntimeSession) { s.Compactions = append(s.Compactions, discussion.CompactionRecord{}) },
		"portable rewrite": func(s *RuntimeSession) { s.PortableMessages[0].Content = "[CONTEXT COMPACTED] summary" },
		"continued from":   func(s *RuntimeSession) { s.ContinuedFrom = "predecessor" },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			next := cloneRuntimeSession(s)
			change(&next)
			c := discussionContextFor(next, "second")
			if c.Snapshot.FrozenRevision == baseline {
				t.Fatal("static change retained snapshot")
			}
		})
	}
	next := cloneRuntimeSession(s)
	next.PortableMessages = append(next.PortableMessages, Message{Role: "assistant", Content: "Answer"}, Message{Role: "user", Content: "Next"})
	next.FrozenSnapshot = discussion.TrackFrozenPortable(next.FrozenSnapshot, next.PortableMessages)
	if discussionContextFor(next, "different").Snapshot.FrozenRevision != baseline {
		t.Fatal("normal append invalidated snapshot")
	}
	next.PortableMessages[len(next.PortableMessages)-1].Content = "Rewritten tail"
	if discussionContextFor(next, "different").Snapshot.FrozenRevision == baseline {
		t.Fatal("rewrite of appended history retained snapshot")
	}
	// Returning to an originally empty history is still a rewrite boundary.
	empty := RuntimeSession{ID: "empty-prefix", RuntimeID: "openai-compatible", PortableMessages: []Message{}}
	empty.FrozenSnapshot = discussionContextFor(empty, "").Snapshot
	originalRevision := empty.FrozenRevision
	empty.PortableMessages = append(empty.PortableMessages, Message{Role: "user", Content: "Appended"})
	empty.FrozenSnapshot = discussion.TrackFrozenPortable(empty.FrozenSnapshot, empty.PortableMessages)
	empty.PortableMessages = []Message{}
	if discussionContextFor(empty, "").Snapshot.FrozenRevision == originalRevision {
		t.Fatal("rewrite back to empty prefix retained snapshot")
	}
	skill, err := saveCapability(Capability{Name: "Review", Instructions: "New skill"})
	if err != nil {
		t.Fatal(err)
	}
	p.CapabilityIDs = []string{skill.ID}
	if err = putStoreJSON(bkProjects, p.ID, p); err != nil {
		t.Fatal(err)
	}
	if discussionContextFor(s, "second").Snapshot.FrozenRevision == baseline {
		t.Fatal("skill selection did not invalidate")
	}
}

func TestFrozenContextCloudExtrasOutgoingOnlyAndPreview(t *testing.T) {
	testHome(t)
	rememberContextItem(t, "reflex", "global", "Frozen core.")
	fact := rememberContextItem(t, "semantic", "global", "quartz is the chosen mineral.")
	s := RuntimeSession{ID: "cloud-extras", RuntimeID: "openai-compatible", Title: "Test", Messages: []Message{{Role: "user", Content: "Earlier"}, {Role: "assistant", Content: "Answer"}}, PortableMessages: []Message{{Role: "user", Content: "Earlier"}, {Role: "assistant", Content: "Answer"}}}
	s.FrozenSnapshot = discussionContextFor(s, "").Snapshot
	before := cloneRuntimeSession(s)
	p := prepareDiscussion(s, "quartz question")
	if strings.Contains(p.Context.System, fact.Text) || !strings.Contains(p.Context.Extras, fact.Text) {
		t.Fatal("retrieval is not separated from frozen system")
	}
	outgoing := msgText(p.Messages[len(p.Messages)-1])
	if !strings.HasPrefix(outgoing, "<loom-context>\n") || !strings.HasSuffix(outgoing, "</loom-context>\n\nquartz question") {
		t.Fatal("bad outgoing extras", outgoing)
	}
	if msgText(s.Messages[0]) != msgText(before.Messages[0]) || msgText(s.PortableMessages[0]) != msgText(before.PortableMessages[0]) {
		t.Fatal("preview mutated transcript")
	}
	frozen, extra := false, false
	for _, item := range p.Context.Items {
		if item.Source == fact.ID {
			extra = !item.Frozen
		}
		frozen = frozen || item.Frozen
	}
	if !frozen || !extra {
		t.Fatal("inspector flags do not describe outgoing text")
	}
	m := newRuntimeSessions()
	old := workspaceSessions
	workspaceSessions = m
	t.Cleanup(func() { workspaceSessions = old })
	if err := putStoreJSON(bkRuntimeSessions, s.ID, s); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/api/runtime/sessions/preview", strings.NewReader(`{"id":"cloud-extras","text":"quartz question"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handleRuntimeSessionPreview(w, req)
	var response struct {
		Preview DiscussionPreview `json:"preview"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || w.Code != 200 {
		t.Fatal(w.Code, err, w.Body.String())
	}
	if msgText(response.Preview.Messages[len(response.Preview.Messages)-1]) != outgoing {
		t.Fatal("HTTP preview differs from send")
	}
	s.Messages = append(s.Messages, Message{Role: "user", Content: "quartz question"}, Message{Role: "assistant", Content: ""})
	s.PortableMessages = append(s.PortableMessages, Message{Role: "user", Content: "quartz question"})
	s.Turns = []RuntimeTurnRecord{{MessageIndex: 3}}
	recordTurnContext(&s.Turns[0], p.Context)
	s.ContextExtras = p.Context.Extras
	run := &runtimeRun{session: s, cancel: func() {}}
	m.runs[s.ID] = run
	adapter := memoryContextAdapter{id: s.RuntimeID, run: func(turn RuntimeTurn, emit ChatCallback) {
		if msgText(turn.Messages[len(turn.Messages)-1]) != outgoing {
			t.Error("wire lost extras")
		}
		emit(StreamEvent{Content: "Cloud answer"})
	}}
	m.generate(context.Background(), run, adapter, p.Messages, p.Context)
	result, _ := m.get(s.ID)
	for _, history := range [][]Message{result.Messages, result.PortableMessages} {
		for _, msg := range history {
			if strings.Contains(msgText(msg), "<loom-context>") {
				t.Fatal("extras persisted into history")
			}
		}
	}
	rememberContextItem(t, "reflex", "global", "Changed while inspecting.")
	actual := discussionContext(result)
	if actual.Extras != p.Context.Extras || actual.System != p.Context.System {
		t.Fatal("completed inspector recomputed sent context")
	}
}

func TestFrozenContextRefreshEndpoint(t *testing.T) {
	testHome(t)
	m := newRuntimeSessions()
	old := workspaceSessions
	workspaceSessions = m
	t.Cleanup(func() { workspaceSessions = old })
	s := RuntimeSession{ID: "refresh-frozen", RuntimeID: "llama.cpp", Instructions: "Original"}
	s.FrozenSnapshot = discussionContextFor(s, "").Snapshot
	if err := putStoreJSON(bkRuntimeSessions, s.ID, s); err != nil {
		t.Fatal(err)
	}
	rememberContextItem(t, "reflex", "global", "Include after refresh.")
	req := httptest.NewRequest("POST", "/api/runtime/sessions/refresh-context", strings.NewReader(`{"id":"refresh-frozen"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handleRuntimeSessionRefreshContext(w, req)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	refreshed, _ := m.get(s.ID)
	if refreshed.FrozenRevision != "" || refreshed.FrozenContext != "" {
		t.Fatal("refresh recomputed instead of invalidating")
	}
	c := discussionContextFor(refreshed, "")
	if !strings.Contains(c.System, "Include after refresh.") {
		t.Fatal("next turn did not refresh memory")
	}
	stored, _ := m.get(s.ID)
	if stored.FrozenRevision != "" {
		t.Fatal("preview persisted after refresh")
	}
	m.preparing[s.ID] = true
	if _, err := m.refreshContext(s.ID); err == nil {
		t.Fatal("refresh accepted during preparation")
	}
}

func TestACPProjectFrozenContextReusesProcessAfterHandoff(t *testing.T) {
	testHome(t)
	a := fakeACPAdapter(t)
	isolateRuntimeRegistry(t, a)
	m := newRuntimeSessions()
	s := createACPSession(t, m, a, "edits")
	p, err := createProject("ACP project")
	if err != nil {
		t.Fatal(err)
	}
	s.ProjectID = p.ID
	if err = putStoreJSON(bkRuntimeSessions, s.ID, s); err != nil {
		t.Fatal(err)
	}
	if err = theBrain().saveHandoff([]handoffTurn{handoffFixtureTurn("prior", p.ID, 1000)}); err != nil {
		t.Fatal(err)
	}
	if err = m.start(s.ID, "frozen-acp-first", "first project query"); err != nil {
		t.Fatal(err)
	}
	first := waitACPTurn(t, m, s.ID)
	theBrain().waitHandoffs()
	m.acpMu.Lock()
	process := m.acp[s.ID]
	m.acpMu.Unlock()
	rememberContextItem(t, "semantic", "project:"+p.ID, "__loom_inspect_portable quartz detail")
	preview := prepareDiscussion(first, "__loom_inspect_portable")
	if preview.Context.Extras != "" || msgText(preview.Messages[len(preview.Messages)-1]) != "__loom_inspect_portable" {
		t.Fatal("ACP received per-message extras")
	}
	if acpContextHash(preview.Messages[:len(preview.Messages)-1]) != first.NativeContext {
		t.Fatal("handoff changed ACP prefix")
	}
	if err = m.start(s.ID, "frozen-acp-second", "__loom_inspect_portable"); err != nil {
		t.Fatal(err)
	}
	second := waitACPTurn(t, m, s.ID)
	m.acpMu.Lock()
	same := m.acp[s.ID] == process
	m.acpMu.Unlock()
	if !same || second.NativeSessionID != first.NativeSessionID {
		t.Fatal("ACP process/session restarted")
	}
	select {
	case <-process.client.done:
		t.Fatal("reused process was closed")
	default:
	}
	if second.Messages[len(second.Messages)-1].Content != "__loom_inspect_portable" {
		t.Fatal("portable history was re-sent", second.Messages[len(second.Messages)-1].Content)
	}
	if first.FrozenContext != second.FrozenContext || first.FrozenRevision != second.FrozenRevision {
		t.Fatal("handoff/query changed snapshot")
	}
}

func TestNativeFrozenContextExtrasStayOutOfArchive(t *testing.T) {
	testHome(t)
	old := workspaceSessions
	workspaceSessions = newRuntimeSessions()
	t.Cleanup(func() { workspaceSessions = old })
	rememberContextItem(t, "reflex", "global", "Native frozen core.")
	c := &Conversation{ID: "native-frozen", Messages: []Message{{Role: "user", Content: "first"}}}
	c.cond = sync.NewCond(&c.mu)
	session := nativeContextSession(c.ID, "")
	first, err := c.captureDiscussionContext(c.epoch, session, "first")
	if err != nil {
		t.Fatal(err)
	}
	fact := rememberContextItem(t, "semantic", "global", "quartz native fact")
	c.Messages = append(c.Messages, Message{Role: "assistant", Content: "Answer"}, Message{Role: "user", Content: []map[string]any{{"type": "text", "text": "quartz"}}})
	if lastUserText(c.Messages) != "quartz" {
		t.Fatal("native multimodal query used an older message")
	}
	second, err := c.captureDiscussionContext(c.epoch, nativeContextSession(c.ID, ""), lastUserText(c.Messages))
	if err != nil {
		t.Fatal(err)
	}
	if first.System != second.System || !strings.Contains(second.Extras, fact.Text) {
		t.Fatal("native snapshot/extras wrong")
	}
	outgoing := discussion.WithContextExtras(c.Messages, second.Extras)
	if !strings.Contains(msgText(outgoing[len(outgoing)-1]), "<loom-context>") {
		t.Fatal("native extras not sent")
	}
	a, ok := loadArchive(c.ID)
	if !ok || a.FrozenContext != first.System {
		t.Fatal("native snapshot did not persist")
	}
	for _, msg := range a.Messages {
		if strings.Contains(msgText(msg), "<loom-context>") {
			t.Fatal("native extras persisted")
		}
	}
}

func TestFrozenContextBrainPassagesAreExtrasAndSourceSelectionInvalidates(t *testing.T) {
	testHome(t)
	vault := primaryMemoryVault(t, theBrain())
	path := filepath.Join(vault, "quartz.md")
	if err := os.WriteFile(path, []byte("quartz passage belongs only to this query"), 0600); err != nil {
		t.Fatal(err)
	}
	e, err := theBrain().get()
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	p, err := createProject("Passages")
	if err != nil {
		t.Fatal(err)
	}
	s := RuntimeSession{ID: "passages-frozen", RuntimeID: "openai-compatible", ProjectID: p.ID}
	s.FrozenSnapshot = discussionContextFor(s, "").Snapshot
	preview := prepareDiscussion(s, "quartz")
	if strings.Contains(preview.Context.System, "belongs only") || !strings.Contains(preview.Context.Extras, "belongs only") {
		t.Fatal("Brain passage was not outgoing-only")
	}
	passages := 0
	for _, item := range preview.Context.Items {
		if item.Kind == "brain_passage" {
			passages++
			if item.Frozen {
				t.Fatal("passage marked frozen")
			}
		}
	}
	if passages == 0 || len(preview.Context.BrainCitations) == 0 {
		t.Fatal("inspector missing passage provenance")
	}
	if err = os.WriteFile(path, []byte("quartz changed passage text"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = e.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if frozenContextRevision(s) != s.FrozenRevision {
		t.Fatal("vault contents invalidated system")
	}
	sources := e.Sources()
	for _, source := range sources {
		if source.Primary {
			source.Label += " renamed"
			if err = e.Update(source); err != nil {
				t.Fatal(err)
			}
		}
	}
	if discussionContextFor(s, "quartz").Snapshot.FrozenRevision == s.FrozenRevision {
		t.Fatal("primary Brain definition did not invalidate")
	}
}
