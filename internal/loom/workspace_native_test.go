package loom

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestNativeTurnProvenanceSurvivesReplayWithoutChangingContext(t *testing.T) {
	a := &convArchive{}
	appendNativeText(a, []Message{{Role: "user", Content: "question"}, {Role: "assistant", Content: "harness answer"}, {Role: "user", Content: "local question"}, {Role: "assistant", Content: "local answer"}})
	turns := []RuntimeTurnRecord{{MessageIndex: 1, RuntimeID: "antigravity", ProviderName: "Antigravity", Model: "gemini-low", Usage: &RuntimeUsage{Output: 7}, DurationSeconds: 2}, {MessageIndex: 3, RuntimeID: "llama.cpp", Model: "original.gguf", Stats: &StatsEvent{GenTokens: 8, GenPerSecond: 4}}}
	before, _ := json.Marshal(archivePortableText(a))
	annotateNativeTurns(a, turns)
	annotateNativeTurns(a, []RuntimeTurnRecord{{MessageIndex: 1, RuntimeID: "llama.cpp", Model: "wrong.gguf"}})
	encoded, _ := json.Marshal(a)
	var restored convArchive
	_ = json.Unmarshal(encoded, &restored)
	got := nativeTurnRecords(&restored)
	if len(got) != 2 || got[0].Model != "gemini-low" || got[0].Usage.Output != 7 || got[0].DurationSeconds != 2 || got[1].Model != "original.gguf" || got[1].Stats.GenTokens != 8 {
		t.Fatalf("provenance lost: %+v", got)
	}
	after, _ := json.Marshal(archivePortableText(&restored))
	if string(before) != string(after) {
		t.Fatal("display metadata entered portable context")
	}
	unknown := &convArchive{}
	appendNativeText(unknown, []Message{{Role: "user", Content: "q"}, {Role: "assistant", Content: "a"}})
	if nativeTurnRecords(unknown)[0].RuntimeID != "" {
		t.Fatal("legacy attribution guessed")
	}
}

func TestNativeLoadedConversationRecoversKnownImportedOrigin(t *testing.T) {
	testHome(t)
	old := workspaceSessions
	workspaceSessions = newRuntimeSessions()
	t.Cleanup(func() { workspaceSessions = old })
	a := &convArchive{ID: "bound"}
	appendNativeText(a, []Message{{Role: "user", Content: "q"}, {Role: "assistant", Content: "a"}})
	s := RuntimeSession{ID: "shared", NativeArchive: a.ID, RuntimeID: "llama.cpp", Status: "idle", Turns: []RuntimeTurnRecord{{MessageIndex: 1, RuntimeID: "antigravity", Model: "original-native", Usage: &RuntimeUsage{Output: 9}}}}
	if err := putStoreJSON(bkRuntimeSessions, s.ID, s); err != nil {
		t.Fatal(err)
	}
	c := newHistTestConv()
	c.ID = a.ID
	c.Log = a.Log
	annotateLoadedNativeConversation(c)
	got := nativeTurnRecords(&convArchive{Log: c.Log})
	if len(got) != 1 || got[0].RuntimeID != "antigravity" || got[0].Usage.Output != 9 {
		t.Fatalf("known origin lost: %+v", got)
	}
}

func TestNativeBridgePreservesRichLocalArchiveAndAddsCloudText(t *testing.T) {
	testHome(t)
	m := newRuntimeSessions()
	oldManager, oldConv := workspaceSessions, conv
	c := newHistTestConv()
	workspaceSessions, conv = m, c
	t.Cleanup(func() { workspaceSessions, conv = oldManager, oldConv })
	SetConfigKey("MODEL", "fixture.gguf")
	source := &convArchive{ID: newSessionID(), Title: "Original", Messages: []Message{
		{Role: "user", Content: "Local question"},
		{Role: "assistant", Content: "Local answer"},
		{Role: "tool", Content: "Private tool output"},
	}, Log: []LogEvent{
		{Seq: 1, Delta: map[string]any{"user": "Local question", "files": []string{"private.txt"}}},
		{Seq: 2, Delta: map[string]any{"reasoning_content": "Private reasoning"}},
		{Seq: 3, Delta: map[string]any{"tool_used": map[string]any{"name": "native-tool"}}},
		{Seq: 4, Delta: map[string]any{"content": "Local answer"}},
	}, Seq: 4, CtxUsed: 99, CompactCount: 2}
	if err := saveArchive(source); err != nil {
		t.Fatal(err)
	}
	s, err := m.importArchive(source.ID)
	if err != nil {
		t.Fatal(err)
	}
	s.Messages = append(s.Messages, Message{Role: "user", Content: "Cloud question"}, Message{Role: "assistant", Content: "Cloud answer"})
	s.Instructions = "Same discussion instruction"
	if err := putStoreJSON(bkRuntimeSessions, s.ID, s); err != nil {
		t.Fatal(err)
	}
	active, err := m.activateLocal(s.ID, c)
	if err != nil {
		t.Fatal(err)
	}
	if active.ID != s.ID || active.NativeArchive == source.ID || active.NativeArchive == "" {
		t.Fatal("forked the discussion or overwrote the original")
	}
	if !reflect.DeepEqual(archivePortableText(c.snapshotForSession()), s.Messages) {
		t.Fatal("portable transcript lost")
	}
	if c.CompactCount != 2 || c.CtxUsed != 99 {
		t.Fatal("native state lost")
	}
	a, _ := loadArchive(active.NativeArchive)
	if len(a.Messages) != 5 || len(a.Log) < len(source.Log) || a.Log[1].Delta["reasoning_content"] != "Private reasoning" {
		t.Fatal("native tools/reasoning lost")
	}
	original, _ := loadArchive(source.ID)
	sourceJSON, _ := json.Marshal(source.Log)
	originalJSON, _ := json.Marshal(original.Log)
	if string(sourceJSON) != string(originalJSON) || len(original.Messages) != 3 {
		t.Fatal("source archive mutated")
	}
	if got := nativeDiscussionContext(active.NativeArchive, ""); !strings.Contains(got, s.Instructions) {
		t.Fatal("discussion instruction missing from native context")
	}
	again, err := m.activateLocal(s.ID, c)
	if err != nil || again.NativeArchive != active.NativeArchive {
		t.Fatal("reactivation duplicated native archive")
	}
	// Native local completion projects only visible text back into the same fil.
	c.appendDelta(c.epoch, map[string]any{"user": "Back in local"})
	c.appendDelta(c.epoch, map[string]any{"content": "Native completion"})
	c.persist()
	mirrored, _ := m.get(s.ID)
	if len(mirrored.Messages) != 6 || mirrored.Messages[5].Content != "Native completion" || len(m.list()) != 1 {
		t.Fatal("native continuation not mirrored")
	}
	encoded, _ := json.Marshal(mirrored.Messages)
	if strings.Contains(string(encoded), "Private") {
		t.Fatal("private runtime state escaped")
	}
	// Native completion must not contaminate a subsequently selected cloud route.
	mirrored.RuntimeID = "openai-compatible"
	putStoreJSON(bkRuntimeSessions, s.ID, mirrored)
	c.appendDelta(c.epoch, map[string]any{"content": "Not part of cloud"})
	c.persist()
	cloud, _ := m.get(s.ID)
	if len(cloud.Messages) != 6 || cloud.Messages[5].Content != "Native completion" {
		t.Fatal("stale native write changed cloud history")
	}
}

func TestNativeBridgeRejectsRunningAndCloudSessions(t *testing.T) {
	testHome(t)
	m := newRuntimeSessions()
	c := newHistTestConv()
	s, err := m.create("", "", false)
	if err != nil {
		t.Fatal(err)
	}
	c.Generating = true
	if _, err = m.activateLocal(s.ID, c); err == nil {
		t.Fatal("implicitly stopped a running native turn")
	}
	if !c.Generating {
		t.Fatal("native run was stopped")
	}
	c.Generating = false
	s.RuntimeID = "openai-compatible"
	putStoreJSON(bkRuntimeSessions, s.ID, s)
	if _, err = m.activateLocal(s.ID, c); err == nil {
		t.Fatal("cloud route activated locally")
	}
}

func TestNativeProjectionUsesJournalAcrossCompaction(t *testing.T) {
	a := &convArchive{Messages: []Message{{Role: "system", Content: "Native summary"}}, Log: []LogEvent{
		{Delta: map[string]any{"user": "Original question"}},
		{Delta: map[string]any{"content": "First "}},
		{Delta: map[string]any{"reasoning_content": "Private"}},
		{Delta: map[string]any{"content": "answer"}},
	}}
	want := []Message{{Role: "user", Content: "Original question"}, {Role: "assistant", Content: "First answer"}}
	if !reflect.DeepEqual(archivePortableText(a), want) {
		t.Fatal("exported compacted/private model state instead of visible history")
	}
}

func TestNativeImportedTextRemainsInertAfterReplayAndCompaction(t *testing.T) {
	c := newHistTestConv()
	a := &convArchive{}
	appendNativeText(a, []Message{{Role: "assistant", Content: "<img src=x onerror=alert(1)>"}})
	c.Log = a.Log
	c.compactLogLocked()
	events := coalesceReplay(c.Log, 0)
	if len(events) == 0 || events[0]["portable_text"] != true {
		t.Fatal("untrusted imported text lost its inert rendering marker")
	}
}
