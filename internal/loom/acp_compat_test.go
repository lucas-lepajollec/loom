package loom

import (
	"encoding/json"
	"testing"

	"github.com/lucas-lepajollec/loom/internal/loom/runtime/acp"
)

func TestACPReplayCompatibilityUsesOriginalDiscussionTypes(t *testing.T) {
	b := newReplayBuilder()
	feed := b.Notify("test-acp", "Fixture", "native-1")
	for _, raw := range []string{
		`{"update":{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"question"}}}`,
		`{"update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"answer"}}}`,
	} {
		feed(acpFrame{Params: json.RawMessage(raw)})
	}
	messages, turns, commands := b.Finish("test-acp", "Fixture", "native-1")
	got, err := json.Marshal(messages)
	want := []Message{{Role: "user", Content: "question"}, {Role: "assistant", Content: "answer"}}
	expected, _ := json.Marshal(want)
	if err != nil || string(got) != string(expected) {
		t.Fatalf("messages %s %v", got, err)
	}
	expectedTurn := RuntimeTurnRecord{MessageIndex: 1, RuntimeID: "test-acp", ProviderName: "Fixture", Model: "default", NativeSessionID: "native-1", ACPEvents: []DiscussionEvent{{"type": "text_delta", "text": "answer"}}}
	got, err = json.Marshal(turns)
	expected, _ = json.Marshal([]RuntimeTurnRecord{expectedTurn})
	if err != nil || string(got) != string(expected) || commands != nil {
		t.Fatalf("turns %s %v commands %v", got, err, commands)
	}
	var response acpSessionResponse
	raw := []byte(`{"sessionId":"s","modes":null,"configOptions":[],"models":null}`)
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}
	got, err = json.Marshal(response)
	expected, _ = json.Marshal(acp.SessionResponse(response))
	if err != nil || string(got) != string(expected) || string(got) != string(raw) {
		t.Fatalf("response %s %v", got, err)
	}
}
