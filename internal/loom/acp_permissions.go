package loom

import (
	"context"
	"encoding/json"
	"errors"
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
	if json.Unmarshal(f.Params, &params) != nil {
		return nil, errors.New("paramètres ACP invalides")
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
		return nil, errors.New("session ACP inactive")
	}
	p.mu.Unlock()
	if ctx == nil || !active || params.SessionID != sid || sid == "" {
		return nil, errors.New("session ACP inactive")
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
		return p.permission(ctx, params.Tool, params.Options)
	default:
		return nil, &acpRPCError{Code: -32601, Message: "méthode client inconnue"}
	}
}
func (p *acpBinding) permission(ctx context.Context, rawTool map[string]any, options []map[string]any) (any, error) {
	p.mu.Lock()
	tool := p.tool(rawTool)
	kind, _ := tool["kind"].(string)
	policy := p.state.Permission
	p.mu.Unlock()
	if len(options) == 0 {
		return nil, errors.New("options de permission requises")
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
		p.publish(DiscussionEvent{"type": "approval_request", "approval": map[string]any{"id": id, "tool": tool, "options": uiOptions}})
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
	p.publish(DiscussionEvent{"type": "approval_resolved", "id": id, "option_id": decision.option, "auto": decision.auto})
	outcome := map[string]any{"outcome": "cancelled"}
	if decision.option != "" {
		outcome = map[string]any{"outcome": "selected", "optionId": decision.option}
	}
	return map[string]any{"outcome": outcome}, nil
}
func (m *runtimeSessions) answerACP(id, approval, option string, cancel bool) error {
	// get checks the vault before consulting private, live approval state.
	if _, ok := m.get(id); !ok {
		return errors.New("discussion introuvable ou verrouillée")
	}
	m.acpMu.Lock()
	p := m.acp[id]
	m.acpMu.Unlock()
	if p == nil {
		return errors.New("session ACP inactive")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	pending := p.approvals[approval]
	if pending == nil || !p.active || p.ctx == nil || p.ctx.Err() != nil {
		return errors.New("approbation absente ou déjà résolue")
	}
	valid := cancel && option == ""
	for _, o := range pending.options {
		if !cancel && o["optionId"] == option {
			valid = true
		}
	}
	if !valid {
		return errors.New("option d’approbation invalide")
	}
	select {
	case pending.answer <- acpDecision{option: option}:
		delete(p.approvals, approval)
		return nil
	default:
		return errors.New("approbation déjà résolue")
	}
}
