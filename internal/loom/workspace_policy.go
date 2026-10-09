package loom

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/events"
	"github.com/lucas-lepajollec/loom/internal/loom/policy"
	agent "github.com/lucas-lepajollec/loom/internal/loom/runtime"
)

const policyStateKey = "capability_policy_v1"

var policySettingsMu sync.Mutex
var policyAudit = &policy.Audit{Capacity: 512}

// The migration marker and rules are one vault-protected record. Existing
// settings remain readable by older clients; conditional defaults track their
// live permission choice without turning a discussion grant into a global one.
func policyDocument() (policy.Document, error) {
	policySettingsMu.Lock()
	defer policySettingsMu.Unlock()
	if !usageVaultAccessStream() {
		return policy.Document{}, errors.New("policy unavailable while vault is locked")
	}
	var d policy.Document
	if raw, ok := getStoreBytes(bkState, policyStateKey); ok && len(raw) > 0 {
		if err := json.Unmarshal(raw, &d); err != nil {
			return d, errors.New("invalid stored policy")
		}
		return d, policy.Validate(d)
	} else if raw, err := getBytesErr(bkState, policyStateKey); err != nil || len(raw) > 0 {
		return d, errors.New("stored policy unreadable")
	}
	d = migratePolicy()
	return d, putStoreJSON(bkState, policyStateKey, d)
}
func migratePolicy() policy.Document {
	d := policy.Document{Version: 1, Migrated: true, Rules: []policy.Rule{}}
	for _, subject := range []string{"agent.command", "agent.file_write", "agent.file_read", "agent.search", "agent.think", "agent.fetch", "agent.tool"} {
		d.Rules = append(d.Rules, policy.Rule{ID: "legacy-full-" + subject, Scope: "global", Subject: subject, Decision: policy.Allow, Migrated: true, Conditions: policy.Conditions{LegacyPermission: "full"}})
		if subject != "agent.command" && subject != "agent.tool" {
			d.Rules = append(d.Rules, policy.Rule{ID: "legacy-edits-" + subject, Scope: "global", Subject: subject, Decision: policy.Allow, Migrated: true, Conditions: policy.Conditions{LegacyPermission: "edits"}})
		}
	}
	// Existing routes already passed the explicit destination consent. Bind it
	// to that discussion, endpoint and model; never grant other destinations.
	for id := range allKV(bkRuntimeSessions) {
		if len(d.Rules) >= 480 {
			break
		}
		var s RuntimeSession
		if getStoreJSON(bkRuntimeSessions, id, &s) && s.RuntimeID != "" && s.RuntimeID != "policy" && s.RuntimeID != "llama.cpp" {
			d.Rules = append(d.Rules, policy.Rule{ID: "legacy-route-" + hashWebKey(id)[:16], Scope: "global", Subject: "data.send_provider", Decision: policy.Allow, Migrated: true, Conditions: policy.Conditions{DiscussionID: id, Operation: "send", AgentID: s.RuntimeID, ProviderID: s.ProviderID, Endpoint: s.Endpoint, Model: s.Model}})
		}
	}
	cfg := ReadConfig()
	var c memoryModelConsent
	if json.Unmarshal([]byte(cfg["brain.consolidation_consent"]), &c) == nil && c.ProviderID != "" {
		d.Rules = append(d.Rules, policy.Rule{ID: "legacy-memory-provider", Scope: "global", Subject: "memory.consolidate", Decision: policy.Allow, Migrated: true, Conditions: policy.Conditions{ProviderID: c.ProviderID, Endpoint: c.Endpoint, Model: c.Model}})
	}
	if endpoint := cfg["brain.consolidation_local_consent"]; endpoint != "" {
		d.Rules = append(d.Rules, policy.Rule{ID: "legacy-memory-engine", Scope: "global", Subject: "memory.consolidate", Decision: policy.Allow, Migrated: true, Conditions: policy.Conditions{Endpoint: endpoint}})
	}
	return d
}
func evaluatePolicy(in policy.Input, dry bool) policy.Result {
	d, err := policyDocument()
	r := policy.Result{Decision: policy.Deny, Reason: "policy_unavailable"}
	if err == nil {
		r = policy.Evaluate(d.Rules, in)
	}
	policyAudit.Record(in, r, dry)
	return r
}
func sessionPolicyInput(s RuntimeSession, subject string, fallback policy.Decision) policy.Input {
	machine := "local"
	if a, ok := acpAgentFor(s.RuntimeID); ok && a.Machine != "" {
		machine = a.Machine
	}
	in := policy.Input{Subject: subject, ProjectID: s.ProjectID, DiscussionID: s.ID, AgentID: s.RuntimeID, MachineID: machine, ProviderID: s.ProviderID, Endpoint: s.Endpoint, Model: s.Model, Workdir: s.Workdir, LegacyPermission: s.Permission, Fallback: fallback}
	if subject == "data.send_provider" {
		in.Operation = "select"
		if fallback == policy.Allow {
			in.Operation = "send"
		}
	}
	return in
}
func (m *runtimeSessions) authorizePolicy(ctx context.Context, in policy.Input, consent bool) error {
	r := evaluatePolicy(in, false)
	if r.Decision == policy.Allow {
		return nil
	}
	if r.Decision == policy.Deny {
		if r.Reason == "policy_unavailable" {
			return errors.New("policy unavailable (vault locked or storage unreadable)")
		}
		return errors.New("policy denied capability: " + in.Subject)
	}
	// Existing explicit consent satisfies only the historical consent gate.
	// An explicit confirm rule always opens an InteractionRequest.
	if r.RuleID == "" && in.Fallback == policy.Confirm {
		return m.authorizeLegacyConsent(in, consent)
	}
	return m.confirmPolicy(ctx, in)
}

// Confirmations outside a running discussion get a metadata-only task in the
// existing session/Tasks journal. No messages, prompts or executor are created.
func (m *runtimeSessions) confirmPolicy(ctx context.Context, in policy.Input, opened ...func(string, string)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 24*time.Hour)
	defer cancel()
	id := in.DiscussionID
	m.mu.Lock()
	run := m.runs[id]
	standalone := run == nil
	if standalone {
		if len(m.runs) >= 4 {
			m.mu.Unlock()
			return errors.New("too many running tasks")
		}
		id = "policy-" + newSessionID()
		now := time.Now().UnixMilli()
		s := RuntimeSession{ID: id, Title: "Policy approval", ProjectID: in.ProjectID, RuntimeID: "policy", Status: "running", CreatedAt: now, UpdatedAt: now, Messages: []Message{}, Turns: []RuntimeTurnRecord{{StartedAt: now, RuntimeID: "policy"}}}
		if err := putStoreJSON(bkRuntimeSessions, id, s); err != nil {
			m.mu.Unlock()
			return err
		}
		run = &runtimeRun{session: s, cancel: cancel}
		m.runs[id] = run
		m.taskEventLocked(s, events.TaskStarted, nil)
	}
	m.mu.Unlock()
	m.acpMu.Lock()
	b := m.requests[id]
	owned := b == nil
	if owned {
		b = agent.NewRequestBroker("policy", func(e AgentEvent) bool {
			m.mu.Lock()
			defer m.mu.Unlock()
			live := m.runs[id]
			if live == nil || ctx.Err() != nil && e.Type == "request.opened" {
				return false
			}
			row := DiscussionEvent{"type": e.Type, "agent_event": e, "request": e.Request, "request_id": e.RequestID, "outcome": e.Outcome}
			if e.Type == "request.opened" {
				live.session.PendingRequests = append(live.session.PendingRequests, *e.Request)
				m.taskEventLocked(live.session, events.TaskWaiting, e.Request)
			} else {
				next := live.session.PendingRequests[:0]
				for _, request := range live.session.PendingRequests {
					if request.ID != e.RequestID {
						next = append(next, request)
					}
				}
				live.session.PendingRequests = next
				m.taskEventLocked(live.session, events.TaskResumed, nil)
			}
			live.session.Turns[len(live.session.Turns)-1].ACPEvents = append(live.session.Turns[len(live.session.Turns)-1].ACPEvents, row)
			if putStoreJSON(bkRuntimeSessions, id, live.session) != nil {
				return false
			}
			m.publishLocked(id, row)
			return true
		})
		if m.requests == nil {
			m.requests = map[string]*agent.RequestBroker{}
		}
		m.requests[id] = b
	}
	m.acpMu.Unlock()
	finalStatus := "cancelled"
	defer func() {
		if owned {
			b.Cancel()
			m.acpMu.Lock()
			delete(m.requests, id)
			m.acpMu.Unlock()
		}
		if standalone {
			m.mu.Lock()
			defer m.mu.Unlock()
			run.session.Status = finalStatus
			run.session.PendingRequests = nil
			t := &run.session.Turns[len(run.session.Turns)-1]
			t.DurationSeconds = time.Since(time.UnixMilli(t.StartedAt)).Seconds()
			t.FinishedAt = time.Now().UnixMilli()
			_ = putStoreJSON(bkRuntimeSessions, id, run.session)
			event := events.TaskCompleted
			if finalStatus != "complete" {
				event = events.TaskFailed
			}
			m.taskEventLocked(run.session, event, nil)
			delete(m.runs, id)
		}
	}()
	req := &agent.AgentRequest{ID: "policy:" + newSessionID(), Kind: "approval", Method: "policy.confirm", ApprovalKind: "tool", Message: in.Subject, Options: []agent.RequestOption{{ID: "allow_once", Label: "Allow once"}, {ID: "deny", Label: "Deny"}}}
	// Trusted identifiers let the UI name the destination without retaining a
	// prompt, credential, endpoint, command or private path in a policy task.
	req.Payload = agent.JSON(struct {
		Subject    string `json:"subject"`
		ProjectID  string `json:"project_id,omitempty"`
		AgentID    string `json:"agent_id,omitempty"`
		MachineID  string `json:"machine_id,omitempty"`
		ProviderID string `json:"provider_id,omitempty"`
		Model      string `json:"model,omitempty"`
	}{in.Subject, in.ProjectID, in.AgentID, in.MachineID, in.ProviderID, in.Model})
	wait, err := b.Open(ctx, AgentEvent{Type: "request.opened", Runtime: "policy", Request: req, Raw: agent.JSON(map[string]string{"subject": in.Subject})})
	if err != nil {
		return err
	}
	if len(opened) > 0 {
		opened[0](id, req.ID)
	}
	a, err := wait(ctx)
	if err != nil {
		return err
	}
	decision := policy.Deny
	if a.Decision == "allow_once" && ctx.Err() == nil && usageVaultAccessStream() {
		decision = policy.Allow
	}
	policyAudit.Record(in, policy.Result{Decision: decision, Reason: "interaction_answer"}, false)
	if decision != policy.Allow {
		if a.Decision == "deny" {
			finalStatus = "failed"
		}
		return errors.New("policy approval declined or cancelled")
	}
	// Re-evaluate in case a deny was saved while the user was deciding.
	if evaluatePolicy(in, false).Decision == policy.Deny {
		finalStatus = "failed"
		return errors.New("policy changed: capability denied")
	}
	finalStatus = "complete"
	return nil
}
func handlePolicy(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !usageVaultAccess(w) {
		return
	}
	if r.Method == http.MethodGet {
		d, err := policyDocument()
		if err != nil {
			sendJSON(w, 503, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		sendJSON(w, 200, d)
		return
	}
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var d policy.Document
	if !workspaceDecode(w, r, &d) {
		return
	}
	if err := policy.Validate(d); err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	// The marker is server-owned; clients can replace/remove migrated rules.
	d.Migrated = true
	policySettingsMu.Lock()
	err := putStoreJSON(bkState, policyStateKey, d)
	policySettingsMu.Unlock()
	if err != nil {
		sendJSON(w, 503, map[string]any{"ok": false, "error": "policy not saved"})
		return
	}
	sendJSON(w, 200, d)
}
func handlePolicyEvaluate(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) || !usageVaultAccess(w) {
		return
	}
	var req struct {
		policy.Input
		Permission string `json:"permission,omitempty"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	in := req.Input
	if !policy.ValidSubject(in.Subject) || strings.Contains(in.Subject, "*") || len(in.Command) > 4096 || len(in.Path) > 4096 || len(in.Operation) > 80 {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "invalid policy input"})
		return
	}
	if req.Permission != "" && req.Permission != "ask" && req.Permission != "edits" && req.Permission != "full" {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "invalid permission"})
		return
	}
	in.LegacyPermission = req.Permission
	if in.DiscussionID != "" {
		if s, ok := workspaceSessions.get(in.DiscussionID); ok {
			if in.LegacyPermission == "" {
				in.LegacyPermission = s.Permission
			}
			if in.Workdir == "" {
				in.Workdir = s.Workdir
			}
		}
	}
	in.Fallback = policy.Confirm
	if in.Subject == "data.send_provider" {
		if in.Operation == "" {
			in.Operation = "select"
		}
		if in.Operation == "send" {
			in.Fallback = policy.Allow
		}
	}
	if strings.HasPrefix(in.Subject, "node.") || strings.HasPrefix(in.Subject, "mcp.tool:") || in.Subject == "spend.provider" {
		in.Fallback = policy.Allow
	}
	if in.Subject == "notification.summary" {
		if cfg, err := notificationConfig(); err == nil && cfg.Rules.IncludeSummaries {
			in.Fallback = policy.Allow
		}
	}
	sendJSON(w, 200, evaluatePolicy(in, true))
}
func handlePolicyAudit(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodGet) || !usageVaultAccess(w) {
		return
	}
	sendJSON(w, 200, map[string]any{"entries": policyAudit.Snapshot()})
}

func agentToolSubject(kind string) string {
	switch kind {
	case "execute", "command":
		return "agent.command"
	case "edit", "delete", "move", "file":
		return "agent.file_write"
	case "read":
		return "agent.file_read"
	case "search":
		return "agent.search"
	case "think":
		return "agent.think"
	case "fetch":
		return "agent.fetch"
	default:
		return "agent.tool"
	}
}
func (p *acpBinding) policyInput(subject string, fallback policy.Decision) policy.Input {
	p.mu.Lock()
	state := cloneACPState(p.state)
	p.mu.Unlock()
	s := RuntimeSession{ID: p.id, RuntimeID: p.agentID, ACPState: state}
	if p.manager != nil {
		if saved, ok := p.manager.get(p.id); ok {
			s.ProjectID, s.ProviderID, s.Endpoint, s.Model = saved.ProjectID, saved.ProviderID, saved.Endpoint, saved.Model
		}
	}
	return sessionPolicyInput(s, subject, fallback)
}

type policyContextKey struct{}
type policyContext struct {
	manager *runtimeSessions
	session RuntimeSession
}

func withPolicySession(ctx context.Context, m *runtimeSessions, s RuntimeSession) context.Context {
	return context.WithValue(ctx, policyContextKey{}, policyContext{m, s})
}
func policyTurn(ctx context.Context) (*runtimeSessions, RuntimeSession) {
	if p, ok := ctx.Value(policyContextKey{}).(policyContext); ok {
		return p.manager, p.session
	}
	return workspaceSessions, RuntimeSession{}
}
func authorizeProviderTurn(ctx context.Context, p CloudProvider) error {
	m, s := policyTurn(ctx)
	in := sessionPolicyInput(s, "data.send_provider", policy.Allow)
	in.ProviderID, in.Endpoint, in.Model = p.ID, p.Endpoint, p.Model
	if err := m.authorizePolicy(ctx, in, false); err != nil {
		return err
	}
	in.Subject, in.Cost = "spend.provider", m.observedMonthlySpend(p.ID, p.Endpoint)
	return m.authorizePolicy(ctx, in, false)
}
func (m *runtimeSessions) observedMonthlySpend(id, endpoint string) *float64 {
	if m.balances == nil {
		return nil
	}
	m.mu.Lock()
	key := m.keys[id]
	m.mu.Unlock()
	m.balances.mu.Lock()
	defer m.balances.mu.Unlock()
	e, ok := m.balances.items[id]
	if !ok || e.identity.Endpoint != endpoint || e.identity.Credential != sha256.Sum256([]byte(key)) || e.result.Error != "" || time.Since(e.at) >= providerBalanceTTL {
		return nil
	}
	if e.result.PeriodUsage.Month == nil {
		return nil
	}
	value := *e.result.PeriodUsage.Month
	return &value
}
func authorizeEngineTurn(ctx context.Context) error {
	return authorizeEngineDestination(ctx, currentEngineNode())
}
func authorizeEngineDestination(ctx context.Context, n *engineNode) error {
	if n == nil {
		return nil
	}
	m, s := policyTurn(ctx)
	in := sessionPolicyInput(s, "data.send_provider", policy.Allow)
	in.Endpoint = strings.TrimRight(n.V1, "/")
	in.Model = n.Model
	return m.authorizePolicy(ctx, in, false)
}

// Restrictive agent policies must keep the upstream's reply channel enabled.
// This conservatively includes conditional rules: their inputs arrive later.
func policyNeedsAgentApprovals(s RuntimeSession) bool {
	d, err := policyDocument()
	if err != nil {
		return true
	}
	in := sessionPolicyInput(s, "agent.command", policy.Confirm)
	for _, r := range d.Rules {
		if r.Migrated || r.Decision == policy.Allow || r.Subject != "*" && !strings.HasPrefix(r.Subject, "agent.") {
			continue
		}
		if r.Scope == "global" || r.Scope == "agent" && r.ScopeID == in.AgentID || r.Scope == "project" && r.ScopeID == in.ProjectID || r.Scope == "machine" && r.ScopeID == in.MachineID {
			return true
		}
	}
	return false
}
func nativePolicyBroker(b *agent.RequestBroker, s RuntimeSession) {
	input := func(r *agent.AgentRequest) policy.Input {
		in := sessionPolicyInput(s, agentToolSubject(r.ApprovalKind), policy.Confirm)
		in.LegacyPermission = "ask"
		var params struct {
			Command string `json:"command"`
			Path    string `json:"path"`
		}
		_ = json.Unmarshal(r.Payload, &params)
		in.Command, in.Path = params.Command, params.Path
		return in
	}
	b.AuthorizeAnswer = func(ctx context.Context, r agent.AgentRequest, a agent.RequestAnswer) (agent.RequestAnswer, error) {
		if r.Kind != "approval" || r.Method == "policy.confirm" {
			return a, nil
		}
		in := input(&r)
		result := evaluatePolicy(in, false)
		if result.Decision == policy.Deny || ctx.Err() != nil || !usageVaultAccessStream() {
			a = agent.RequestAnswer{Decision: "cancel"}
		}
		decision := policy.Deny
		if a.Decision == "allow_always" && policyNeedsAgentApprovals(s) {
			a.Decision = "cancel"
			for _, option := range r.Options {
				if option.ID == "allow_once" {
					a.Decision = "allow_once"
					break
				}
			}
		}
		if a.Decision == "allow_once" || a.Decision == "allow_always" || a.Decision == "accept" || a.Decision == "once" || a.Decision == "acceptForSession" {
			decision = policy.Allow
		}
		policyAudit.Record(in, policy.Result{Decision: decision, Reason: "interaction_answer"}, false)
		return a, nil
	}
	b.Decide = func(ctx context.Context, e AgentEvent) (agent.RequestAnswer, bool, error) {
		if e.Request == nil || e.Request.Kind != "approval" || e.Request.Method == "policy.confirm" {
			return agent.RequestAnswer{}, false, nil
		}
		r := e.Request
		in := input(r)
		// An explicit native request historically always asked, even when the
		// CLI's launch policy was full. Only user rules may auto-answer it.
		result := evaluatePolicy(in, false)
		if result.Decision == policy.Confirm {
			return agent.RequestAnswer{}, false, nil
		}
		wanted := []string{"allow_once", "accept", "once"}
		if result.Decision == policy.Deny {
			wanted = []string{"deny", "decline", "reject"}
		}
		for _, option := range r.Options {
			for _, id := range wanted {
				if option.ID == id {
					return agent.RequestAnswer{Decision: id}, true, nil
				}
			}
		}
		if result.Decision == policy.Deny {
			return agent.RequestAnswer{Decision: "cancel"}, true, nil
		}
		return agent.RequestAnswer{}, false, nil
	}
}
func policyAPI(register func(string, http.HandlerFunc)) func(string, http.HandlerFunc) {
	return func(path string, handler http.HandlerFunc) {
		register(path, func(w http.ResponseWriter, r *http.Request) {
			subject := ""
			linked := currentEngineNode()
			remoteEngine := linked != nil && !linked.Direct
			if r.Method == http.MethodPost {
				if strings.HasSuffix(path, "/node/migrate") || remoteEngine && (path == "/api/llamacpp/install" || path == "/api/llamacpp/install-custom" || path == "/api/llamacpp/prebuilt") {
					subject = "node.install"
				}
				if strings.HasSuffix(path, "/update/apply") || remoteEngine && path == "/api/llamacpp/update" {
					subject = "node.update"
				}
				if remoteEngine && path == "/api/engines/vllm" && r.Body != nil {
					raw, err := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
					_ = r.Body.Close()
					if err != nil || len(raw) > 1<<20 {
						sendJSON(w, 400, map[string]any{"ok": false, "error": "invalid action body"})
						return
					}
					r.Body = io.NopCloser(bytes.NewReader(raw))
					var req struct {
						Action string `json:"action"`
					}
					if json.Unmarshal(raw, &req) == nil && (req.Action == "install" || req.Action == "update") {
						subject = "node." + req.Action
					}
				}
			}
			if subject != "" {
				machine := r.PathValue("id")
				if machine == "" {
					machine = "local"
				}
				if remoteEngine && (strings.HasPrefix(path, "/api/llamacpp/") || path == "/api/engines/vllm" || strings.HasPrefix(path, "/api/engine/node/update")) {
					machine = currentEngineCapabilityOwner()
					for _, saved := range loadRemoteMachines() {
						if node := savedMachineNode(saved); node != nil && node.URL == linked.URL {
							machine = saved.ID
							break
						}
					}
				}
				if !usageVaultAccess(w) {
					return
				}
				if err := workspaceSessions.authorizePolicy(r.Context(), policy.Input{Subject: subject, MachineID: machine, Fallback: policy.Allow}, false); err != nil {
					sendJSON(w, 403, map[string]any{"ok": false, "error": err.Error()})
					return
				}
			}
			handler(w, r)
		})
	}
}

// Every Loom-owned model POST, including compaction/Brain helpers, consults
// the same destination and observed-spend policies before any body is sent.
func authorizeModelDestination(ctx context.Context, endpoint, model string) error {
	m, session := policyTurn(ctx)
	base := strings.TrimSuffix(strings.TrimSuffix(endpoint, "/chat/completions"), "/embeddings")
	for _, provider := range m.providers() {
		if strings.TrimRight(provider.Endpoint, "/") == strings.TrimRight(base, "/") {
			provider.Model = model
			return authorizeProviderTurn(ctx, provider)
		}
	}
	if n := currentEngineNode(); n != nil && strings.TrimRight(base, "/") == strings.TrimRight(n.V1, "/") {
		in := sessionPolicyInput(session, "data.send_provider", policy.Allow)
		in.Endpoint, in.Model = base, model
		return m.authorizePolicy(ctx, in, false)
	}
	if strings.TrimRight(base, "/") == strings.TrimRight(llamaBackendURL().String(), "/")+"/v1" {
		return nil
	}
	in := sessionPolicyInput(session, "data.send_provider", policy.Allow)
	in.Endpoint, in.Model = base, model
	return m.authorizePolicy(ctx, in, false)
}
