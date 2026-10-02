package acp

import (
	"encoding/json"
	"testing"
)

func TestReplayBuilderRebuildsTurnsFromSessionLoad(t *testing.T) {
	runtimeID, name := "claude-code", "Claude Code"
	b := testReplayBuilder()
	feed := b.Notify(runtimeID, name, "native-1")
	send := func(u map[string]any) {
		raw, _ := json.Marshal(map[string]any{"sessionId": "native-1", "update": u})
		feed(Frame{Params: raw})
	}
	text := func(kind, s string) map[string]any {
		return map[string]any{"sessionUpdate": kind, "content": map[string]any{"type": "text", "text": s}}
	}
	send(text("user_message_chunk", "Corrige "))
	send(text("user_message_chunk", "le bug"))
	send(text("agent_thought_chunk", "je regarde"))
	send(map[string]any{"sessionUpdate": "tool_call", "toolCallId": "t1", "title": "Read main.go", "kind": "read", "status": "completed"})
	send(text("agent_message_chunk", "C'est corrigé."))
	send(text("user_message_chunk", "Merci"))
	send(text("agent_message_chunk", "De rien."))
	send(map[string]any{"sessionUpdate": "available_commands_update", "availableCommands": []any{map[string]any{"name": "review"}}})
	b.Finish(runtimeID, name, "native-1")
	if len(b.messages) != 4 || b.messages[0].Content != "Corrige le bug" || b.messages[1].Content != "C'est corrigé." || b.messages[3].Content != "De rien." {
		t.Fatalf("%+v", b.messages)
	}
	if len(b.turns) != 2 || b.turns[0].MessageIndex != 1 || b.turns[0].NativeSessionID != "native-1" || len(b.turns[0].ACPEvents) != 3 {
		t.Fatalf("%+v", b.turns)
	}
	if b.turns[0].ACPEvents[1]["type"] != "tool_end" || len(b.commands) != 1 {
		t.Fatalf("events %+v commands %+v", b.turns[0].ACPEvents, b.commands)
	}
}

type replayMessage struct{ Role, Content string }
type replayTurn struct {
	MessageIndex                                    int
	RuntimeID, ProviderName, Model, NativeSessionID string
	ACPEvents                                       []map[string]any
}

func testReplayBuilder() *ReplayBuilder[replayMessage, replayTurn, map[string]any] {
	return NewReplayBuilder(
		func(role, content string) replayMessage { return replayMessage{role, content} },
		func(index int, id, name, native string, events []map[string]any) replayTurn {
			return replayTurn{index, id, name, "default", native, events}
		},
	)
}
