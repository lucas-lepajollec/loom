package loom

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// Exercise the historical adapter, message JSON and shared usage flags without
// sockets, so the migration boundary remains covered in restricted sandboxes.
func TestOpenAICompatibilityPayloadAndUsage(t *testing.T) {
	testHome(t)
	client := &http.Client{Transport: benchRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		want := `{"messages":[{"role":"system","content":"Context"},{"role":"user","content":"Hi"}],"model":"fixture","stream":true,"stream_options":{"include_usage":true}}`
		if string(body) != want {
			t.Fatalf("request JSON changed: %s", body)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(sseChunk("Hello") + "data: {\"usage\":{\"completion_tokens\":0,\"thinking_tokens\":2,\"cache_read_tokens\":3}}\n\ndata: [DONE]\n\n"))}, nil
	})}
	a := cloudRuntimeAdapter{provider: CloudProvider{Endpoint: "https://provider.example/v1", Model: "fixture"}, key: "fixture-key", client: client}
	var usage *RuntimeUsage
	answer, err := a.Run(context.Background(), RuntimeTurn{Messages: []Message{{Role: "system", Content: "Context"}, {Role: "user", Content: "Hi"}}}, func(e StreamEvent) bool {
		if e.Usage != nil {
			usage = e.Usage
		}
		return true
	})
	if err != nil || len(answer) != 1 || answer[0].Role != "assistant" || answer[0].Content != "Hello" {
		t.Fatalf("answer=%+v err=%v", answer, err)
	}
	if usage == nil || usage.inputReported || !usage.outputReported || usage.Thinking != 2 || usage.Cached != 3 {
		t.Fatalf("usage=%+v", usage)
	}
	body, err := json.Marshal(usage)
	if err != nil || string(body) != `{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0,"thinking_tokens":2,"cache_read_tokens":3}` {
		t.Fatalf("usage JSON changed: %s (%v)", body, err)
	}
	if err := json.Unmarshal([]byte(`{"prompt_tokens":0}`), usage); err != nil || !usage.inputReported || usage.outputReported || usage.Thinking != 0 || usage.Cached != 0 {
		t.Fatalf("usage presence did not reset: %+v (%v)", usage, err)
	}
}
