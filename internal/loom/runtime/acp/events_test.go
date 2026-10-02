package acp

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestToolUpdatesMergeDetachedSnapshotsAndExactDiffs(t *testing.T) {
	tools := map[string]map[string]any{}
	first := UpdateEvent(map[string]any{"sessionUpdate": "tool_call", "toolCallId": "t", "title": "Write", "kind": "edit", "rawInput": map[string]any{"path": "file"}, "locations": []any{map[string]any{"path": "file", "line": 3, "private": "ignored"}}}, tools)
	b, _ := json.Marshal(first)
	want := `{"tool":{"diffs":[],"id":"t","input":"{\"path\":\"file\"}","kind":"edit","locations":[{"line":3,"path":"file"}],"output":"","status":"pending","title":"Write"},"type":"tool_start"}`
	if string(b) != want {
		t.Fatalf("%s", b)
	}
	end := UpdateEvent(map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "t", "status": "completed", "content": []any{map[string]any{"type": "diff", "path": "file", "oldText": nil, "newText": "new"}, map[string]any{"type": "content", "content": map[string]any{"type": "text", "text": "done"}}}}, tools)
	tool := end["tool"].(map[string]any)
	if end["type"] != "tool_end" || tool["title"] != "Write" || tool["output"] != "done" {
		t.Fatal(end)
	}
	if !reflect.DeepEqual(tool["diffs"], []any{map[string]any{"path": "file", "old": "", "new": "new"}}) {
		t.Fatal(tool)
	}
	tool["title"] = "caller mutation"
	if tools["t"]["title"] != "Write" || first["tool"].(map[string]any)["status"] != "pending" {
		t.Fatal("aliased snapshots")
	}
	// A new call reusing an ID drops the old tool's title, output and diffs.
	reset := UpdateEvent(map[string]any{"sessionUpdate": "tool_call", "toolCallId": "t"}, tools)["tool"].(map[string]any)
	if reset["title"] != "" || reset["output"] != "" || len(reset["diffs"].([]any)) != 0 {
		t.Fatal(reset)
	}
}

func TestUpdateEventWireShapesAndMissingValues(t *testing.T) {
	tests := []struct{ raw, want string }{
		{`{"sessionUpdate":"agent_thought_chunk","content":{"type":"text","text":"thought"}}`, `{"text":"thought","type":"reasoning_delta"}`},
		{`{"sessionUpdate":"plan"}`, `{"entries":[],"type":"plan"}`},
		{`{"sessionUpdate":"usage_update","used":0}`, `{"context":{"size":null,"used":0},"cost":null,"type":"usage"}`},
		{`{"sessionUpdate":"current_mode_update"}`, `{"current":"","type":"mode"}`},
		{`{"sessionUpdate":"config_option_update"}`, `{"options":null,"type":"config"}`},
		{`{"sessionUpdate":"available_commands_update","availableCommands":[{"name":"review","description":"Review","input":{"hint":"ignored"}}]}`, `{"commands":[{"description":"Review","name":"review"}],"type":"commands"}`},
	}
	for _, tt := range tests {
		var u map[string]any
		_ = json.Unmarshal([]byte(tt.raw), &u)
		b, err := json.Marshal(UpdateEvent(u, map[string]map[string]any{}))
		if err != nil || string(b) != tt.want {
			t.Fatalf("%s => %s (%v)", tt.raw, b, err)
		}
	}
	if UpdateEvent(map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "image"}}, nil) != nil || UpdateEvent(map[string]any{"sessionUpdate": "unknown"}, nil) != nil {
		t.Fatal("unexpected event")
	}
	p, err := ParseUpdate(json.RawMessage(`{"sessionId":"native","update":{"sessionUpdate":"plan"}}`))
	if err != nil || p.SessionID != "native" || p.Update["sessionUpdate"] != "plan" {
		t.Fatal(p, err)
	}
	if _, err := ParseUpdate(json.RawMessage(`{"sessionId":1}`)); err == nil {
		t.Fatal("malformed session ID accepted")
	}
	clipped := Clip(strings.Repeat("é", 100), 64)
	if len(clipped) > 64 || !utf8.ValidString(clipped) {
		t.Fatal(clipped)
	}
}
