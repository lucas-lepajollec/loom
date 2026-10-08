package discussion

import (
	"reflect"
	"testing"
)

func TestNativeDiscussionEventMapping(t *testing.T) {
	rt := RuntimeTurnRecord[testUsage, testStats]{RuntimeID: "llama.cpp", Model: "local"}
	cases := []struct {
		delta map[string]any
		kind  string
		field string
		value any
	}{
		{map[string]any{"user": "question", "files": []string{"a"}}, "turn_start", "text", "question"},
		{map[string]any{"content": "<script>"}, "text_delta", "text", "<script>"},
		{map[string]any{"reasoning_content": "trace"}, "reasoning_delta", "text", "trace"},
		{map[string]any{"drop_reasoning": true}, "reasoning_delta", "drop", true},
		{map[string]any{"tool_used": map[string]any{"name": "read"}}, "tool_start", "", nil},
		{map[string]any{"tool_used": map[string]any{"typing": true}}, "tool_delta", "", nil},
		{map[string]any{"tool_used": map[string]any{"done": true}}, "tool_end", "", nil},
		{map[string]any{"stats": &testStats{GenTokens: 12}}, "usage", "", nil},
		{map[string]any{"error": "failure"}, "error", "error", "failure"},
		{map[string]any{"turn_done": true, "runtime_turn": rt}, "turn_done", "provenance", rt},
	}
	for _, tc := range cases {
		tc.delta["seq"], tc.delta["replace"], tc.delta["portable_text"], tc.delta["toks"] = 17, true, true, 12
		got := NativeDiscussionEvents(tc.delta)
		if len(got) != 1 || got[0]["type"] != tc.kind {
			t.Fatalf("mapping %v = %v", tc.delta, got)
		}
		if tc.field != "" && !reflect.DeepEqual(got[0][tc.field], tc.value) {
			t.Fatalf("lost payload: %v", got)
		}
		if got[0]["seq"] != 17 || got[0]["replace"] != true || got[0]["portable_text"] != true || got[0]["toks"] != 12 {
			t.Fatalf("lost replay metadata: %v", got)
		}
	}
	control := map[string]any{"reset": true, "replay": true, "ctx_used": 40, "compacted": true}
	if got := NativeDiscussionEvents(control); len(got) != 2 || got[0]["type"] != "compacted" || !reflect.DeepEqual(map[string]any(got[1]), control) {
		t.Fatalf("control: %v", got)
	}
}

func TestRuntimeDiscussionEventMappingIsHonest(t *testing.T) {
	rt := RuntimeTurnRecord[testUsage, testStats]{RuntimeID: "antigravity", Model: "native"}
	if got := RuntimeDiscussionEvents(StreamEvent[testUsage, testStats]{Reasoning: "unreported"}, rt); len(got) != 0 {
		t.Fatalf("invented events: %v", got)
	}
	got := RuntimeDiscussionEvents(StreamEvent[testUsage, testStats]{Content: "answer"}, rt)
	if len(got) != 1 || got[0]["text"] != "answer" || got[0]["toks"] != nil || got[0]["usage"] != nil {
		t.Fatalf("invented token count: %v", got)
	}
	rt.RuntimeID = "codex"
	got = RuntimeDiscussionEvents(StreamEvent[testUsage, testStats]{Reasoning: "reported summary"}, rt)
	if got[0]["type"] != "reasoning_delta" || got[0]["summary"] != true {
		t.Fatal(got)
	}
	for state, kind := range map[string]string{"ACTIVE": "tool_start", "DONE": "tool_end", "UNKNOWN": "tool_delta"} {
		h := HarnessEvent{Index: 1, Name: "write_to_file", State: state, Failed: true, FileTarget: "target"}
		got = RuntimeDiscussionEvents(StreamEvent[testUsage, testStats]{HarnessEvent: &h}, rt)
		if got[0]["type"] != kind || got[0]["native_tool"] != h || got[0]["tool"] != nil {
			t.Fatalf("tool metadata: %v", got)
		}
	}
}

func TestReplayKeepsUnknownMetricsAndPrivateMetadataOutOfText(t *testing.T) {
	s := RuntimeSession[testUsage, testStats]{Status: "complete", Messages: []Message{{Role: "user", Content: "q"}, {Role: "assistant", Content: "a"}, {Role: "assistant", Content: "unknown"}}, Turns: []RuntimeTurnRecord[testUsage, testStats]{{MessageIndex: 1, ReasoningSummary: "summary", DurationSeconds: 2}}}
	calls := 0
	events := RuntimeReplay(s, func() any { calls++; return "context" })
	if calls != 2 || events[0]["reset"] != true || events[len(events)-1]["caught_up"] != true {
		t.Fatal("replay boundaries changed")
	}
	if events[2]["text"] != "summary" || events[2]["summary"] != true || events[3]["text"] != "a" {
		t.Fatal("reasoning display lost")
	}
	if metrics := events[6]["metrics"].(DiscussionEvent); len(metrics) != 0 {
		t.Fatal("unknown historical metrics invented")
	}
	s.Status = "running"
	events = RuntimeReplay(s, func() any { return nil })
	if events[len(events)-2]["running"] != true || events[len(events)-2]["type"] != nil {
		t.Fatal("running assistant completed on replay")
	}
}
