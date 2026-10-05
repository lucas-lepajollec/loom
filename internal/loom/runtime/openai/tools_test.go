package openai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

type searchFixture struct{ calls int }

func (*searchFixture) Definitions() any {
	return []any{map[string]any{"type": "function", "function": map[string]any{"name": "web_search"}}}
}
func (f *searchFixture) Execute(ctx context.Context, name string, args map[string]any) (string, error) {
	if name != "web_search" {
		return "", errors.New("not allowed")
	}
	f.calls++
	if args["query"] != "query" {
		return "", errors.New("bad query")
	}
	return "Fixture search result", ctx.Err()
}
func toolChunk(name, args, finish string) string {
	data, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "call-one", "type": "function", "function": map[string]any{"name": name, "arguments": args}}}}, "finish_reason": finish}}})
	return "data: " + string(data) + "\n\ndata: [DONE]\n\n"
}
func TestCloudWebToolRoundTripAndReportedUsage(t *testing.T) {
	fixture := &searchFixture{}
	round := 0
	a := adapterWithTransport(func(r *http.Request) (*http.Response, error) {
		var request map[string]any
		_ = json.NewDecoder(r.Body).Decode(&request)
		if request["tools"] == nil {
			t.Fatal("tools not advertised")
		}
		round++
		if round == 1 {
			return sseResponse(strings.Replace(toolChunk("web_search", `{"query":"query"}`, "tool_calls"), "data: [DONE]", `data: {"choices":[],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`+"\n\ndata: [DONE]", 1)), nil
		}
		messages := request["messages"].([]any)
		last := messages[len(messages)-1].(map[string]any)
		if last["role"] != "tool" || last["content"] != "Fixture search result" || last["tool_call_id"] != "call-one" {
			t.Fatal("tool result missing")
		}
		return sseResponse(textChunk("Answer", "stop") + `data: {"choices":[],"usage":{"prompt_tokens":7,"completion_tokens":4,"total_tokens":11}}` + "\n\ndata: [DONE]\n\n"), nil
	})
	var total Usage
	events := 0
	answer, err := a.Run(context.Background(), Turn{Messages: []any{map[string]any{"role": "user", "content": "Question"}}, Tools: fixture}, func(e Event) bool {
		if e.Usage != nil {
			total = *e.Usage
		}
		if e.Tool != nil {
			events++
		}
		return true
	})
	if err != nil || answer != "Answer" || fixture.calls != 1 || events != 2 || total.Input != 10 || total.Output != 6 || total.Total != 16 {
		t.Fatalf("roundtrip failed: %s %v %+v", answer, err, total)
	}
}
func TestCloudToolsRejectTruncatedCallsUnadvertisedToolsAndBoundRetries(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		maxCalls   int
	}{{"truncated", toolChunk("web_search", `{"query":"query"}`, "stop"), 0}, {"unadvertised", toolChunk("bash", `{}`, "tool_calls"), 0}, {"malformed", toolChunk("web_search", `{`, "tool_calls"), 0}, {"loop", toolChunk("web_search", `{"query":"query"}`, "tool_calls"), 4}} {
		t.Run(tc.name, func(t *testing.T) {
			f := &searchFixture{}
			a := adapterWithTransport(func(*http.Request) (*http.Response, error) { return sseResponse(tc.body), nil })
			_, err := a.Run(context.Background(), Turn{Tools: f}, func(Event) bool { return true })
			if err == nil || f.calls != tc.maxCalls {
				t.Fatalf("calls=%d err=%v", f.calls, err)
			}
		})
	}
}
