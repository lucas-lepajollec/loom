// Package pirpc speaks the installed Pi CLI's native JSONL RPC interface.
package pirpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/lucas-lepajollec/loom/internal/loom/harness"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/runtime"
	"github.com/lucas-lepajollec/loom/internal/loom/runtime/agentstdio"
)

var TestedVersion = harness.LatestTestedVersion("pi")

type State struct {
	SessionID   string          `json:"sessionId"`
	SessionFile string          `json:"sessionFile"`
	IsStreaming bool            `json:"isStreaming"`
	Model       json.RawMessage `json:"model"`
}
type Session struct {
	Client     *agentstdio.Client
	Broker     *runtime.RequestBroker
	Emit       runtime.EventSink[runtime.AgentEvent]
	mu         sync.Mutex
	mapper     Mapper
	completion chan runtime.AgentEvent
	ctx        context.Context
}

func New(c *agentstdio.Client, b *runtime.RequestBroker, emit runtime.EventSink[runtime.AgentEvent]) *Session {
	s := &Session{Client: c, Broker: b, Emit: emit, completion: make(chan runtime.AgentEvent, 1)}
	c.Notify = func(f agentstdio.Frame) {
		if f.Type == "extension_ui_request" {
			s.dialog(f)
			return
		}
		s.mu.Lock()
		events := s.mapper.Notification(f)
		s.mu.Unlock()
		for _, e := range events {
			if e.Type == "turn.completed" {
				b.Cancel()
			}
			if !emit(e) {
				c.Close()
			}
			if e.Type == "turn.completed" {
				select {
				case s.completion <- e:
				default:
				}
			}
		}
	}
	return s
}

// Pi supplies native session IDs but no turn IDs. The application supplies its
// request identity so canonical turns remain distinct across process resumes.
func (s *Session) SetTurnID(id string) { s.mu.Lock(); s.mapper.RunID = id; s.mu.Unlock() }

func (s *Session) State(ctx context.Context) (State, error) {
	var r State
	err := s.Client.Call(ctx, "get_state", nil, true, &r)
	return r, err
}
func (s *Session) Models(ctx context.Context) ([]json.RawMessage, error) {
	var r struct {
		Models []json.RawMessage `json:"models"`
	}
	err := s.Client.Call(ctx, "get_available_models", nil, true, &r)
	return r.Models, err
}
func (s *Session) Turn(ctx context.Context, sessionPath, provider, model, effort, prompt string, onSession func(State)) error {
	s.mu.Lock()
	s.ctx = ctx
	s.mu.Unlock()
	if sessionPath != "" {
		var r struct {
			Cancelled bool `json:"cancelled"`
		}
		if err := s.Client.Call(ctx, "switch_session", map[string]any{"sessionPath": sessionPath}, true, &r); err != nil {
			return fmt.Errorf("Pi session resume: %w", err)
		}
		if r.Cancelled {
			return errors.New("Pi session resume cancelled")
		}
	}
	state, err := s.State(ctx)
	if err != nil {
		return err
	}
	if state.IsStreaming {
		return errors.New("session open in another Pi window")
	}
	onSession(state)
	s.mu.Lock()
	s.mapper.ThreadID = state.SessionID
	s.mu.Unlock()
	if model != "" && model != "default" {
		if provider == "" {
			return errors.New("Pi model selection requires provider/model")
		}
		if err := s.Client.Call(ctx, "set_model", map[string]any{"provider": provider, "modelId": model}, true, nil); err != nil {
			return err
		}
	}
	if effort != "" {
		if err := s.Client.Call(ctx, "set_thinking_level", map[string]any{"level": effort}, true, nil); err != nil {
			return err
		}
	}
	if model != "" && model != "default" {
		current, err := s.State(ctx)
		if err != nil {
			return err
		}
		onSession(current)
	}

	// A stop may race the prompt acknowledgement. Abort in that case too.
	defer func() {
		if ctx.Err() != nil {
			s.Broker.Cancel()
			stop, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = s.Client.Call(stop, "abort", nil, true, nil)
		}
	}()
	var acknowledgement agentstdio.Frame
	if err := s.Client.Call(ctx, "prompt", map[string]any{"message": prompt}, true, &acknowledgement); err != nil {
		return err
	}
	var accepted struct {
		Disposition string `json:"disposition"`
	}
	_ = json.Unmarshal(acknowledgement.Data, &accepted)
	if accepted.Disposition == "handled" {
		// Pi 1.1.0 distinguishes extension-handled input, which starts no
		// run and will not emit agent_settled. Legacy acknowledgements have
		// no disposition and still settle through the native event stream.
		s.mu.Lock()
		turnID := s.mapper.turnID()
		s.mu.Unlock()
		s.Broker.Cancel()
		if !s.Emit(runtime.AgentEvent{Type: "turn.completed", Runtime: "pi", ThreadID: state.SessionID, TurnID: turnID, Status: "completed", Method: "prompt", Raw: runtime.BoundedJSON(acknowledgement.Raw), Payload: runtime.BoundedJSON(acknowledgement.Data)}) {
			s.Client.Close()
		}
		return nil
	}
	select {
	case e := <-s.completion:
		// Context occupancy is a separate native observation, not the sum of
		// retained spend. Read it after settlement without blocking the reader.
		statsCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		var frame agentstdio.Frame
		statsErr := s.Client.Call(statsCtx, "get_session_stats", nil, true, &frame)
		cancel()
		if statsErr == nil {
			if usage := ContextObservation(frame); usage != nil {
				s.Emit(*usage)
			}
		} else if ctx.Err() == nil {
			s.Emit(runtime.AgentEvent{Type: "warning", Runtime: "pi", Method: "get_session_stats", Message: statsErr.Error(), Raw: runtime.JSON(map[string]any{"error": statsErr.Error()})})
		}
		if e.Status == "failed" {
			return errors.New(e.Error)
		}
		if e.Status == "interrupted" {
			return context.Canceled
		}
		return nil
	case <-s.Client.Done():
		return s.Client.Err()
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (s *Session) dialog(f agentstdio.Frame) {
	e, expects, err := Request(f)
	if err != nil {
		s.Emit(e)
		s.Emit(runtime.AgentEvent{Type: "error", Runtime: "pi", Method: e.Method, Error: err.Error(), Raw: runtime.BoundedJSON(f.Raw)})
		_ = s.Client.Write(map[string]any{"type": "extension_ui_response", "id": f.ID, "cancelled": true, "error": err.Error()})
		return
	}
	if !expects {
		s.Emit(e)
		return
	}
	s.mu.Lock()
	ctx := s.ctx
	e.ThreadID = s.mapper.ThreadID
	e.TurnID = s.mapper.turnID()
	s.mu.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}
	var params struct {
		Timeout int `json:"timeout"`
	}
	_ = json.Unmarshal(f.Raw, &params)
	cancel := func() {}
	if params.Timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, time.Duration(params.Timeout)*time.Millisecond)
	}
	// Open in wire order, then keep the reader free while awaiting the user.
	wait, openErr := s.Broker.Open(ctx, e)
	if openErr != nil {
		cancel()
		s.Emit(runtime.AgentEvent{Type: "error", Runtime: "pi", Method: e.Method, Error: openErr.Error(), Raw: runtime.BoundedJSON(f.Raw)})
		_ = s.Client.Write(map[string]any{"type": "extension_ui_response", "id": f.ID, "cancelled": true})
		return
	}
	go func() {
		defer cancel()
		a, err := wait(ctx)
		if err != nil {
			a.Decision = "cancel"
		}
		_ = s.Client.Write(Response(*e.Request, f.ID, a))
	}()
}
func Request(f agentstdio.Frame) (runtime.AgentEvent, bool, error) {
	var p struct {
		ID      string   `json:"id"`
		Method  string   `json:"method"`
		Title   string   `json:"title"`
		Message string   `json:"message"`
		Options []string `json:"options"`
		Timeout int      `json:"timeout"`
	}
	e := runtime.AgentEvent{Type: "raw", Runtime: "pi", Raw: runtime.BoundedJSON(f.Raw), Payload: runtime.BoundedJSON(f.Raw)}
	if err := json.Unmarshal(f.Raw, &p); err != nil {
		return e, false, err
	}
	e.Method = p.Method
	switch p.Method {
	case "notify", "setStatus", "setWidget", "setTitle", "set_editor_text":
		return e, false, nil
	}
	if p.Method != "confirm" && p.Method != "select" && p.Method != "input" && p.Method != "editor" {
		return e, true, fmt.Errorf("unsupported Pi extension request: %s", p.Method)
	}
	r := &runtime.AgentRequest{ID: "pi:" + p.ID, Kind: "user_input", Method: p.Method, Message: p.Message, Payload: e.Payload}
	q := runtime.InputQuestion{ID: "value", Question: p.Title, FreeText: p.Method == "input" || p.Method == "editor"}
	for _, o := range p.Options {
		q.Options = append(q.Options, runtime.RequestOption{ID: o, Label: o})
	}
	if p.Method == "confirm" {
		q.Options = []runtime.RequestOption{{ID: "yes", Label: "Yes"}, {ID: "no", Label: "No"}}
	}
	r.Questions = []runtime.InputQuestion{q}
	e.Type = "request.opened"
	e.Request = r
	return e, true, nil
}
func Response(r runtime.AgentRequest, id json.RawMessage, a runtime.RequestAnswer) any {
	out := map[string]any{"type": "extension_ui_response", "id": id}
	if a.Decision == "cancel" {
		out["cancelled"] = true
		return out
	}
	v := ""
	if answers := a.Answers["value"]; len(answers) > 0 {
		v = answers[0]
	}
	if r.Method == "confirm" {
		out["confirmed"] = v == "yes"
	} else {
		out["value"] = v
	}
	return out
}

type Mapper struct {
	RunID    string
	started  bool
	text     string
	ThreadID string
	turn     int
	message  int
	status   string
	failure  string
	toolIDs  map[int]string
}

func ContextObservation(f agentstdio.Frame) *runtime.AgentEvent {
	var stats struct {
		SessionID string `json:"sessionId"`
		Context   *struct {
			Tokens *int64 `json:"tokens"`
			Window *int64 `json:"contextWindow"`
		} `json:"contextUsage"`
	}
	if json.Unmarshal(f.Data, &stats) != nil || stats.Context == nil {
		return nil
	}
	return &runtime.AgentEvent{Type: "context.updated", Runtime: "pi", ThreadID: stats.SessionID, Method: "get_session_stats", Raw: runtime.BoundedJSON(f.Raw), Payload: runtime.BoundedJSON(f.Data), Usage: &runtime.AgentUsage{Scope: "context", Total: stats.Context.Tokens, ContextWindow: stats.Context.Window}}
}

func (m *Mapper) turnID() string {
	if m.RunID != "" {
		return m.RunID
	}
	return strconv.Itoa(m.turn)
}

func (m *Mapper) Notification(f agentstdio.Frame) []runtime.AgentEvent {
	e := runtime.AgentEvent{Type: "raw", Runtime: "pi", Method: f.Type, ThreadID: m.ThreadID, TurnID: m.turnID(), Raw: runtime.BoundedJSON(f.Raw), Payload: runtime.BoundedJSON(f.Raw)}
	var p struct {
		ToolCallID            string          `json:"toolCallId"`
		ToolName              string          `json:"toolName"`
		IsError               bool            `json:"isError"`
		Message               json.RawMessage `json:"message"`
		Success               bool            `json:"success"`
		Aborted               bool            `json:"aborted"`
		FinalError            string          `json:"finalError"`
		Error                 string          `json:"error"`
		AssistantMessageEvent struct {
			Type         string          `json:"type"`
			Delta        string          `json:"delta"`
			ContentIndex int             `json:"contentIndex"`
			ID           string          `json:"id"`
			ToolName     string          `json:"toolName"`
			ToolCall     json.RawMessage `json:"toolCall"`
		} `json:"assistantMessageEvent"`
	}
	if err := json.Unmarshal(f.Raw, &p); err != nil {
		e.Type = "error"
		e.Error = err.Error()
		return []runtime.AgentEvent{e}
	}
	switch f.Type {
	case "agent_start":
		if m.started {
			return []runtime.AgentEvent{e}
		}
		m.started = true
		m.turn++
		m.status = "completed"
		m.failure = ""
		e.TurnID = m.turnID()
		e.Type = "turn.started"
	case "agent_settled":
		e.Type = "turn.completed"
		e.Status = m.status
		e.Error = m.failure
		// Pi 1.1.0 can report cancellation without an aborted message_end.
		// Older releases omit this field and retain message-based settlement.
		if p.Aborted {
			e.Status = "interrupted"
			e.Error = ""
		}
	case "message_start", "message_end":
		var msg struct {
			Role         string          `json:"role"`
			StopReason   string          `json:"stopReason"`
			ErrorMessage string          `json:"errorMessage"`
			Usage        json.RawMessage `json:"usage"`
			Content      json.RawMessage `json:"content"`
		}
		_ = json.Unmarshal(p.Message, &msg)
		if msg.Role != "assistant" {
			break
		}
		if f.Type == "message_start" {
			m.message++
			m.text = ""
			m.toolIDs = map[int]string{}
			e.Type = "item.started"
		} else {
			e.Type = "item.completed"
		}
		e.ItemID = "message:" + strconv.Itoa(m.message)
		e.ItemType = "assistant_message"
		e.Payload = runtime.BoundedJSON(p.Message)
		if f.Type == "message_end" {
			events := []runtime.AgentEvent{}
			var blocks []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}
			_ = json.Unmarshal(msg.Content, &blocks)
			full := ""
			for _, block := range blocks {
				if block.Type == "text" {
					full += block.Text
				}
			}
			if full != "" && full != m.text {
				delta := e
				delta.Type = "content.delta"
				delta.Stream = "assistant_text"
				delta.Delta = full
				delta.Replace = true
				if strings.HasPrefix(full, m.text) {
					delta.Delta = strings.TrimPrefix(full, m.text)
					delta.Replace = false
				}
				events = append(events, delta)
				m.text = full
			}

			switch msg.StopReason {
			case "error":
				m.status = "failed"
				m.failure = msg.ErrorMessage
				e.Error = msg.ErrorMessage
			case "aborted":
				m.status = "interrupted"
			case "stop", "length", "toolUse":
				m.status = "completed"
				m.failure = ""
			}
			if len(msg.Usage) > 0 {
				u := e
				u.Type = "usage.spent"
				u.Payload = runtime.BoundedJSON(msg.Usage)
				var usage struct {
					Input  *int64 `json:"input"`
					Output *int64 `json:"output"`
					Cached *int64 `json:"cacheRead"`
					Total  *int64 `json:"totalTokens"`
				}
				if json.Unmarshal(msg.Usage, &usage) == nil {
					u.Usage = &runtime.AgentUsage{Scope: "message", Input: usage.Input, Output: usage.Output, Cached: usage.Cached, Total: usage.Total}
				}
				events = append(events, e, u)
				return events
			}
			events = append(events, e)
			return events
		}
	case "message_update":
		a := p.AssistantMessageEvent
		e.ItemID = "message:" + strconv.Itoa(m.message) + ":" + strconv.Itoa(a.ContentIndex)
		switch a.Type {
		case "text_delta", "thinking_delta":
			e.Type = "content.delta"
			e.Stream = "assistant_text"
			if a.Type == "thinking_delta" {
				e.Stream = "reasoning_text"
			}
			e.Delta = a.Delta
			if a.Type == "text_delta" {
				m.text += a.Delta
			}
		case "toolcall_start":
			if m.toolIDs == nil {
				m.toolIDs = map[int]string{}
			}
			m.toolIDs[a.ContentIndex] = a.ID
			e.Type = "item.started"
			e.ItemType = "tool_call"
			e.ItemID = a.ID
			e.Payload = runtime.JSON(map[string]any{"id": a.ID, "toolName": a.ToolName})
		case "toolcall_delta":
			e.Type = "content.delta"
			e.Stream = "tool_arguments"
			e.Delta = a.Delta
			if id := m.toolIDs[a.ContentIndex]; id != "" {
				e.ItemID = id
			}
		case "toolcall_end":
			e.Type = "item.updated"
			e.ItemType = "tool_call"
			e.Payload = runtime.BoundedJSON(a.ToolCall)
			var call struct {
				ID string `json:"id"`
			}
			_ = json.Unmarshal(a.ToolCall, &call)
			e.ItemID = call.ID
		}
	case "tool_execution_start", "tool_execution_update", "tool_execution_end":
		e.Type = "item.started"
		if f.Type == "tool_execution_update" {
			e.Type = "item.updated"
		}
		if f.Type == "tool_execution_end" {
			e.Type = "item.completed"
		}
		e.ItemID = p.ToolCallID
		e.ItemType = "tool_call"
		if p.ToolName == "bash" {
			e.ItemType = "command_execution"
		}
		if p.ToolName == "write" || p.ToolName == "edit" {
			e.ItemType = "file_change"
		}
		if p.IsError {
			e.Status = "failed"
		} else if f.Type == "tool_execution_end" {
			e.Status = "completed"
		}
	case "auto_retry_end":
		if !p.Success {
			m.status = "failed"
			m.failure = p.FinalError
			e.Type = "error"
			e.Error = p.FinalError
		}
	case "extension_error":
		e.Type = "error"
		e.Error = p.Error
	}
	return []runtime.AgentEvent{e}
}
