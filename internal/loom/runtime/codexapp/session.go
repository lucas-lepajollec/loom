package codexapp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/runtime"
	"github.com/lucas-lepajollec/loom/internal/loom/runtime/agentstdio"
)

type Session struct {
	requestCtx  context.Context
	ActiveModel string
	Client      *agentstdio.Client
	Broker      *runtime.RequestBroker
	Emit        runtime.EventSink[runtime.AgentEvent]
	mu          sync.Mutex
	thread      string
	turn        string
	completion  chan runtime.AgentEvent
}

func New(c *agentstdio.Client, b *runtime.RequestBroker, emit runtime.EventSink[runtime.AgentEvent]) *Session {
	s := &Session{Client: c, Broker: b, Emit: emit, completion: make(chan runtime.AgentEvent, 1)}
	itemText := map[string]string{}
	c.Notify = func(f agentstdio.Frame) {
		if f.Method == "serverRequest/resolved" {
			var p ServerRequestResolvedNotification
			if json.Unmarshal(f.Params, &p) == nil {
				_ = b.Resolve("codex:"+string(p.RequestId), runtime.RequestAnswer{Decision: "cancel"})
			}
			return
		}

		for _, e := range Notification(f) {
			if e.Type == "content.delta" && e.Stream == "assistant_text" {
				itemText[e.ItemID] += e.Delta
			}
			if e.Type == "item.completed" && e.ItemType == "assistant_message" {
				var item struct {
					Text string `json:"text"`
				}
				if json.Unmarshal(e.Payload, &item) == nil && item.Text != itemText[e.ItemID] {
					delta := e
					delta.Type = "content.delta"
					delta.Stream = "assistant_text"
					delta.Delta = item.Text
					delta.Replace = true
					if strings.HasPrefix(item.Text, itemText[e.ItemID]) {
						delta.Delta = strings.TrimPrefix(item.Text, itemText[e.ItemID])
						delta.Replace = false
					}
					if !emit(delta) {
						c.Close()
					}
					itemText[e.ItemID] = item.Text
				}
			}

			s.mu.Lock()
			thread := s.thread
			if e.Type == "turn.started" {
				s.turn = e.TurnID
			}
			s.mu.Unlock()
			if thread != "" && e.ThreadID != "" && e.ThreadID != thread {
				continue
			}
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

	c.Prepare = func(f agentstdio.Frame) func() (any, error) {
		e, err := Request(f)
		if err != nil {
			e.Type = "raw"
			e.Request = nil
			e.Payload = runtime.BoundedJSON(f.Params)
			emit(e)
			emit(runtime.AgentEvent{Type: "error", Runtime: "codex", Method: f.Method, Error: err.Error(), Raw: runtime.BoundedJSON(f.Raw)})
			return func() (any, error) { return nil, err }
		}
		s.mu.Lock()
		requestCtx := s.requestCtx
		s.mu.Unlock()
		if requestCtx == nil {
			requestCtx = context.Background()
		}
		wait, err := b.Open(requestCtx, e)
		if err != nil {
			emit(runtime.AgentEvent{Type: "error", Runtime: "codex", Method: f.Method, Error: err.Error(), Raw: runtime.BoundedJSON(f.Raw)})
			return func() (any, error) { return nil, err }
		}
		return func() (any, error) {
			answer, err := wait(requestCtx)
			if err != nil {
				return nil, err
			}
			return Response(*e.Request, answer), nil
		}
	}

	return s
}
func (s *Session) Initialize(ctx context.Context) error {
	if err := s.Client.Call(ctx, "initialize", InitializeParams{ClientInfo: runtime.JSON(map[string]any{"name": "loom", "title": "Loom", "version": "2"}), Capabilities: runtime.JSON(map[string]any{"experimentalApi": true})}, false, nil); err != nil {
		return err
	}
	return s.Client.Write(map[string]any{"method": "initialized"})
}
func (s *Session) Models(ctx context.Context) ([]json.RawMessage, error) {
	var models []json.RawMessage
	cursor := ""
	for {
		var r ModelListResponse
		if err := s.Client.Call(ctx, "model/list", ModelListParams{Cursor: cursor}, false, &r); err != nil {
			return nil, err
		}
		var data []json.RawMessage
		if err := json.Unmarshal(r.Data, &data); err != nil {
			return nil, err
		}
		models = append(models, data...)
		if r.NextCursor == "" {
			return models, nil
		}
		if r.NextCursor == cursor {
			return nil, errors.New("Codex model cursor did not advance")
		}
		cursor = r.NextCursor
		if len(models) > 4096 {
			return nil, errors.New("Codex model catalog too large")
		}
	}
}

type TurnConfig struct {
	ThreadID, Workdir, Model, Effort, Approval, Sandbox string
	AdditionalDirs                                      []string
}

func (s *Session) Turn(ctx context.Context, c TurnConfig, prompt string, onThread func(string)) error {
	s.mu.Lock()
	s.thread = c.ThreadID
	s.requestCtx = ctx
	s.mu.Unlock()
	params := map[string]any{"cwd": c.Workdir}
	if c.Model != "" && c.Model != "default" {
		params["model"] = c.Model
	}
	if c.Approval != "" {
		params["approvalPolicy"] = c.Approval
	}
	if c.Sandbox != "" {
		params["sandbox"] = c.Sandbox
	}
	if len(c.AdditionalDirs) > 0 {
		if c.ThreadID == "" {
			params["config"] = map[string]any{"sandbox_workspace_write.writable_roots": c.AdditionalDirs}
		}
	}
	method := "thread/start"
	if c.ThreadID != "" {
		method = "thread/resume"
		params["threadId"] = c.ThreadID
	}
	var response ThreadStartResponse
	if err := s.Client.Call(ctx, method, params, false, &response); err != nil {
		return SessionError(err)
	}
	var thread struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(response.Thread, &thread) != nil || thread.ID == "" {
		return errors.New("Codex returned no thread id")
	}
	s.mu.Lock()
	s.thread = thread.ID
	s.mu.Unlock()
	s.ActiveModel = response.Model
	onThread(thread.ID)
	p := TurnStartParams{ThreadId: thread.ID, Input: runtime.JSON([]any{map[string]any{"type": "text", "text": prompt, "text_elements": []any{}}})}
	if c.Sandbox != "" {
		p.SandboxPolicy = response.Sandbox
	}
	if len(c.AdditionalDirs) > 0 {
		var policy map[string]any
		if json.Unmarshal(response.Sandbox, &policy) == nil && policy["type"] == "workspaceWrite" {
			roots, _ := policy["writableRoots"].([]any)
			for _, dir := range c.AdditionalDirs {
				roots = append(roots, dir)
			}
			policy["writableRoots"] = roots
			p.SandboxPolicy = runtime.JSON(policy)
		}
	}

	if c.Effort != "" {
		p.Effort = runtime.JSON(c.Effort)
	}
	if c.Model != "" && c.Model != "default" {
		p.Model = c.Model
	}
	defer func() {
		if ctx.Err() != nil {
			s.Broker.Cancel()
			s.mu.Lock()
			turn := s.turn
			s.mu.Unlock()
			stop, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if turn != "" {
				_ = s.Client.Call(stop, "turn/interrupt", TurnInterruptParams{ThreadId: thread.ID, TurnId: turn}, false, nil)
			}
		}
	}()
	var started TurnStartResponse
	if err := s.Client.Call(ctx, "turn/start", p, false, &started); err != nil {
		return SessionError(err)
	}
	var active struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(started.Turn, &active) == nil && active.ID != "" {
		s.mu.Lock()
		s.turn = active.ID
		s.mu.Unlock()
	}
	select {
	case e := <-s.completion:
		switch e.Status {
		case "completed":
			return nil
		case "interrupted", "cancelled":
			return context.Canceled
		default:
			if e.Error != "" {
				return errors.New(e.Error)
			}
			return errors.New("Codex turn " + e.Status)
		}
	case <-s.Client.Done():
		return s.Client.Err()
	case <-ctx.Done():
		return ctx.Err()
	}
}
