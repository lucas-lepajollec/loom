package runtime

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

func TestAnswerReplacementScopes(t *testing.T) {
	events := []AgentEvent{
		{Type: "content.delta", Stream: "assistant_text", ItemID: "message:1:0", Delta: "old"},
		{Type: "content.delta", Stream: "reasoning_text", ItemID: "message:1:1", Delta: "private"},
		{Type: "content.delta", Stream: "assistant_text", ItemID: "other", Delta: "!"},
		{Type: "content.delta", Stream: "assistant_text", ItemID: "message:1:2", Delta: " draft"},
		{Type: "content.delta", Stream: "assistant_text", ItemID: "message:1", Delta: "correct", Replace: true},
		{Type: "content.delta", Stream: "assistant_text", ItemID: "other", Delta: "?", Replace: true},
		{Type: "content.delta", Stream: "assistant_text", ItemID: "message:1", Replace: true},
	}
	for _, tc := range []struct {
		name   string
		policy AnswerReplacement
		want   []string
	}{
		{"Codex and Pi", ReplaceItemAndChildren, []string{"old", "old!", "old! draft", "!correct", "?correct", "?"}},
		{"OpenCode", ReplaceItem, []string{"old", "old!", "old! draft", "old! draftcorrect", "old? draftcorrect", "old? draft"}},
		{"Antigravity", ReplaceAnswer, []string{"old", "old!", "old! draft", "correct", "?", ""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := NewAnswerAccumulator(tc.policy)
			snapshots := []*string{}
			for _, e := range events {
				if snapshot := a.Apply(e); snapshot != nil {
					snapshots = append(snapshots, snapshot)
				}
			}
			got := []string{}
			for _, snapshot := range snapshots {
				got = append(got, *snapshot)
			}
			if !reflect.DeepEqual(got, tc.want) || a.Text() != tc.want[len(tc.want)-1] {
				t.Fatalf("snapshots=%q answer=%q", got, a.Text())
			}
			if a.Apply(AgentEvent{Type: "turn.completed"}) != nil {
				t.Fatal("non-text event published an answer snapshot")
			}
		})
	}
}

func TestCloseItemsPreservesTerminalAndDisplayRows(t *testing.T) {
	rows := map[string]map[string]any{
		"z":    {"kind": "execute", "status": "in_progress", "output": "partial"},
		"a":    {"kind": "edit"},
		"s":    {"kind": "search", "status": "running"},
		"t":    {"kind": "other", "status": "running"},
		"done": {"status": "completed"}, "failed": {"status": "failed"}, "interrupted": {"status": "interrupted"},
	}
	before, _ := json.Marshal(rows)
	for _, status := range []string{"completed", "failed", "cancelled", "interrupted"} {
		end := AgentEvent{Type: "turn.completed", Runtime: "fixture", ThreadID: "thread", TurnID: "turn", Status: status, Raw: JSON(map[string]string{"status": status})}
		closed := CloseItems(rows, end)
		if len(closed) != 4 {
			t.Fatal(closed)
		}
		for i, want := range []struct{ id, kind string }{{"a", "file_change"}, {"s", "web_search"}, {"t", "tool_call"}, {"z", "command_execution"}} {
			e := closed[i]
			if e.ItemID != want.id || e.ItemType != want.kind || e.Status != "interrupted" || e.Runtime != end.Runtime || e.ThreadID != end.ThreadID || e.TurnID != end.TurnID || !bytes.Equal(e.Raw, end.Raw) {
				t.Fatal(e)
			}
			if !bytes.Equal(e.Payload, JSON(rows[e.ItemID])) {
				t.Fatal("lost partial output", e)
			}
		}
	}
	after, _ := json.Marshal(rows)
	if !bytes.Equal(before, after) {
		t.Fatal("closing mutated input")
	}
}
