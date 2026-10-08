package loom

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/lucas-lepajollec/loom/internal/loom/runtime/codexapp"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	agent "github.com/lucas-lepajollec/loom/internal/loom/runtime"
)

type requestFixtureAdapter struct {
	manager  *runtimeSessions
	id       string
	opened   chan bool
	resolved chan agent.RequestAnswer
}

func (a *requestFixtureAdapter) Descriptor() RuntimeDescriptor {
	return RuntimeDescriptor{ID: "fixture-agent", Name: "Fixture", Kind: "harness", Implemented: true, Capabilities: []string{"chat"}}
}
func (a *requestFixtureAdapter) Run(ctx context.Context, turn RuntimeTurn, emit ChatCallback) ([]Message, error) {
	broker := agent.NewRequestBroker("fixture", func(e AgentEvent) bool {
		d := DiscussionEvent{"type": e.Type, "agent_event": e, "request": e.Request, "request_id": e.RequestID, "outcome": e.Outcome}
		ok := emit(StreamEvent{ACPEvent: d, AgentEvent: &e})
		if e.Type == "request.opened" {
			a.opened <- true
		}
		return ok
	})
	a.manager.acpMu.Lock()
	a.manager.requests = map[string]*agent.RequestBroker{a.id: broker}
	a.manager.acpMu.Unlock()
	defer broker.Cancel()
	answer, err := broker.Ask(ctx, AgentEvent{Type: "request.opened", Runtime: "fixture", Raw: agent.JSON(map[string]any{"method": "question"}), Request: &agent.AgentRequest{ID: "pi:fixture", Kind: "user_input", Method: "input", Questions: []agent.InputQuestion{{ID: "value", Question: "A note?", FreeText: true}}}})
	a.resolved <- answer
	return nil, err
}
func TestAgentRequestPersistenceResolutionAndCancellation(t *testing.T) {
	for _, cancelTurn := range []bool{false, true} {
		t.Run(map[bool]string{false: "answer", true: "cancel"}[cancelTurn], func(t *testing.T) {
			testHome(t)
			m := newRuntimeSessions()
			t.Cleanup(m.shutdownACP)
			a := &requestFixtureAdapter{manager: m, opened: make(chan bool, 1), resolved: make(chan agent.RequestAnswer, 1)}
			isolateRuntimeRegistry(t, a)
			s, err := m.create("", "", false)
			if err != nil {
				t.Fatal(err)
			}
			s.RuntimeID = "fixture-agent"
			s.Model = "default"
			a.id = s.ID
			if err := putStoreJSON(bkRuntimeSessions, s.ID, s); err != nil {
				t.Fatal(err)
			}
			if err := m.start(s.ID, "fixture-request", "Ask a question"); err != nil {
				t.Fatal(err)
			}
			select {
			case <-a.opened:
			case <-time.After(3 * time.Second):
				t.Fatal("request never opened")
			}
			stored := RuntimeSession{}
			if !getStoreJSON(bkRuntimeSessions, s.ID, &stored) || len(stored.PendingRequests) != 1 {
				t.Fatalf("pending request was not persisted: %+v", stored)
			}
			// A reload reads the same snapshot, independently of the live registry.
			copy := cloneRuntimeSession(stored)
			copy.PendingRequests[0].Questions[0].Question = "changed"
			if stored.PendingRequests[0].Questions[0].Question == "changed" {
				t.Fatal("request clone aliases original")
			}
			replay := runtimeReplay(stored)
			found := false
			for _, e := range replay {
				if e["type"] == "request.opened" {
					found = true
				}
			}
			if !found {
				t.Fatal("replay lost request")
			}
			if cancelTurn {
				if err := m.stop(s.ID); err != nil {
					t.Fatal(err)
				}
			} else {
				original := workspaceSessions
				workspaceSessions = m
				defer func() { workspaceSessions = original }()
				req := httptest.NewRequest("POST", "/api/workspace/sessions/fixture/requests/pi:fixture", strings.NewReader(`{"answers":{"value":["A fixture answer"]}}`))
				req.Header.Set("Content-Type", "application/json")
				req.SetPathValue("id", s.ID)
				req.SetPathValue("request_id", "pi:fixture")
				w := httptest.NewRecorder()
				handleAgentRequest(w, req)
				if w.Code != 200 {
					t.Fatalf("resolve endpoint: %d %s", w.Code, w.Body.String())
				}
				w = httptest.NewRecorder()
				duplicate := httptest.NewRequest("POST", req.URL.String(), strings.NewReader(`{"answers":{"value":["Again"]}}`))
				duplicate.Header.Set("Content-Type", "application/json")
				duplicate.SetPathValue("id", s.ID)
				duplicate.SetPathValue("request_id", "pi:fixture")
				handleAgentRequest(w, duplicate)
				if w.Code != 409 {
					t.Fatalf("duplicate resolution: %d", w.Code)
				}
			}
			select {
			case answer := <-a.resolved:
				if cancelTurn && answer.Decision != "cancel" {
					t.Fatal(answer)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("resolution hung")
			}
			deadline := time.Now().Add(3 * time.Second)
			for time.Now().Before(deadline) {
				current, _ := m.get(s.ID)
				if current.Status != "running" {
					if len(current.PendingRequests) != 0 {
						t.Fatal("request remains actionable")
					}
					return
				}
				time.Sleep(time.Millisecond)
			}
			t.Fatal("turn remained running")
		})
	}
}
func TestAgentFormValidationAndRawBound(t *testing.T) {
	r := agent.AgentRequest{Kind: "elicitation", Schema: json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}`)}
	if err := agent.ValidateAnswer(r, agent.RequestAnswer{Decision: "accept", Content: json.RawMessage(`{}`)}); err == nil {
		t.Fatal("invalid form accepted")
	}
	if err := agent.ValidateAnswer(r, agent.RequestAnswer{Decision: "accept", Content: json.RawMessage(`{"name":"Fixture"}`)}); err != nil {
		t.Fatal(err)
	}
	raw := agent.BoundedJSON([]byte(strings.Repeat("x", 1<<20)))
	if len(raw) > 64<<10 || !json.Valid(raw) || !strings.Contains(string(raw), "truncated") {
		t.Fatal("raw not bounded")
	}
	if err := codexapp.SessionError(errors.New("thread is busy")); err == nil || !strings.Contains(err.Error(), "session open in another Codex window") {
		t.Fatal("lock error lost")
	}
}

func TestAgentAbandonedRequestsAndIncompleteTools(t *testing.T) {
	testHome(t)
	m := newRuntimeSessions()
	s, err := m.create("", "", false)
	if err != nil {
		t.Fatal(err)
	}
	s.Status = "running"
	s.RuntimeID = "codex"
	s.PendingRequests = []agent.AgentRequest{{ID: "codex:pending", Kind: "user_input"}}
	s.Turns = []RuntimeTurnRecord{{ACPEvents: []DiscussionEvent{{"type": "request.opened", "request": s.PendingRequests[0]}}}}
	if err := putStoreJSON(bkRuntimeSessions, s.ID, s); err != nil {
		t.Fatal(err)
	}
	restored, ok := m.get(s.ID)
	if !ok || restored.Status != "interrupted" || len(restored.PendingRequests) != 0 {
		t.Fatal(restored)
	}
	last := restored.Turns[0].ACPEvents[len(restored.Turns[0].ACPEvents)-1]
	if last["type"] != "request.resolved" || last["outcome"] != "cancelled" {
		t.Fatal(last)
	}
	p := newAgentProjection()
	p.event(AgentEvent{Type: "item.started", ItemID: "tool-1", ItemType: "command_execution", Payload: agent.JSON(map[string]any{"command": "fixture"})})
	closed := p.closeItems(AgentEvent{Type: "turn.completed", Status: "failed", Raw: agent.JSON(map[string]any{"status": "failed"})})
	if len(closed) != 1 || closed[0].Status != "interrupted" {
		t.Fatal(closed)
	}
	row := p.event(closed[0])
	tool := row["tool"].(map[string]any)
	if row["type"] != "tool_end" || tool["status"] != "interrupted" {
		t.Fatal(row)
	}
}

func TestACPElicitationUsesSharedRequestBroker(t *testing.T) {
	for _, mode := range []string{"form", "url"} {
		t.Run(mode, func(t *testing.T) {
			opened := make(chan AgentEvent, 1)
			b := agent.NewRequestBroker("fixture-acp", func(e AgentEvent) bool {
				if e.Type == "request.opened" {
					opened <- e
				}
				return true
			})
			defer b.Cancel()
			p := &acpBinding{broker: b, agentID: "fixture-acp", state: ACPState{NativeSessionID: "fixture-session"}}
			params := map[string]any{"sessionId": "fixture-session", "mode": mode, "message": "Fixture"}
			answer := agent.RequestAnswer{Decision: "accept"}
			if mode == "form" {
				params["requestedSchema"] = map[string]any{"type": "object", "properties": map[string]any{"name": map[string]any{"type": "string"}}, "required": []string{"name"}}
				answer.Content = json.RawMessage(`{"name":"Fixture"}`)
			} else {
				params["url"] = "https://example.com/fixture"
			}
			f := &acpFrame{ID: json.RawMessage(`17`), Method: "session/create_elicitation", Params: agent.JSON(params)}
			f.Raw = agent.JSON(f)
			result := make(chan any, 1)
			go func() {
				r, err := p.elicitation(context.Background(), f)
				if err != nil {
					result <- err
				} else {
					result <- r
				}
			}()
			select {
			case e := <-opened:
				if e.Request.Kind != "elicitation" || e.Request.ID != "acp:17" {
					t.Fatal(e)
				}
				if err := b.Resolve(e.Request.ID, answer); err != nil {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("elicitation did not open")
			}
			select {
			case r := <-result:
				if value, ok := r.(map[string]any); !ok || value["action"] != "accept" {
					t.Fatal(r)
				}
			case <-time.After(time.Second):
				t.Fatal("elicitation did not resolve")
			}
		})
	}
}
