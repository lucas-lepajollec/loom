package loom

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestAntigravityCompatibilityJSONAndEventOrder(t *testing.T) {
	body := `{"event":"step_update","step_update":{"step_index":2,"step_type":"tool","state":"DONE","tool_info":{"name":"write_to_file","parameters":{"TargetFile":"fixture.txt","CodeContent":"private"},"error":{"message":"private"}}}}
{"event":"result","result":{"status":"SUCCESS","response":"OK","conversation_id":"native","duration_seconds":2.5,"usage":{"input_tokens":0,"output_tokens":3,"total_tokens":3,"thinking_tokens":2,"cache_read_tokens":1}}}
`
	var order []string
	answer, err := consumeAgyStream(context.Background(), strings.NewReader(body), func(e StreamEvent) bool {
		var value any
		var want string
		switch {
		case e.HarnessEvent != nil:
			order = append(order, "tool")
			value, want = e.HarnessEvent, `{"index":2,"name":"write_to_file","state":"DONE","failed":true,"file_target":"fixture.txt"}`
		case e.Usage != nil:
			order = append(order, "usage")
			value, want = e.Usage, `{"prompt_tokens":0,"completion_tokens":3,"total_tokens":3,"thinking_tokens":2,"cache_read_tokens":1}`
			if e.Usage.inputReported || e.Usage.outputReported {
				t.Fatal("native usage acquired cloud presence flags")
			}
		case e.DurationSeconds != 0:
			order = append(order, "duration")
			if e.DurationSeconds != 2.5 {
				t.Fatal(e.DurationSeconds)
			}
		case e.NativeSessionID != "":
			order = append(order, "session")
			if e.NativeSessionID != "native" {
				t.Fatal(e.NativeSessionID)
			}
		case e.Content != "":
			order = append(order, "text")
			if e.Content != "OK" {
				t.Fatal(e.Content)
			}
		default:
			t.Fatalf("unexpected event: %+v", e)
		}
		if e.Reasoning != "" || e.Stats != nil || e.ToolUsed != nil {
			t.Fatal("native metadata gained local inference fields")
		}
		if value != nil {
			got, err := json.Marshal(value)
			if err != nil || string(got) != want {
				t.Fatalf("JSON=%s err=%v want=%s", got, err, want)
			}
		}
		return true
	})
	if err != nil || answer != "OK" || strings.Join(order, ",") != "tool,usage,duration,session,text" {
		t.Fatalf("answer=%q err=%v order=%v", answer, err, order)
	}
}

func TestAntigravityPersistedConnectionAndQuotaJSON(t *testing.T) {
	for _, tc := range []struct {
		value any
		want  string
	}{
		{antigravityConnection{Models: []string{"native"}}, `{"models":["native"]}`},
		{QuotaSnapshot{RuntimeID: "antigravity", Name: "Antigravity", Source: "agy /usage · compte natif", Windows: []QuotaWindow{}}, `{"runtime_id":"antigravity","name":"Antigravity","source":"agy /usage · compte natif","fetched_at":0,"windows":[],"reset_credits":null,"credits":null}`},
	} {
		got, err := json.Marshal(tc.value)
		if err != nil || string(got) != tc.want {
			t.Fatalf("JSON=%s err=%v want=%s", got, err, tc.want)
		}
	}
}
