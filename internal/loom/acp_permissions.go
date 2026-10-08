package loom

import (
	"context"
	"encoding/json"
	"errors"
	agent "github.com/lucas-lepajollec/loom/internal/loom/runtime"
	"net/http"
	"strings"
	"time"
)

func acpAutoOption(policy, kind string, options []map[string]any) string {
	allow := policy == "full" || policy == "edits" && (kind == "read" || kind == "search" || kind == "edit" || kind == "think" || kind == "fetch")
	if allow {
		for _, option := range options {
			if option["kind"] == "allow_once" {
				id, _ := option["optionId"].(string)
				return id
			}
		}
	}
	return ""
}
func (p *acpBinding) handleRequest(f *acpFrame) (any, error) {
	var params struct {
		SessionID string           `json:"sessionId"`
		Path      string           `json:"path"`
		Content   string           `json:"content"`
		Line      *int             `json:"line"`
		Limit     *int             `json:"limit"`
		Tool      map[string]any   `json:"toolCall"`
		Options   []map[string]any `json:"options"`
	}
	if f.Method != "fs/read_text_file" && f.Method != "fs/write_text_file" && f.Method != "session/request_permission" && f.Method != "session/create_elicitation" {
		p.publishRawACP(*f)
		p.publish(DiscussionEvent{"type": "error", "error": "unsupported ACP client request: " + f.Method})
		return nil, &acpRPCError{Code: -32601, Message: "unsupported client request"}
	}
	if json.Unmarshal(f.Params, &params) != nil {
		return nil, errors.New("invalid ACP parameters")
	}
	p.mu.Lock()
	ctx := p.ctx
	sid := p.state.NativeSessionID
	active := p.active
	if ctx != nil && active && params.SessionID == sid && sid != "" {
		p.requestWG.Add(1)
		f.Replied = p.requestWG.Done
	} else {
		p.mu.Unlock()
		return nil, errors.New("inactive ACP session")
	}
	p.mu.Unlock()
	if ctx == nil || !active || params.SessionID != sid || sid == "" {
		return nil, errors.New("inactive ACP session")
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-p.client.done:
		return nil, errACPClosed
	default:
	}
	switch f.Method {
	case "fs/read_text_file":
		return p.readFile(params.Path, params.Line, params.Limit)
	case "fs/write_text_file":
		return p.writeFile(params.Path, params.Content)
	case "session/request_permission":
		return p.permission(ctx, params.Tool, params.Options, f)
	case "session/create_elicitation":
		return p.elicitation(ctx, f)
	default:
		e := AgentEvent{Type: "raw", Runtime: p.agentID, Method: f.Method, Raw: agent.BoundedJSON(f.Raw), Payload: agent.BoundedJSON(f.Params)}
		p.publish(DiscussionEvent{"type": "tool_end", "tool": map[string]any{"id": "raw:" + string(f.ID), "kind": "other", "title": f.Method, "status": "failed", "output": string(e.Payload)}, "agent_event": e})
		p.publish(DiscussionEvent{"type": "error", "error": "unsupported ACP client request: " + f.Method})
		return nil, &acpRPCError{Code: -32601, Message: "unknown client method"}
	}
}
func (p *acpBinding) permission(ctx context.Context, rawTool map[string]any, options []map[string]any, frames ...*acpFrame) (any, error) {
	p.mu.Lock()
	tool := p.tool(rawTool)
	kind, _ := tool["kind"].(string)
	policy := p.state.Permission
	if p.state.FilesystemPolicy == "workspace-write" {
		policy = "ask"
	} // Never auto-approve native sandbox escalation.
	p.mu.Unlock()
	if len(options) == 0 {
		return nil, errors.New("permission options required")
	}
	id := newSessionID()
	decision := acpDecision{option: acpAutoOption(policy, kind, options), auto: true}
	if decision.option == "" {
		pending := &acpApproval{options: options, answer: make(chan acpDecision, 1)}
		p.mu.Lock()
		p.approvals[id] = pending
		p.mu.Unlock()
		defer func() { p.mu.Lock(); delete(p.approvals, id); p.mu.Unlock() }()
		uiOptions := []map[string]any{}
		for _, o := range options {
			uiOptions = append(uiOptions, map[string]any{"id": o["optionId"], "name": o["name"], "kind": o["kind"]})
		}
		r := &agent.AgentRequest{ID: id, Kind: "approval", Method: "session/request_permission", ItemID: firstNonEmptyString(tool["id"]), ApprovalKind: "tool"}
		if kind == "execute" {
			r.ApprovalKind = "command"
		}
		if kind == "edit" || kind == "delete" || kind == "move" {
			r.ApprovalKind = "file"
		}
		for _, o := range options {
			optionID, _ := o["optionId"].(string)
			label, _ := o["name"].(string)
			r.Options = append(r.Options, agent.RequestOption{ID: optionID, Label: label})
		}
		raw := agent.JSON(map[string]any{"toolCall": rawTool, "options": options})
		if len(frames) > 0 {
			raw = agent.BoundedJSON(frames[0].Raw)
			r.Payload = agent.BoundedJSON(frames[0].Params)
		}
		e := AgentEvent{Type: "request.opened", Runtime: p.agentID, Request: r, Raw: raw}
		p.publish(DiscussionEvent{"type": "approval_request", "approval": map[string]any{"id": id, "tool": tool, "options": uiOptions}, "agent_event": e})
		grace := p.approvalGrace
		if grace <= 0 {
			grace = 30 * time.Minute
		}
		ticker := time.NewTicker(min(time.Second, grace))
		defer ticker.Stop()
		absentSince := time.Now()
	wait:
		for {
			select {
			case decision = <-pending.answer:
				break wait
			case <-ctx.Done():
				decision = acpDecision{}
				break wait
			case <-p.client.done:
				decision = acpDecision{}
				break wait
			case now := <-ticker.C:
				p.manager.mu.Lock()
				subscribed := len(p.manager.subscribers[p.id]) > 0
				p.manager.mu.Unlock()
				if subscribed {
					absentSince = now
				} else if now.Sub(absentSince) >= grace {
					decision = acpDecision{auto: true}
					break wait
				}
			}
		}
	}
	// Cancellation always wins over a simultaneously submitted approval.
	if ctx.Err() != nil {
		decision.option = ""
	}
	outcomeName := "accepted"
	if decision.option == "" {
		outcomeName = "cancelled"
	}
	for _, o := range options {
		if o["optionId"] == decision.option {
			kind, _ := o["kind"].(string)
			if strings.HasPrefix(kind, "reject") {
				outcomeName = "declined"
			}
		}
	}
	p.publish(DiscussionEvent{"type": "approval_resolved", "id": id, "option_id": decision.option, "auto": decision.auto, "agent_event": AgentEvent{Type: "request.resolved", Runtime: p.agentID, RequestID: id, Outcome: outcomeName, Decision: decision.option, Raw: agent.JSON(map[string]any{"optionId": decision.option})}})
	outcome := map[string]any{"outcome": "cancelled"}
	if decision.option != "" {
		outcome = map[string]any{"outcome": "selected", "optionId": decision.option}
	}
	return map[string]any{"outcome": outcome}, nil
}
func (m *runtimeSessions) answerACP(id, approval, option string, cancel bool) error {
	// get checks the vault before consulting private, live approval state.
	if _, ok := m.get(id); !ok {
		return errors.New("discussion not found or locked")
	}
	m.acpMu.Lock()
	broker := m.requests[id]
	p := m.acp[id]
	m.acpMu.Unlock()
	if broker != nil && (strings.HasPrefix(approval, "codex:") || strings.HasPrefix(approval, "pi:") || strings.HasPrefix(approval, "acp:")) {
		decision := option
		if cancel {
			decision = "cancel"
		}
		return broker.Resolve(approval, agent.RequestAnswer{Decision: decision})
	}
	if p == nil {
		return errors.New("inactive ACP session")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	pending := p.approvals[approval]
	if pending == nil || !p.active || p.ctx == nil || p.ctx.Err() != nil {
		return errors.New("approval missing or already resolved")
	}
	valid := cancel && option == ""
	for _, o := range pending.options {
		if !cancel && o["optionId"] == option {
			valid = true
		}
	}
	if !valid {
		return errors.New("invalid approval option")
	}
	select {
	case pending.answer <- acpDecision{option: option}:
		delete(p.approvals, approval)
		return nil
	default:
		return errors.New("approval already resolved")
	}
}

// The canonical endpoint and legacy approval endpoint share vault checks and
// response channels. Native requests never use a second permission registry.
func (m *runtimeSessions) answerRequest(id, requestID string, answer agent.RequestAnswer) error {
	if _, ok := m.get(id); !ok {
		return errors.New("discussion not found or locked")
	}
	m.acpMu.Lock()
	broker := m.requests[id]
	m.acpMu.Unlock()
	if broker != nil && (strings.HasPrefix(requestID, "codex:") || strings.HasPrefix(requestID, "pi:") || strings.HasPrefix(requestID, "acp:")) {
		return broker.Resolve(requestID, answer)
	}
	option := answer.Decision
	if answer.Decision == "cancel" {
		return m.answerACP(id, requestID, "", true)
	}
	return m.answerACP(id, requestID, option, false)
}
func handleAgentRequest(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) || !usageVaultAccess(w) {
		return
	}
	var answer agent.RequestAnswer
	if !workspaceDecode(w, r, &answer) {
		return
	}
	if err := workspaceSessions.answerRequest(r.PathValue("id"), r.PathValue("request_id"), answer); err != nil {
		sendJSON(w, 409, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true})
}

func firstNonEmptyString(v any) string { s, _ := v.(string); return s }
func (p *acpBinding) elicitation(ctx context.Context, f *acpFrame) (any, error) {
	var params struct {
		Message string          `json:"message"`
		Mode    string          `json:"mode"`
		Schema  json.RawMessage `json:"requestedSchema"`
		URL     string          `json:"url"`
	}
	if err := json.Unmarshal(f.Params, &params); err != nil {
		return nil, err
	}
	if params.Mode != "form" && params.Mode != "url" {
		return nil, errors.New("unsupported ACP elicitation mode")
	}
	if p.broker == nil {
		return nil, errors.New("inactive request broker")
	}
	r := &agent.AgentRequest{ID: "acp:" + string(f.ID), Kind: "elicitation", Method: f.Method, Message: params.Message, Schema: params.Schema, URL: params.URL, Payload: agent.BoundedJSON(f.Params)}
	e := AgentEvent{Type: "request.opened", Runtime: p.agentID, ThreadID: p.state.NativeSessionID, Method: f.Method, Request: r, Raw: agent.BoundedJSON(f.Raw)}
	a, err := p.broker.Ask(ctx, e)
	if err != nil {
		return nil, err
	}
	return map[string]any{"action": a.Decision, "content": a.Content}, nil
}
