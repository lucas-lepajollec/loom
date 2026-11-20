package loom

import (
	"strings"
	"sync"
	"testing"

	"github.com/lucas-lepajollec/loom/internal/loom/discussion"
)

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
	if first.System != second.System || strings.Contains(second.Extras, fact.Text) {
		t.Fatal("native snapshot/extras wrong")
	}
	outgoing := discussion.WithContextExtras(c.Messages, second.Extras)
	if strings.Contains(msgText(outgoing[len(outgoing)-1]), fact.Text) {
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
