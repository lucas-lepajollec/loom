package acp

import (
	"encoding/json"
	"strings"
	"sync"
)

// ReplayBuilder turns the session/update stream of a session/load into Loom
// messages and turn records (same event vocabulary as a live turn).
type ReplayBuilder[M, T any, E ~map[string]any] struct {
	makeMessage func(role, content string) M
	makeTurn    func(index int, runtimeID, name, native string, events []E) T
	mu          sync.Mutex
	tools       map[string]map[string]any
	messages    []M
	turns       []T
	user        strings.Builder
	answer      strings.Builder
	events      []E
	inAgent     bool
	commands    []map[string]any
}

func (b *ReplayBuilder[M, T, E]) flush(runtimeID, name, native string) {
	if b.user.Len() == 0 && b.answer.Len() == 0 && len(b.events) == 0 {
		return
	}
	user := strings.TrimSpace(b.user.String())
	if user == "" {
		user = "(suite)"
	}
	b.messages = append(b.messages, b.makeMessage("user", user), b.makeMessage("assistant", b.answer.String()))
	b.turns = append(b.turns, b.makeTurn(len(b.messages)-1, runtimeID, name, native, b.events))
	b.user.Reset()
	b.answer.Reset()
	b.events, b.inAgent = nil, false
}

func (b *ReplayBuilder[M, T, E]) Notify(runtimeID, name, native string) func(Frame) {
	return func(f Frame) {
		var params struct {
			Update map[string]any `json:"update"`
		}
		if json.Unmarshal(f.Params, &params) != nil {
			return
		}
		u := params.Update
		b.mu.Lock()
		defer b.mu.Unlock()
		text := func() string {
			c, _ := u["content"].(map[string]any)
			t, _ := c["text"].(string)
			return t
		}
		switch u["sessionUpdate"] {
		case "user_message_chunk":
			if b.inAgent {
				b.flush(runtimeID, name, native)
			}
			b.user.WriteString(text())
		case "agent_message_chunk":
			b.inAgent = true
			t := text()
			b.answer.WriteString(t)
			b.events = append(b.events, E{"type": "text_delta", "text": t})
		case "agent_thought_chunk":
			b.inAgent = true
			b.events = append(b.events, E{"type": "reasoning_delta", "text": text()})
		case "tool_call", "tool_call_update":
			b.inAgent = true
			if u["sessionUpdate"] == "tool_call" {
				id, _ := u["toolCallId"].(string)
				delete(b.tools, id)
			}
			t := Tool(b.tools, u)
			kind := "tool_delta"
			if t["status"] == "completed" || t["status"] == "failed" {
				kind = "tool_end"
			}
			b.events = append(b.events, E{"type": kind, "tool": t})
		case "plan", "plan_update":
			b.events = append(b.events, E{"type": "plan", "entries": u["entries"]})
		case "available_commands_update":
			if list, ok := u["availableCommands"].([]any); ok {
				b.commands = nil
				for _, raw := range list {
					if m, ok := raw.(map[string]any); ok {
						b.commands = append(b.commands, m)
					}
				}
			}
		}
	}
}

// NewReplayBuilder binds application types without importing application state.
func NewReplayBuilder[M, T any, E ~map[string]any](message func(string, string) M, turn func(int, string, string, string, []E) T) *ReplayBuilder[M, T, E] {
	return &ReplayBuilder[M, T, E]{tools: map[string]map[string]any{}, makeMessage: message, makeTurn: turn}
}

// Finish flushes trailing updates under the same lock as Notify.
func (b *ReplayBuilder[M, T, E]) Finish(runtimeID, name, native string) ([]M, []T, []map[string]any) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.flush(runtimeID, name, native)
	return b.messages, b.turns, b.commands
}
