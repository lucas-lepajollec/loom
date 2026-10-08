package loom

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/lucas-lepajollec/loom/internal/loom/brain"
	"github.com/lucas-lepajollec/loom/internal/loom/project"
)

func rememberContextItem(t *testing.T, class, scope, text string) brain.MemoryItem {
	t.Helper()
	req := brainItemRequest(text)
	req.Class, req.Scope = class, scope
	item, err := theBrain().Remember(req)
	if err != nil {
		t.Fatal(err)
	}
	return item
}
func contextLastUsed(t *testing.T, id string) int64 {
	t.Helper()
	list, err := theBrain().ListMemory(brain.MemoryFilter{Status: "all"})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range list.Items {
		if item.ID == id {
			return item.LastUsedAt
		}
	}
	t.Fatal("memory missing")
	return -1
}

func TestDiscussionContextItemsCoverSystemInOrder(t *testing.T) {
	testHome(t)
	if err := MemSave("preferences", "", "preference-marker"); err != nil {
		t.Fatal(err)
	}
	if err := putStoreJSON(bkState, sharedPreferencesKey, sharedPreferences{Page: "preferences"}); err != nil {
		t.Fatal(err)
	}
	vault := primaryMemoryVault(t, theBrain())
	for _, path := range []string{"one.md", "two.md"} {
		if err := os.WriteFile(filepath.Join(vault, path), []byte("amber passage-marker "+path), 0600); err != nil {
			t.Fatal(err)
		}
	}
	e, err := theBrain().get()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"archive-one", "archive-two"} {
		if err := e.Update(brain.Source{ID: id, Label: id, Path: t.TempDir(), Kind: "context", Secondary: true}); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	folder := t.TempDir()
	if err := os.WriteFile(filepath.Join(folder, "included.md"), []byte("file-marker"), 0600); err != nil {
		t.Fatal(err)
	}
	skill, err := saveCapability(Capability{Name: "Review", Instructions: "skill-marker"})
	if err != nil {
		t.Fatal(err)
	}
	p, err := saveProjectContext(ChatProject{Name: "Work", Instructions: "project-marker", Directory: folder, ContextFiles: []string{"included.md"}, CapabilityIDs: []string{skill.ID}, Continuity: &project.Continuity{Core: project.Core{Purpose: "continuity-marker"}}})
	if err != nil {
		t.Fatal(err)
	}
	memory := rememberContextItem(t, "reflex", "global", "memory-marker")
	s := RuntimeSession{ID: "metadata", RuntimeID: "llama.cpp", ProjectID: p.ID, Instructions: "discussion-marker"}
	c := discussionContextFor(s, "amber")
	if c.Problem != "" {
		t.Fatal(c.Problem)
	}
	kinds := []string{}
	labels := []string{}
	total := 0
	byKind := map[string]int{}
	for _, item := range c.Items {
		kinds = append(kinds, item.Kind)
		labels = append(labels, item.Label)
		if item.Source == "" || item.Label == "" || item.Reason == "" || item.Tokens <= 0 {
			t.Fatalf("incomplete item: %+v", item)
		}
		total += item.Tokens
		byKind[item.Kind] += item.Tokens
		if item.Kind == "memory" && (item.Source != memory.ID || item.Class != "reflex" || item.Scope != "global") {
			t.Fatalf("memory provenance: %+v", item)
		}
	}
	want := []string{"global_preferences", "project", "project", "project_files", "brain_passage", "brain_passage", "memory", "skill", "primary_brain", "secondary_brains", "secondary_brains", "discussion_instructions"}
	if !reflect.DeepEqual(kinds, want) {
		t.Fatalf("kinds/order: %v; labels: %v; system: %s", kinds, labels, c.System)
	}
	// Every item maps to the matching rendered segment, including each passage
	// and each announced secondary source, with no aggregate hiding extra parts.
	markers := []string{"preference-marker", "continuity-marker", "project-marker", "Project file included.md", "[primary: " + c.Items[4].Label + "]", "[primary: " + c.Items[5].Label + "]", "memory-marker", "skill-marker", "Primary second brain:", "- " + c.Items[9].Label, "- " + c.Items[10].Label, "discussion-marker"}
	cursor := 0
	for _, marker := range markers {
		offset := strings.Index(c.System[cursor:], marker)
		if offset < 0 {
			t.Fatalf("missing/out-of-order segment: %q", marker)
		}
		cursor += offset + len(marker)
	}
	if total != c.EstimatedTokens || c.EstimatedTokens != brain.Tokens(c.System) || !reflect.DeepEqual(byKind, c.Budget.ByKind) {
		t.Fatalf("token accounting: %+v", c.Budget)
	}
	packText := c.System[strings.Index(c.System, "Loom memory"):strings.Index(c.System, "\n\nSkill:")]
	if c.Budget.Memory.Available != 1500 || c.Budget.Memory.Used != brain.Tokens(packText) || c.Budget.Memory.Classes["reflex"].Used != c.Budget.Memory.Used || c.Budget.Memory.Classes["reflex"].Available != 300 || c.Budget.Memory.Classes["session"].Available != 0 {
		t.Fatalf("memory budget: %+v", c.Budget.Memory)
	}
	pack := projectBrainPack(p, "amber")
	if !strings.Contains(c.System, pack.Text) || len(c.BrainCitations) != len(pack.Citations) {
		t.Fatal("passage text/citations changed during metadata assembly")
	}
	if again := discussionContextFor(s, "amber"); again.System != c.System || again.Revision != c.Revision {
		t.Fatal("unchanged preview changed revision")
	}
	c.Items[0].Reason = "different explanation"
	c.Budget.Memory.Available = 9999
	if discussionContextRevision(s, c) != c.Revision {
		t.Fatal("metadata changed revision without a text change")
	}
	s.Instructions += " changed"
	if discussionContextFor(s, "amber").Revision == c.Revision {
		t.Fatal("system text change did not change revision")
	}
	if contextLastUsed(t, memory.ID) != 0 {
		t.Fatal("context inspection touched memory")
	}
}

type memoryContextAdapter struct {
	id  string
	run func(RuntimeTurn, ChatCallback)
}

func (a memoryContextAdapter) Descriptor() RuntimeDescriptor {
	return RuntimeDescriptor{ID: a.id, Name: "Context fixture", Kind: "harness", Implemented: true, Capabilities: []string{"chat"}}
}
func (a memoryContextAdapter) Run(_ context.Context, turn RuntimeTurn, emit ChatCallback) ([]Message, error) {
	a.run(turn, emit)
	return nil, nil
}
func TestDiscussionMemoryPreviewReadOnlySendTouches(t *testing.T) {
	testHome(t)
	item := rememberContextItem(t, "reflex", "global", "Always check context fixture instructions.")
	excluded := rememberContextItem(t, "reflex", "project:other", "Must stay in the other project.")
	m := newRuntimeSessions()
	sent := make(chan []Message, 1)
	adapter := memoryContextAdapter{id: "memory-fixture", run: func(turn RuntimeTurn, emit ChatCallback) { sent <- turn.Messages; emit(StreamEvent{Content: "Answer"}) }}
	isolateRuntimeRegistry(t, adapter)
	s := RuntimeSession{ID: "memory-send", RuntimeID: adapter.id, Title: "Test", Status: "idle", Messages: []Message{}}
	if err := putStoreJSON(bkRuntimeSessions, s.ID, s); err != nil {
		t.Fatal(err)
	}
	preview := prepareDiscussion(s, "Question")
	again := prepareDiscussion(s, "Question")
	if preview.Context.Revision != again.Context.Revision || contextLastUsed(t, item.ID) != 0 {
		t.Fatal("preview mutated memory/revision")
	}
	if err := m.start(s.ID, "stale-request", "Question", "stale"); err == nil {
		t.Fatal("stale send accepted")
	}
	if contextLastUsed(t, item.ID) != 0 {
		t.Fatal("rejected send touched memory")
	}
	if err := m.start(s.ID, "send-request", "Question", preview.Context.Revision); err != nil {
		t.Fatal(err)
	}
	awaitCloudFinished(t, m, s.ID)
	if !reflect.DeepEqual(<-sent, preview.Messages) {
		t.Fatal("send differs from preview")
	}
	used := contextLastUsed(t, item.ID)
	if used == 0 || contextLastUsed(t, excluded.ID) != 0 {
		t.Fatal("send did not touch exactly included memory")
	}
	if after := discussionContextFor(RuntimeSession{RuntimeID: s.RuntimeID}, "Question"); after.System != preview.Context.System {
		t.Fatal("touch changed existing memory ranking or system text")
	}
	after := prepareDiscussion(s, "Question")
	if after.Context.Revision == preview.Context.Revision || !strings.Contains(after.Context.System, "Answer") {
		t.Fatal("completed turn did not add its handoff")
	}
	if err := m.start(s.ID, "send-request", "Question", "stale"); err != nil {
		t.Fatal(err)
	}
	if contextLastUsed(t, item.ID) != used {
		t.Fatal("idempotent resend touched memory again")
	}
}
func TestNativeMemoryPreparationReadOnlyGenerationTouches(t *testing.T) {
	testHome(t)
	item := rememberContextItem(t, "reflex", "global", "Respect native fixture instructions.")
	excluded := rememberContextItem(t, "reflex", "agent:other", "Other agent memory.")
	oldSessions := workspaceSessions
	workspaceSessions = newRuntimeSessions()
	t.Cleanup(func() { workspaceSessions = oldSessions })
	c := &Conversation{ID: "native-memory", Messages: []Message{{Role: "user", Content: "Question"}}}
	c.cond = sync.NewCond(&c.mu)
	s := RuntimeSession{ID: "common-native", RuntimeID: "llama.cpp", NativeArchive: c.ID}
	if err := putStoreJSON(bkRuntimeSessions, s.ID, s); err != nil {
		t.Fatal(err)
	}
	before, err := nativePreparedContext(c.ID, "", "Question")
	if err != nil || !strings.Contains(before, item.Text) {
		t.Fatalf("native context: %q %v", before, err)
	}
	if contextLastUsed(t, item.ID) != 0 {
		t.Fatal("native preparation touched memory")
	}
	calls := 0
	adapter := memoryContextAdapter{id: "llama.cpp", run: func(turn RuntimeTurn, emit ChatCallback) {
		calls++
		if len(turn.Messages) == 0 || !strings.Contains(turn.Messages[0].Content.(string), before) {
			t.Error("native send lost prepared memory")
		}
		if contextLastUsed(t, item.ID) == 0 {
			t.Error("native send did not touch")
		}
		emit(StreamEvent{Content: "Answer"})
	}}
	isolateRuntimeRegistry(t, adapter)
	c.generate(context.Background(), Caps{}, .7, c.epoch)
	if calls != 1 || contextLastUsed(t, excluded.ID) != 0 {
		t.Fatalf("native send calls/scope: %d", calls)
	}
}
