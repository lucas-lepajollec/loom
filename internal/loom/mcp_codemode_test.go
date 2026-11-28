package loom

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/lucas-lepajollec/loom/internal/loom/policy"
	agent "github.com/lucas-lepajollec/loom/internal/loom/runtime"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestCodeModeDataFlow(t *testing.T) {
	raw := `{"steps":[
		{"id":"states","op":"call","server":"home","tool":"states"},
		{"id":"on","op":"filter","from":"states","where":{"field":"state","op":"eq","value":"on"}},
		{"id":"names","op":"map","from":"on","fields":["id","attributes.name"]},
		{"id":"labels","op":"foreach","from":"names","call":{"server":"home","tool":"label","args":{"label":"Lamp {{item.attributes.name}}","id":"{{item.id}}"}}},
		{"id":"first","op":"pick","from":"labels.0","fields":["label"]},
		{"op":"return","fields":["first","labels.0.id"]}]} `
	calls := 0
	res, err := executePipeline(t.Context(), json.RawMessage(raw), func(_ context.Context, target mcpTarget, args json.RawMessage) (*mcp.CallToolResult, error) {
		calls++
		if target.Tool == "states" {
			return textResult([]byte(`[{"state":"on","attributes":{"name":"Kitchen","extra":"private intermediate"},"id":9007199254740993},{"state":"off"}]`), false), nil
		}
		if string(args) != `{"id":9007199254740993,"label":"Lamp Kitchen"}` {
			t.Fatalf("interpolation: %s", args)
		}
		return textResult(args, false), nil
	})
	if err != nil || calls != 2 {
		t.Fatal(err, calls)
	}
	if got := res.Content[0].(*mcp.TextContent).Text; got != `{"first":{"label":"Lamp Kitchen"},"labels":{"0":{"id":9007199254740993}}}` {
		t.Fatal(got)
	}
	for _, tc := range []struct {
		op string
		v  any
	}{{"neq", "off"}, {"contains", "n"}, {"exists", nil}, {"gt", json.Number("9007199254740992")}, {"lt", json.Number("9007199254740994")}} {
		v := any("on")
		if tc.op == "gt" || tc.op == "lt" {
			v = json.Number("9007199254740993")
		}
		if !matchesPredicate(map[string]any{"field": v}, predicate{"field", tc.op, tc.v}) {
			t.Fatal(tc)
		}
	}
}

func TestCodeModeParallelOrderAndBound(t *testing.T) {
	calls := make([]pipelineCall, 17)
	for i := range calls {
		calls[i] = pipelineCall{"s", "echo", map[string]any{"i": i}}
	}
	raw, _ := json.Marshal(map[string]any{"steps": []pipelineStep{{ID: "p", Op: "parallel", Calls: calls}}})
	started, release := make(chan struct{}, 17), make(chan struct{})
	done := make(chan error, 1)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	go func() {
		r, err := executePipeline(ctx, raw, func(ctx context.Context, _ mcpTarget, a json.RawMessage) (*mcp.CallToolResult, error) {
			started <- struct{}{}
			select {
			case <-release:
				return textResult(a, false), nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		})
		if err == nil {
			var rows []struct{ I int }
			err = json.Unmarshal([]byte(r.Content[0].(*mcp.TextContent).Text), &rows)
			if len(rows) != 17 {
				err = fmt.Errorf("unexpected row count %d", len(rows))
			}
			for i, row := range rows {
				if i != row.I {
					err = fmt.Errorf("out of order at %d", i)
					break
				}
			}
		}
		done <- err
	}()
	for range 8 {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	select {
	case <-started:
		t.Fatal("more than eight concurrent calls")
	default:
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestCodeModeLimits(t *testing.T) {
	hits := 0
	run := func(steps []pipelineStep, payload string) (*mcp.CallToolResult, error) {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{"steps": steps})
		return executePipeline(t.Context(), raw, func(context.Context, mcpTarget, json.RawMessage) (*mcp.CallToolResult, error) {
			hits++
			return textResult([]byte(payload), false), nil
		})
	}
	call := pipelineStep{ID: "x", Op: "call", pipelineCall: pipelineCall{Server: "s", Tool: "t"}}
	for _, steps := range [][]pipelineStep{
		{call, call},
		slices.Repeat([]pipelineStep{call}, 65),
		{{ID: "p", Op: "parallel", Calls: slices.Repeat([]pipelineCall{call.pipelineCall}, 31)}},
		{{Op: "return", From: "later"}, call},
	} {
		if _, err := run(steps, "true"); err == nil || hits != 0 {
			t.Fatal(err, hits)
		}
	}
	rows := "[" + strings.Repeat("0,", 25) + "0]"
	loop := pipelineStep{ID: "y", Op: "foreach", From: "x", Call: &call.pipelineCall}
	if _, err := run([]pipelineStep{call, loop}, rows); err == nil || hits != 1 {
		t.Fatal(err, hits)
	}
	loop.Limit = 25
	more := loop
	more.ID = "z"
	more.Limit = 5
	if _, err := run([]pipelineStep{call, loop, more}, rows); err == nil || hits != 27 {
		t.Fatal(err, hits)
	}
	if _, err := run([]pipelineStep{call}, strings.Repeat("x", intermediateLimit+1)); err == nil {
		t.Fatal("oversized result")
	}
	next := call
	next.ID = "again"
	if _, err := run([]pipelineStep{call, next}, strings.Repeat("x", intermediateLimit/2)); err == nil {
		t.Fatal("aggregate budget")
	}
	res, err := run([]pipelineStep{call}, strings.Repeat("é", finalLimit))
	if err != nil {
		t.Fatal(err)
	}
	raw := res.Content[0].(*mcp.TextContent).Text
	var trimmed struct {
		Trimmed bool
		Total   int `json:"total_bytes"`
		Preview string
	}
	if len(raw) > finalLimit || !utf8.ValidString(raw) || json.Unmarshal([]byte(raw), &trimmed) != nil || !trimmed.Trimmed || trimmed.Total != finalLimit*2+2 || trimmed.Preview == "" {
		t.Fatal("invalid final envelope")
	}
}

func TestCodeModePolicyAtEveryCall(t *testing.T) {
	sessions := isolatePhase4b(t)
	var calls atomic.Int32
	onDemandFixture(t, &calls)
	s := connectOnDemandGateway(t)
	const subject = "mcp.tool:home/light_level"
	savePolicyRules(t, policy.Rule{ID: "confirm", Scope: "global", Subject: subject, Decision: policy.Confirm})
	done := make(chan bool, 1)
	go func() {
		res, err := callMCPFixture(t.Context(), s, mcpExecuteTool, `{"steps":[{"op":"call","id":"first","server":"direct","tool":"echo"},{"op":"parallel","id":"batch","calls":[{"server":"home","tool":"light_level","args":{"room_id":"kitchen"}}]}]}`)
		done <- err == nil && !res.IsError
	}()
	p := waitMCPApproval(t, sessions, subject)
	if calls.Load() != 1 || p.Detail != `{"room_id":"kitchen"}` {
		t.Fatal(calls.Load(), p.Detail)
	}
	if err := sessions.answerRequest(p.SessionID, p.RequestID, agent.RequestAnswer{Decision: "deny"}); err != nil {
		t.Fatal(err)
	}
	if <-done || calls.Load() != 1 {
		t.Fatal("unapproved call executed")
	}
	for _, name := range []string{"hidden", "light_level"} {
		if name == "light_level" {
			savePolicyRules(t, policy.Rule{ID: "deny", Scope: "global", Subject: subject, Decision: policy.Deny})
		}
		raw := json.RawMessage(fmt.Sprintf(`{"steps":[{"op":"call","id":"x","server":"home","tool":%q}]}`, name))
		if out := discussionMCPResult(t.Context(), mcpExecuteTool, raw); !strings.Contains(out, "error") {
			t.Fatal(out)
		}
		out, err := (cloudTools{mcp: true}).ExecuteJSON(t.Context(), mcpExecuteTool, raw)
		if err != nil || !strings.Contains(out, "error") {
			t.Fatal(out, err)
		}
	}
	if calls.Load() != 1 {
		t.Fatal(calls.Load())
	}
}
