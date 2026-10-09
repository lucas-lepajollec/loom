package loom

import (
	"encoding/json"
	"errors"
	"time"
)

// Display provenance is persisted with messages, but stripped from inference.
func modelMessages(messages []Message) []Message {
	out := append([]Message(nil), messages...)
	for i := range out {
		out[i].Source = ""
		out[i].SourceID = ""
	}
	return out
}
func voiceAlreadyInjected(messages []Message, id string) bool {
	for _, m := range messages {
		if m.Source == "voice" && m.SourceID == id {
			return true
		}
	}
	return false
}
func voiceMessages(messages []Message, id string) []Message {
	out := modelMessages(messages)
	for i := range out {
		out[i].Source = "voice"
		out[i].SourceID = id
	}
	return out
}

// Same lock order as native activation/start. No generation, native session
// materialization or model selection occurs here. Busy targets can be retried.
func injectVoiceExchange(id, voiceID string, messages []Message) error {
	if len(messages) == 0 {
		return nil
	}
	m := workspaceSessions
	m.nativeMu.Lock()
	defer m.nativeMu.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	conv.mu.Lock()
	defer conv.mu.Unlock()
	s, workspace := m.getLocked(id)
	if workspace && (m.runs[id] != nil || m.preparing[id]) {
		return errors.New("discussion busy; end voice after the discussion turn finishes")
	}
	archiveID := id
	native := !workspace
	if workspace && s.RuntimeID == "llama.cpp" && s.NativeArchive != "" {
		archiveID = s.NativeArchive
		native = true
	}
	added := voiceMessages(messages, voiceID)
	if native {
		active := conv.ID == archiveID
		if active && conv.Generating {
			return errors.New("discussion busy; end voice after the discussion turn finishes")
		}
		a, exists := loadArchive(archiveID)
		if active && (a == nil || !voiceAlreadyInjected(a.Messages, voiceID)) {
			a = &convArchive{FrozenSnapshot: conv.FrozenSnapshot, ContextExtras: conv.ContextExtras, ID: conv.ID, Title: conv.ActiveTitle, Fav: conv.ActiveFav, ProjectID: conv.ActiveProject, Messages: append([]Message(nil), conv.Messages...), Log: append([]LogEvent(nil), conv.Log...), Seq: conv.Seq, CtxUsed: conv.CtxUsed, CompactCount: conv.CompactCount}
			exists = true
		}
		if !exists {
			return errors.New("discussion not found")
		}
		if !voiceAlreadyInjected(a.Messages, voiceID) {
			appendNativeText(a, added)
		}
		if a.Title == "" {
			a.Title = archiveTitle(a.Log)
		}
		a.Turns = countUserTurns(a.Log)
		a.SavedAt = time.Now().UnixMilli()
		if err := saveArchive(a); err != nil {
			return err
		}
		if active {
			conv.Messages, conv.Log, conv.Seq = a.Messages, a.Log, a.Seq
			// Keep the active snapshot coherent even if a following mirror write fails.
			b, err := json.Marshal(conv)
			if err != nil {
				return err
			}
			if err = putStoreBytes(bkChat, "conversation", b); err != nil {
				return err
			}
			conv.cond.Broadcast()
		}
		// Mirror every local binding, including direct legacy archive injections.
		published := []RuntimeSession{}
		for sid := range allKV(bkRuntimeSessions) {
			bound, ok := m.getLocked(sid)
			if !ok || bound.RuntimeID != "llama.cpp" || bound.NativeArchive != archiveID {
				continue
			}
			already := voiceAlreadyInjected(bound.Messages, voiceID)
			bound.Messages = archivePortableText(a)
			bound.PortableMessages = portableText(a.Messages)
			bound.UpdatedAt = a.SavedAt
			if err := putStoreJSON(bkRuntimeSessions, sid, bound); err != nil {
				return err
			}
			if !already {
				published = append(published, bound)
			}
		}
		for _, bound := range published {
			publishVoiceMessagesLocked(m, bound, added)
		}
		return nil
	}
	if voiceAlreadyInjected(s.Messages, voiceID) {
		return nil
	} else {
		s.Messages = append(s.Messages, added...)
		if s.PortableMessages != nil {
			s.PortableMessages = append(s.PortableMessages, added...)
		}
		s.UpdatedAt = time.Now().UnixMilli()
	}
	if err := putStoreJSON(bkRuntimeSessions, id, s); err != nil {
		return err
	}
	// Publish ordinary text events; no turn is submitted to an executor.
	publishVoiceMessagesLocked(m, s, added)
	return nil
}

func publishVoiceMessagesLocked(m *runtimeSessions, s RuntimeSession, messages []Message) {
	for _, msg := range messages {
		if msg.Role == "user" {
			m.publishLocked(s.ID, DiscussionEvent{"type": "turn_start", "text": msg.Content, "source": "voice", "portable_text": true})
		} else {
			m.publishLocked(s.ID, DiscussionEvent{"type": "text_delta", "text": msg.Content, "source": "voice"}, DiscussionEvent{"type": "turn_done", "source": "voice"})
		}
	}
	m.publishLocked(s.ID, DiscussionEvent{"caught_up": true, "session": cloneRuntimeSession(s)})
}
