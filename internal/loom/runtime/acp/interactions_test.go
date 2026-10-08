package acp

import (
	"encoding/json"
	"testing"

	agent "github.com/lucas-lepajollec/loom/internal/loom/runtime"
)

func TestInteractionCapabilitiesAndStrictFormDetection(t *testing.T) {
	caps := ClientCapabilities(true, true)
	if caps["terminal"] != false || caps["fs"].(map[string]bool)["writeTextFile"] != true {
		t.Fatal(caps)
	}
	for _, mode := range []string{"form", "url"} {
		if _, ok := caps["elicitation"].(map[string]any)[mode].(map[string]any); !ok {
			t.Fatal(caps)
		}
	}
	if ClientCapabilities(false, false)["elicitation"] != nil {
		t.Fatal("history reader advertised interactive elicitation")
	}
	for _, raw := range []string{`{"mode":"form","toolCallId":"mcp-tool","requestedSchema":{"type":"object","properties":{"name":{"type":"string"}}}}`, `{"mode":"form","requestedSchema":{"type":"object","properties":{"question_0":{"type":"string","oneOf":[{"const":"A","title":"A"}]},"question_0_custom":{"type":"string","title":"Other"}}}}`} {
		if qs := AskUserQuestions(json.RawMessage(raw)); qs != nil {
			t.Fatal("ordinary MCP form misclassified", qs)
		}
	}
}
func TestQuestionAnswerCardinalityAndOptionalSkip(t *testing.T) {
	r := agent.AgentRequest{Kind: "user_input", Questions: []agent.InputQuestion{{ID: "question_0", FreeText: true, Optional: true, Options: []agent.RequestOption{{ID: "A", Label: "A"}, {ID: "B", Label: "B"}}}}}
	for _, values := range [][]string{{}, {"Other"}, {"A"}, {"A", "A note"}} {
		if err := agent.ValidateAnswer(r, agent.RequestAnswer{Answers: map[string][]string{"question_0": values}}); err != nil {
			t.Fatal(values, err)
		}
	}
	for _, values := range [][]string{{"A", "B"}, {"A", "A"}} {
		if err := agent.ValidateAnswer(r, agent.RequestAnswer{Answers: map[string][]string{"question_0": values}}); err == nil {
			t.Fatal("invalid answer accepted", values)
		}
	}
	r.Questions[0].MultiSelect = true
	if err := agent.ValidateAnswer(r, agent.RequestAnswer{Answers: map[string][]string{"question_0": {"A", "B"}}}); err != nil {
		t.Fatal(err)
	}
}
func TestTerminalMetaKeepsOutputAndExitCode(t *testing.T) {
	tools := map[string]map[string]any{}
	Tool(tools, map[string]any{"toolCallId": "bash", "kind": "execute", "_meta": map[string]any{"terminal_info": map[string]any{"terminal_id": "bash"}}})
	got := Tool(tools, map[string]any{"toolCallId": "bash", "_meta": map[string]any{"terminal_output": map[string]any{"terminal_id": "bash", "data": "fixture\n"}}})
	if got["output"] != "fixture\n" {
		t.Fatal(got)
	}
	got = Tool(tools, map[string]any{"toolCallId": "bash", "status": "completed", "_meta": map[string]any{"terminal_exit": map[string]any{"exit_code": float64(2), "signal": nil}}})
	if got["output"] != "fixture\n" || got["exit_code"] != float64(2) {
		t.Fatal(got)
	}
}
