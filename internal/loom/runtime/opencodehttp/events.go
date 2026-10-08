package opencodehttp

import (
	"encoding/json"
	"fmt"
	"strings"

	agent "github.com/lucas-lepajollec/loom/internal/loom/runtime"
)

type Frame struct {
	Type       string          `json:"type"`
	Properties json.RawMessage `json:"properties"`
}
type part struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionID"`
	MessageID string `json:"messageID"`
	Type      string `json:"type"`
	Text      string `json:"text"`
	Tool      string `json:"tool"`
	CallID    string `json:"callID"`
	State     struct {
		Status string `json:"status"`
		Input  any    `json:"input"`
		Output string `json:"output"`
		Error  string `json:"error"`
	} `json:"state"`
}
type Mapper struct {
	SessionID, UserID string
	parts             map[string]part
	text              map[string]string
	roles             map[string]string
	usage             map[string]bool
	tools             map[string]bool
	pending           map[string]bool
	Active            bool
}

func NewMapper(id, user string) *Mapper {
	return &Mapper{SessionID: id, UserID: user, parts: map[string]part{}, text: map[string]string{}, roles: map[string]string{}, usage: map[string]bool{}, tools: map[string]bool{}, pending: map[string]bool{}}
}
func (m *Mapper) Events(raw []byte) ([]agent.AgentEvent, error) {
	var f Frame
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, err
	}
	var p struct {
		ID        string `json:"id"`
		SessionID string `json:"sessionID"`
		Part      part   `json:"part"`
		PartID    string `json:"partID"`
		MessageID string `json:"messageID"`
		Field     string `json:"field"`
		Delta     string `json:"delta"`
		Info      struct {
			ID        string          `json:"id"`
			SessionID string          `json:"sessionID"`
			Role      string          `json:"role"`
			ParentID  string          `json:"parentID"`
			Error     json.RawMessage `json:"error"`
			Time      struct {
				Completed int64 `json:"completed"`
			} `json:"time"`
			Tokens struct {
				Input     *int64 `json:"input"`
				Output    *int64 `json:"output"`
				Reasoning *int64 `json:"reasoning"`
				Total     *int64 `json:"total"`
				Cache     struct {
					Read *int64 `json:"read"`
				} `json:"cache"`
			} `json:"tokens"`
		} `json:"info"`
		Status struct {
			Type string `json:"type"`
		} `json:"status"`
		Error json.RawMessage `json:"error"`
		Todos []struct {
			Content string `json:"content"`
			Status  string `json:"status"`
		} `json:"todos"`
	}
	if err := json.Unmarshal(f.Properties, &p); err != nil {
		return nil, err
	}
	sid := p.SessionID
	if sid == "" {
		sid = p.Part.SessionID
	}
	if sid == "" {
		sid = p.Info.SessionID
	}
	if sid == "" && f.Type == "session.error" {
		sid = m.SessionID
	}
	if sid == "" && f.Type == "server.heartbeat" {
		return nil, nil
	}
	if sid != m.SessionID {
		return nil, nil
	}
	base := agent.AgentEvent{Type: "raw", Runtime: "opencode", ThreadID: sid, Method: f.Type, Raw: agent.BoundedJSON(raw), Payload: agent.BoundedJSON(f.Properties)}
	one := func(e agent.AgentEvent) []agent.AgentEvent { return []agent.AgentEvent{e} }
	switch f.Type {
	case "message.updated":
		m.roles[p.Info.ID] = p.Info.Role
		if p.Info.Role != "assistant" || (p.Info.ParentID != "" && p.Info.ParentID != m.UserID) {
			return nil, nil
		}
		m.Active = true
		if len(p.Info.Error) > 0 && string(p.Info.Error) != "null" {
			base.Type = "error"
			base.Error = ErrorMessage(p.Info.Error, "OpenCode message failed")
			return one(base), nil
		}
		if p.Info.Time.Completed > 0 && !m.usage[p.Info.ID] {
			m.usage[p.Info.ID] = true
			u := p.Info.Tokens
			if u.Input != nil || u.Output != nil {
				base.Type = "usage.spent"
				base.ItemID = p.Info.ID
				base.Usage = &agent.AgentUsage{Scope: "message", Input: u.Input, Output: u.Output, Cached: u.Cache.Read, Reasoning: u.Reasoning, Total: u.Total}
				return one(base), nil
			}
		}
		return nil, nil
	case "message.part.updated":
		v := p.Part
		if v.MessageID == m.UserID || m.roles[v.MessageID] == "user" {
			return nil, nil
		}
		m.Active = true
		m.parts[v.ID] = v
		base.ItemID = v.ID
		switch v.Type {
		case "text", "reasoning":
			stream := "assistant_text"
			if v.Type == "reasoning" {
				stream = "reasoning_text"
			}
			prev := m.text[v.ID]
			m.text[v.ID] = v.Text
			if prev == v.Text {
				return nil, nil
			}
			base.Type = "content.delta"
			base.Stream = stream
			if strings.HasPrefix(v.Text, prev) {
				base.Delta = strings.TrimPrefix(v.Text, prev)
			} else {
				base.Delta = v.Text
				base.Replace = true
			}
			return one(base), nil
		case "tool":
			base.Type = "item.started"
			if m.tools[v.ID] {
				base.Type = "item.updated"
			}
			m.tools[v.ID] = true
			base.ItemType = "tool_call"
			base.Status = v.State.Status
			if v.State.Status == "completed" || v.State.Status == "error" {
				base.Type = "item.completed"
				if v.State.Status == "error" {
					base.Status = "failed"
				}
			}
			base.Payload = agent.JSON(map[string]any{"toolName": v.Tool, "command": commandInput(v.State.Input), "input": v.State.Input, "aggregatedOutput": v.State.Output, "error": v.State.Error, "part": json.RawMessage(f.Properties)})
			if v.Tool == "bash" {
				base.ItemType = "command_execution"
			}
			return one(base), nil
		default:
			return one(base), nil
		}
	case "message.part.delta":
		if p.MessageID == m.UserID || m.roles[p.MessageID] == "user" {
			return nil, nil
		}
		v, ok := m.parts[p.PartID]
		if !ok || (v.Type != "text" && v.Type != "reasoning") || p.Field != "text" {
			return one(base), nil
		}
		m.Active = true
		m.text[p.PartID] += p.Delta
		base.Type = "content.delta"
		base.ItemID = p.PartID
		base.Stream = "assistant_text"
		if v.Type == "reasoning" {
			base.Stream = "reasoning_text"
		}
		base.Delta = p.Delta
		return one(base), nil
	case "session.diff":
		base.Type = "item.updated"
		base.ItemType = "file_change"
		base.ItemID = "session-diff"
		base.Payload = agent.BoundedJSON(f.Properties)
		return one(base), nil
	case "todo.updated":
		plan := []map[string]any{}
		for _, t := range p.Todos {
			plan = append(plan, map[string]any{"step": t.Content, "status": t.Status})
		}
		base.Type = "item.updated"
		base.ItemType = "plan"
		base.ItemID = "plan"
		base.Payload = agent.JSON(map[string]any{"plan": plan})
		return one(base), nil
	case "permission.asked", "permission.updated", "question.asked":
		r, err := request(f)
		if err != nil {
			return nil, err
		}
		if m.pending[r.ID] {
			return nil, nil
		}
		m.pending[r.ID] = true
		m.Active = true
		base.Type = "request.opened"
		base.Request = r
		return one(base), nil
	case "session.error":
		base.Type = "error"
		base.Error = ErrorMessage(p.Error, "OpenCode session failed")
		return one(base), nil
	case "session.status":
		if p.Status.Type == "idle" && m.Active {
			base.Type = "turn.completed"
			base.Status = "completed"
			return one(base), nil
		}
		return nil, nil
	case "session.idle":
		if m.Active {
			base.Type = "turn.completed"
			base.Status = "completed"
			return one(base), nil
		}
		return nil, nil
	case "permission.v2.asked", "question.v2.asked":
		base.Type = "error"
		base.Error = "unsupported OpenCode interactive event: " + f.Type
		return one(base), nil
	case "permission.replied", "question.replied", "question.rejected":
		return nil, nil // resolved once by the broker
	default:
		return one(base), nil
	}
}
func commandInput(v any) string { m, _ := v.(map[string]any); s, _ := m["command"].(string); return s }
func request(f Frame) (*agent.AgentRequest, error) {
	var p struct {
		ID         string `json:"id"`
		Permission string `json:"permission"`
		Type       string `json:"type"`
		Title      string `json:"title"`
		Tool       struct {
			CallID string `json:"callID"`
		} `json:"tool"`
		Questions []struct {
			Question string `json:"question"`
			Header   string `json:"header"`
			Multiple bool   `json:"multiple"`
			Custom   *bool  `json:"custom"`
			Options  []struct {
				Label       string `json:"label"`
				Description string `json:"description"`
			} `json:"options"`
		} `json:"questions"`
	}
	if err := json.Unmarshal(f.Properties, &p); err != nil {
		return nil, err
	}
	if p.ID == "" {
		return nil, fmt.Errorf("OpenCode request missing ID")
	}
	r := &agent.AgentRequest{ID: "opencode:" + p.ID, Method: f.Type, ItemID: p.Tool.CallID, Payload: agent.BoundedJSON(f.Properties)}
	if f.Type == "question.asked" {
		r.Kind = "user_input"
		for i, q := range p.Questions {
			v := agent.InputQuestion{ID: fmt.Sprint(i), Header: q.Header, Question: q.Question, MultiSelect: q.Multiple, FreeText: q.Custom == nil || *q.Custom}
			for _, o := range q.Options {
				v.Options = append(v.Options, agent.RequestOption{ID: o.Label, Label: o.Label, Description: o.Description})
			}
			r.Questions = append(r.Questions, v)
		}
		if len(r.Questions) == 0 {
			return nil, fmt.Errorf("OpenCode question has no questions")
		}
	} else {
		r.Kind = "approval"
		r.ApprovalKind = "tool"
		r.Message = p.Permission
		if r.Message == "" {
			r.Message = p.Title
		}
		if p.Permission == "bash" {
			r.ApprovalKind = "command"
		}
		if p.Permission == "edit" {
			r.ApprovalKind = "file"
		}
		r.Options = []agent.RequestOption{{ID: "allow_once", Label: "Allow once"}, {ID: "allow_always", Label: "Allow for this instance"}, {ID: "deny", Label: "Deny"}}
	}
	return r, nil
}
