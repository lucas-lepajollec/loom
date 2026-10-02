package loom

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestDiscussionCompatibilityPreservesUsageFlagsAndStreamMetadata(t *testing.T) {
	var u RuntimeUsage
	if err := json.Unmarshal([]byte(`{"prompt_tokens":0}`), &u); err != nil {
		t.Fatal(err)
	}
	s := RuntimeSession{Usage: &u, Turns: []RuntimeTurnRecord{{Usage: &u}}}
	clone := cloneRuntimeSession(s)
	if !clone.Usage.inputReported || clone.Usage.outputReported || !clone.Turns[0].Usage.inputReported {
		t.Fatal("usage flags changed at alias boundary")
	}
	if got := runtimeDiscussionEvents(StreamEvent{Reasoning: "unreported", NativeSessionID: "private", DurationSeconds: 2}, RuntimeTurnRecord{RuntimeID: "antigravity"}); len(got) != 0 {
		t.Fatalf("invented events: %v", got)
	}
}

func TestDiscussionCompatibilityJSONShapes(t *testing.T) {
	// Frozen persisted wire record: all moved model fields, including the embedded
	// display-only ACP state. Round-trip rejects lost/renamed fields and extra keys.
	const wire = `{"mcp_servers":[],"native_session_id":"native","native_runtime_id":"h","native_context":"private","workdir":"work","additional_dirs":["extra"],"permission":"ask","mode":"code","config_options":{"k":"v"},"available_modes":[{"id":"code"}],"available_config_options":[{"id":"model"}],"file_baselines":{"f":"base"},"changed_files":[{"path":"f","op":"edit","add":1,"del":2,"at":3}],"harness_usage":{"cost":{"amount":0,"currency":"USD"}},"commands":[{"name":"help"}],"agent_capabilities":{"load":true},"id":"s","project_id":"p","runtime_id":"h","provider_id":"provider","provider_name":"P","endpoint":"endpoint","model":"m","reasoning_effort":"high","title":"t","created_at":1,"updated_at":2,"message_count":1,"status":"complete","messages":[{"role":"assistant","content":"answer","tool_calls":[{"id":"call","type":"function","function":{"name":"read","arguments":"{}"}}],"tool_call_id":"tool"}],"usage":{"prompt_tokens":0,"completion_tokens":2,"total_tokens":2,"thinking_tokens":1,"cache_read_tokens":1},"error":"error","instructions":"instructions","custom_title":true,"last_request_id":"req","request_ids":["req"],"turns":[{"acp_events":[{"type":"text_delta","text":"answer"}],"message_index":1,"runtime_id":"h","provider_name":"P","provider_id":"provider","endpoint":"endpoint","model":"m","context_bytes":8,"context_revision":"rev","input_bytes":4,"usage":{"prompt_tokens":0,"completion_tokens":2,"total_tokens":2},"native_session_id":"native","events":[{"index":1,"name":"read","state":"DONE"}],"duration_seconds":2,"started_at":1,"stats":{"gen_tokens":2},"reasoning_summary":"display","reasoning_effort":"high"}],"source_archive":"source","native_archive":"archive"}`
	var s RuntimeSession
	if err := json.Unmarshal([]byte(wire), &s); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var before, after any
	_ = json.Unmarshal([]byte(wire), &before)
	_ = json.Unmarshal(data, &after)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("wire shape changed: %s", data)
	}
}
