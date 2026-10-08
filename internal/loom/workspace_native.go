package loom

import (
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"time"
)

// Portable text is a projection of the display journal, not a replacement for
// native tool/compaction state. The source archive remains intact.
func nativePortableMessageCount(events []LogEvent, messages []Message) int {
	count, assistant := 0, false
	if len(events) == 0 {
		for _, msg := range messages {
			if text, ok := msg.Content.(string); ok && text != "" && (msg.Role == "user" || msg.Role == "assistant") {
				count++
			}
		}
		return count
	}
	for _, event := range events {
		if _, ok := event.Delta["user"].(string); ok {
			count++
			assistant = false
		}
		if text, ok := event.Delta["content"].(string); ok && text != "" && !assistant {
			count++
			assistant = true
		}
	}
	return count
}

func archivePortableText(a *convArchive) []Message {
	out := []Message{}
	if len(a.Log) == 0 {
		for _, msg := range a.Messages {
			if text, ok := msg.Content.(string); ok && text != "" && (msg.Role == "user" || msg.Role == "assistant") {
				out = append(out, Message{Role: msg.Role, Content: text})
			}
		}
		return out
	}
	for _, event := range a.Log {
		if text, ok := event.Delta["user"].(string); ok {
			out = append(out, Message{Role: "user", Content: text})
		}
		if text, ok := event.Delta["content"].(string); ok && text != "" {
			if len(out) > 0 && out[len(out)-1].Role == "assistant" {
				out[len(out)-1].Content = out[len(out)-1].Content.(string) + text
			} else {
				out = append(out, Message{Role: "assistant", Content: text})
			}
		}
	}
	return out
}

func appendNativeText(a *convArchive, messages []Message) {
	for _, msg := range messages {
		a.Messages = append(a.Messages, msg)
		a.Seq++
		key := "content"
		if msg.Role == "user" {
			key = "user"
		}
		a.Log = append(a.Log, LogEvent{Seq: a.Seq, TS: time.Now().UnixMilli(), Delta: map[string]any{key: msg.Content, "portable_text": true}})
		if msg.Role == "assistant" {
			a.Seq++
			a.Log = append(a.Log, LogEvent{Seq: a.Seq, Delta: map[string]any{"turn_done": true}})
		}
	}
}

// Metadata is replayable display data, not part of the model-visible prompt.
// Never relabel imported answers with whichever local model is selected now.
func nativeTurnRecords(a *convArchive) []RuntimeTurnRecord {
	var out []RuntimeTurnRecord
	index := -1
	assistant := false
	var stats *StatsEvent
	for _, ev := range a.Log {
		if _, ok := ev.Delta["user"]; ok {
			index++
			assistant = false
			stats = nil
		}
		if text, ok := ev.Delta["content"].(string); ok && text != "" && !assistant {
			index++
			assistant = true
		}
		if raw, ok := ev.Delta["stats"]; ok {
			data, _ := json.Marshal(raw)
			var value StatsEvent
			if json.Unmarshal(data, &value) == nil {
				stats = &value
			}
		}
		if ev.Delta["turn_done"] == true && assistant {
			var turn RuntimeTurnRecord
			if raw, ok := ev.Delta["runtime_turn"]; ok {
				data, _ := json.Marshal(raw)
				_ = json.Unmarshal(data, &turn)
			}
			turn.MessageIndex = index
			if turn.Stats == nil {
				turn.Stats = stats
			}
			out = append(out, turn)
		}
	}
	return out
}

func annotateNativeTurns(a *convArchive, turns []RuntimeTurnRecord) {
	index := -1
	assistant := false
	for _, ev := range a.Log {
		if _, ok := ev.Delta["user"]; ok {
			index++
			assistant = false
		}
		if text, ok := ev.Delta["content"].(string); ok && text != "" && !assistant {
			index++
			assistant = true
		}
		if ev.Delta["turn_done"] != true || !assistant {
			continue
		}
		if _, exists := ev.Delta["runtime_turn"]; exists {
			continue
		}
		for _, turn := range turns {
			if turn.MessageIndex == index {
				ev.Delta["runtime_turn"] = turn
				break
			}
		}
	}
}

// Older workspace bindings already know the origin of imported answers even
// when their native display journal predates runtime_turn metadata.
func annotateLoadedNativeConversation(c *Conversation) {
	sessions := workspaceSessions.list()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.Generating {
		return
	}
	for _, s := range sessions {
		if s.NativeArchive != "" && s.NativeArchive == c.ID {
			annotateNativeTurns(&convArchive{Log: c.Log}, s.Turns)
			return
		}
	}
}

// Restore the existing Conversation pipeline, including attachments, presets,
// model prompt, tools, reasoning, compaction and replay. No new local executor.
func (m *runtimeSessions) activateLocal(id string, c *Conversation) (RuntimeSession, error) {
	m.nativeMu.Lock()
	defer m.nativeMu.Unlock()
	c.mu.Lock()
	generating := c.Generating
	c.mu.Unlock()
	if generating {
		return RuntimeSession{}, errors.New("stop the local response before changing discussions")
	}
	c.upsertSession()
	m.mu.Lock()
	s, ok := m.getLocked(id)
	if !ok || s.RuntimeID != "llama.cpp" || m.runs[id] != nil || m.preparing[id] {
		m.mu.Unlock()
		return s, errors.New("local discussion unavailable or busy")
	}
	var a *convArchive
	if s.NativeArchive != "" {
		a, _ = loadArchive(s.NativeArchive)
	} else if s.SourceArchive != "" {
		a, _ = loadArchive(s.SourceArchive)
	}
	prefix := []Message{}
	if a != nil {
		prefix = archivePortableText(a)
	}
	if a == nil || !portablePrefix(prefix, s.Messages) {
		// An edited native archive is retained; never overwrite its rich history.
		a = &convArchive{}
		prefix = []Message{}
	}
	if s.NativeArchive == "" || a.ID != s.NativeArchive {
		a.ID = newSessionID()
		s.NativeArchive = a.ID
	}
	if len(a.Log) == 0 && len(prefix) > 0 {
		a.Messages = nil
		appendNativeText(a, prefix)
	}
	appendNativeText(a, s.Messages[len(prefix):])
	if s.PortableMessages != nil && !reflect.DeepEqual(portableText(a.Messages), s.PortableMessages) {
		a.Messages = append([]Message{}, s.PortableMessages...)
	}
	annotateNativeTurns(a, s.Turns)
	a.Title, a.ProjectID, a.SavedAt = s.Title, s.ProjectID, time.Now().UnixMilli()
	a.Turns = countUserTurns(a.Log)
	err := saveArchive(a)
	if err == nil {
		err = putStoreJSON(bkRuntimeSessions, s.ID, s)
	}
	m.mu.Unlock()
	if err != nil {
		return s, err
	}
	// Recheck under StartTurn's lock. Never implicitly stop a native turn.
	c.mu.Lock()
	if c.Generating {
		c.mu.Unlock()
		return s, errors.New("a local response just started; try again after it stops")
	}
	c.ID, c.ActiveTitle, c.ActiveProject, c.ActiveFav = a.ID, a.Title, a.ProjectID, a.Fav
	c.Messages, c.Log = append([]Message(nil), a.Messages...), append([]LogEvent(nil), a.Log...)
	c.Seq, c.CtxUsed, c.CompactCount = a.Seq, a.CtxUsed, a.CompactCount
	c.epoch++
	c.pendingReplay = true
	c.cond.Broadcast()
	c.mu.Unlock()
	c.persist()
	return s, nil
}

func (m *runtimeSessions) nativeRunning(s RuntimeSession) bool {
	conv.mu.Lock()
	defer conv.mu.Unlock()
	return conv.Generating && (conv.ID == s.NativeArchive || conv.ID == s.SourceArchive)
}

func (m *runtimeSessions) syncNativeArchive(a *convArchive) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id := range allKV(bkRuntimeSessions) {
		s, ok := m.getLocked(id)
		if !ok || s.NativeArchive != a.ID || s.RuntimeID != "llama.cpp" || m.runs[id] != nil {
			continue
		}
		s.Messages = archivePortableText(a)
		s.PortableMessages = portableText(a.Messages)
		seen := map[int]bool{}
		for _, turn := range s.Turns {
			seen[turn.MessageIndex] = true
		}
		for _, turn := range nativeTurnRecords(a) {
			if !seen[turn.MessageIndex] {
				s.Turns = append(s.Turns, turn)
			}
		}
		s.Title, s.ProjectID = a.Title, a.ProjectID
		s.Model, s.Status, s.UpdatedAt = ReadConfig()["MODEL"], "idle", time.Now().UnixMilli()
		_ = putStoreJSON(bkRuntimeSessions, id, s)
		summary := ""
		if a.CompactCount > len(s.Compactions) {
			for _, msg := range a.Messages {
				if strings.HasPrefix(msgText(msg), compactSummaryPrefix) {
					summary = msgText(msg)
				}
			}
		}
		if summary != "" {
			_ = saveDiscussionHandoff(s, summary)
		}
	}
}

func nativeDiscussionContext(archiveID, projectID string) string {
	for _, s := range workspaceSessions.list() {
		if s.NativeArchive == archiveID && s.RuntimeID == "llama.cpp" {
			return discussionContext(s).System
		}
	}
	return projectContext(projectID)
}

// The native path uses the current draft exactly once, like every other
// executor. Selected preferences or projects that became unreadable block send.
func nativePreparedContext(archiveID, projectID, query string) (string, error) {
	c, err := nativePreparedDiscussionContext(archiveID, projectID, query)
	return c.System, err
}

func nativePreparedDiscussionContext(archiveID, projectID, query string) (DiscussionContext, error) {
	session := nativeContextSession(archiveID, projectID)
	c := discussionContextFor(session, query)
	if c.Problem != "" {
		return c, errors.New(c.Problem)
	}
	return c, nil
}

func nativeContextSession(archiveID, projectID string) RuntimeSession {
	session := RuntimeSession{ID: archiveID, RuntimeID: "llama.cpp", ProjectID: projectID}
	for _, s := range workspaceSessions.list() {
		if s.NativeArchive == archiveID && s.RuntimeID == "llama.cpp" {
			session = s
			break
		}
	}
	return session
}

func handleRuntimeSessionLocal(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	s, err := workspaceSessions.activateLocal(req.ID, conv)
	if err != nil {
		sendJSON(w, 409, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "session": clientSession(s), "context": discussionContext(s)})
}
