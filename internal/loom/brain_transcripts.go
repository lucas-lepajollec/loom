package loom

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/lucas-lepajollec/loom/internal/loom/brain"
)

func transcriptMessageEntries(messages []Message) []brain.TranscriptEntry {
	out := []brain.TranscriptEntry{}
	for _, m := range messages {
		if m.Role == "user" || m.Role == "assistant" {
			if text := msgText(m); text != "" {
				out = append(out, brain.TranscriptEntry{Role: m.Role, Text: text})
			}
		}
		for _, call := range m.ToolCalls {
			out = append(out, brain.TranscriptEntry{Tool: call.Function.Name})
		}
	}
	return out
}
func sessionTranscript(s RuntimeSession) brain.Transcript {
	t := brain.Transcript{ID: s.ID, Title: s.Title, ProjectID: s.ProjectID, Executor: s.RuntimeID, Model: s.Model, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt}
	if p, ok := getProject(s.ProjectID); ok {
		t.Project = p.Name
	}
	// Messages is the full visible journal. PortableMessages can be compacted.
	for i, m := range s.Messages {
		t.Entries = append(t.Entries, transcriptMessageEntries([]Message{m})...)
		if m.Role != "assistant" {
			continue
		}
		for _, turn := range s.Turns {
			if turn.MessageIndex != i-1 {
				continue
			}
			for _, name := range turn.ToolSummaries {
				t.Entries = append(t.Entries, brain.TranscriptEntry{Tool: name})
			}
			seen := map[string]bool{}
			for _, event := range turn.ACPEvents {
				kind, _ := event["type"].(string)
				if kind != "tool_start" && kind != "tool_call" && kind != "tool_update" {
					continue
				}
				tool, _ := event["tool"].(map[string]any)
				id, _ := tool["id"].(string)
				if id != "" && seen[id] {
					continue
				}
				seen[id] = true
				name, _ := tool["title"].(string)
				if name == "" {
					name, _ = tool["kind"].(string)
				}
				if name != "" {
					t.Entries = append(t.Entries, brain.TranscriptEntry{Tool: name})
				}
			}
			for _, event := range turn.Events {
				if event.Name != "" {
					t.Entries = append(t.Entries, brain.TranscriptEntry{Tool: event.Name})
				}
			}
		}
	}
	return t
}
func nativeTranscript(a convArchive, model string) brain.Transcript {
	t := brain.Transcript{ID: a.ID, Title: a.Title, ProjectID: a.ProjectID, Executor: "llama.cpp", Model: model, CreatedAt: a.SavedAt, UpdatedAt: a.SavedAt}
	if p, ok := getProject(a.ProjectID); ok {
		t.Project = p.Name
	}
	if len(a.Log) == 0 {
		t.Entries = transcriptMessageEntries(archivePortableText(&a))
		return t
	}
	var assistant strings.Builder
	flush := func() {
		if assistant.Len() > 0 {
			t.Entries = append(t.Entries, brain.TranscriptEntry{Role: "assistant", Text: assistant.String()})
			assistant.Reset()
		}
	}
	for i, event := range a.Log {
		if i == 0 {
			t.UpdatedAt = event.TS
			t.CreatedAt = event.TS
		}
		t.UpdatedAt = max(t.UpdatedAt, event.TS)
		if user, ok := event.Delta["user"].(string); ok {
			flush()
			t.Entries = append(t.Entries, brain.TranscriptEntry{Role: "user", Text: user})
		}
		if text, ok := event.Delta["content"].(string); ok {
			assistant.WriteString(text)
		}
		if tool, ok := event.Delta["tool_used"].(map[string]any); ok && tool["done"] == true {
			flush()
			name, _ := tool["name"].(string)
			t.Entries = append(t.Entries, brain.TranscriptEntry{Tool: name})
		}
		if raw, ok := event.Delta["runtime_turn"]; ok {
			data, _ := json.Marshal(raw)
			var turn struct {
				Model string `json:"model"`
			}
			if json.Unmarshal(data, &turn) == nil && turn.Model != "" {
				t.Model = turn.Model
			}
		}
	}
	flush()
	return t
}
func (s *brainService) queueTranscript(t brain.Transcript) {
	s.transcriptMu.Lock()
	previous := s.transcriptTail
	done := make(chan struct{})
	s.transcriptTail = done
	transcriptJobs.Add(1)
	s.transcriptMu.Unlock()
	go func() {
		defer transcriptJobs.Done()
		defer close(done)
		if previous != nil {
			<-previous
		}
		store, err := s.memoryStore()
		if err == nil {
			_, err = store.WriteTranscript(t)
		}
		if err != nil {
			log.Printf("Brain transcript: %v", err)
		} else {
			s.refreshMemoryIndex()
		}
	}()
}
func (s *brainService) queueSessionTranscript(session RuntimeSession) {
	t := sessionTranscript(cloneRuntimeSession(session))
	if session.RuntimeID == "llama.cpp" && session.NativeArchive != "" {
		var archive convArchive
		ok := false
		if active := conv.snapshotForSession(); active != nil && active.ID == session.NativeArchive {
			archive = *active
			ok = true
		} else {
			ok = getStoreJSON(bkChatHist, session.NativeArchive, &archive)
		}
		if ok {
			native := nativeTranscript(archive, session.Model)
			native.ID, native.ProjectID, native.Project, native.Title, native.CreatedAt = session.ID, session.ProjectID, t.Project, session.Title, session.CreatedAt
			t = native
		}
	}
	s.queueTranscript(t)
}
func (s *brainService) queueNativeTranscript(a convArchive, model string) {
	t := nativeTranscript(a, model)
	for id := range allKV(bkRuntimeSessions) {
		var session RuntimeSession
		if getStoreJSON(bkRuntimeSessions, id, &session) && session.NativeArchive == a.ID {
			t.ID, t.ProjectID, t.Title = session.ID, session.ProjectID, session.Title
			t.CreatedAt = session.CreatedAt
			if p, ok := getProject(t.ProjectID); ok {
				t.Project = p.Name
			}
			break
		}
	}
	s.queueTranscript(t)
}
func queueDiscussionTranscript(id string) {
	var session RuntimeSession
	if getStoreJSON(bkRuntimeSessions, id, &session) {
		theBrain().queueSessionTranscript(session)
		return
	}
	if a := conv.snapshotForSession(); a != nil && a.ID == id {
		theBrain().queueNativeTranscript(*a, engineRequestModel())
		return
	}
	var archive convArchive
	if getStoreJSON(bkChatHist, id, &archive) {
		theBrain().queueNativeTranscript(archive, "")
	}
}
func (s *brainService) memoryDocuments(ctx context.Context, emit func(brain.Document) bool) error {
	store, err := s.memoryStore()
	if err != nil {
		return err
	}
	return store.Documents(ctx.Done(), false, emit)
}
func (s *brainService) discussionDocuments(ctx context.Context, emit func(brain.Document) bool) error {
	store, err := s.memoryStore()
	if err != nil {
		return err
	}
	// Backfill older journals once, while retaining canonical files edited by
	// agents or people. Completed turns use the explicit write hooks.
	discussions, err := memoryDiscussions(ctx)
	if err != nil {
		return err
	}
	for _, d := range discussions {
		path, err := store.DiscussionPath(d.Session.ID)
		if err != nil {
			return err
		}
		if path == "" && len(d.Transcript.Entries) > 0 {
			if _, err = store.WriteTranscript(d.Transcript); err != nil {
				return err
			}
		}
	}
	return store.Documents(ctx.Done(), true, emit)
}

type memoryDiscussion struct {
	Session    RuntimeSession
	Transcript brain.Transcript
}

func memoryDiscussions(ctx context.Context) ([]memoryDiscussion, error) {
	active := conv.snapshotForSession()
	out := []memoryDiscussion{}
	bound := map[string]bool{}
	_, err := brainRecords(ctx, bkRuntimeSessions, func(id string, b []byte) bool {
		var session RuntimeSession
		if json.Unmarshal(b, &session) != nil || session.ID != id {
			return true
		}
		t := sessionTranscript(session)
		if session.NativeArchive != "" {
			bound[session.NativeArchive] = true
			var a convArchive
			if active != nil && active.ID == session.NativeArchive {
				a = *active
			} else if !getStoreJSON(bkChatHist, session.NativeArchive, &a) {
				return true
			}
			t = nativeTranscript(a, session.Model)
			t.ID, t.ProjectID, t.Title, t.CreatedAt = session.ID, session.ProjectID, session.Title, session.CreatedAt
		}
		out = append(out, memoryDiscussion{session, t})
		return true
	})
	if err != nil {
		return nil, err
	}
	add := func(a convArchive) {
		if bound[a.ID] {
			return
		}
		t := nativeTranscript(a, "")
		session := RuntimeSession{ID: a.ID, ProjectID: a.ProjectID, RuntimeID: "llama.cpp", Model: t.Model, UpdatedAt: t.UpdatedAt}
		out = append(out, memoryDiscussion{session, t})
	}
	_, err = brainRecords(ctx, bkChatHist, func(id string, b []byte) bool {
		var a convArchive
		if json.Unmarshal(b, &a) == nil && (active == nil || id != active.ID) {
			add(a)
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	if active != nil {
		add(*active)
	}
	return out, nil
}
func transcriptBody(t brain.Transcript) string {
	var b strings.Builder
	for _, entry := range t.Entries {
		if entry.Tool != "" {
			fmt.Fprintf(&b, "Tool: %s\n", strings.Join(strings.Fields(entry.Tool), " "))
		} else {
			fmt.Fprintf(&b, "%s:\n%s\n\n", entry.Role, entry.Text)
		}
	}
	return b.String()
}
