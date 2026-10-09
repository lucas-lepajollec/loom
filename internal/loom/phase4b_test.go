package loom

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/capability"
	"github.com/lucas-lepajollec/loom/internal/loom/events"
	"github.com/lucas-lepajollec/loom/internal/loom/notify"
	"github.com/lucas-lepajollec/loom/internal/loom/policy"
	agent "github.com/lucas-lepajollec/loom/internal/loom/runtime"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func isolatePhase4b(t *testing.T) *runtimeSessions {
	t.Helper()
	testHome(t)
	old, oldState, oldAudit := workspaceSessions, degradedCapabilities, policyAudit
	m := newRuntimeSessions()
	workspaceSessions = m
	degradedCapabilities = &capability.State{}
	policyAudit = &policy.Audit{Capacity: 512}
	t.Cleanup(func() {
		m.shutdownACP()
		workspaceSessions, degradedCapabilities, policyAudit = old, oldState, oldAudit
	})
	return m
}
func TestPhase4bLegacyConsentRequestRetryAndShutdown(t *testing.T) {
	m := isolatePhase4b(t)
	savePolicyRules(t)
	in := policy.Input{Subject: "data.send_provider", ProviderID: "p", Endpoint: "https://provider.test/v1", Model: "model", Fallback: policy.Confirm}
	err := m.authorizePolicy(t.Context(), in, false)
	var required *policyConsentRequired
	if !errors.As(err, &required) {
		t.Fatal(err)
	}
	if len(m.list()) != 0 {
		t.Fatal("policy task appeared as a discussion")
	}
	if err := m.answerRequest(required.request.SessionID, required.request.RequestID, agent.RequestAnswer{Decision: "allow_once"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-required.request.done:
	case <-time.After(time.Second):
		t.Fatal("consent worker did not settle")
	}
	if err := m.authorizePolicy(t.Context(), in, false); err != nil {
		t.Fatal("answer not consumed by retry", err)
	}
	err = m.authorizePolicy(t.Context(), in, false)
	if !errors.As(err, &required) {
		t.Fatal("grant reused", err)
	}
	if err := m.authorizePolicy(t.Context(), in, true); err != nil {
		t.Fatal(err)
	}
	err = m.authorizePolicy(t.Context(), in, false)
	if !errors.As(err, &required) {
		t.Fatal("explicit consent left a reusable grant", err)
	}
	done := make(chan struct{})
	go func() { m.shutdownACP(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shutdown left a consent worker waiting")
	}
}
func TestPhase4bNativePolicyRecheckedOnAnswer(t *testing.T) {
	isolatePhase4b(t)
	savePolicyRules(t, policy.Rule{ID: "confirm", Scope: "global", Subject: "agent.command", Decision: policy.Confirm})
	b := agent.NewRequestBroker("fixture", func(AgentEvent) bool { return true })
	nativePolicyBroker(b, RuntimeSession{RuntimeID: "fixture"})
	request := &agent.AgentRequest{ID: "codex:1", Kind: "approval", ApprovalKind: "command", Method: "native.command", Options: []agent.RequestOption{{ID: "allow_once"}, {ID: "allow_always"}, {ID: "deny"}}}
	wait, err := b.Open(t.Context(), AgentEvent{Type: "request.opened", Request: request})
	if err != nil {
		t.Fatal(err)
	}
	savePolicyRules(t, policy.Rule{ID: "deny", Scope: "global", Subject: "agent.command", Decision: policy.Deny})
	if err := b.Resolve(request.ID, agent.RequestAnswer{Decision: "allow_once"}); err != nil {
		t.Fatal(err)
	}
	answer, err := wait(t.Context())
	if err != nil || answer.Decision != "cancel" {
		t.Fatal(answer, err)
	}
	savePolicyRules(t, policy.Rule{ID: "confirm", Scope: "global", Subject: "agent.command", Decision: policy.Confirm})
	request.ID = "codex:2"
	wait, err = b.Open(t.Context(), AgentEvent{Type: "request.opened", Request: request})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Resolve(request.ID, agent.RequestAnswer{Decision: "allow_always"}); err != nil {
		t.Fatal(err)
	}
	answer, err = wait(t.Context())
	if err != nil || answer.Decision != "allow_once" {
		t.Fatal("persistent grant bypassed central policy", answer, err)
	}
}
func TestPhase4bMCPPolicyAndScopedCredentials(t *testing.T) {
	m := isolatePhase4b(t)
	savePolicyRules(t, policy.Rule{ID: "deny-tool", Scope: "project", ScopeID: "project", Subject: "mcp.tool:server/echo", Decision: policy.Deny})
	s := RuntimeSession{ID: "session", RuntimeID: "fixture", ProjectID: "project"}
	m.runs[s.ID] = &runtimeRun{session: s, cancel: func() {}}
	r := httptest.NewRequest("POST", "/mcp/loom", nil)
	r.Header.Set("Authorization", "Bearer "+scopedGatewayToken(s.ID))
	ctx, ok := gatewayPolicyContext(r)
	if !ok {
		t.Fatal("launch-scoped token rejected")
	}
	called := false
	gate := gatewayPolicyMiddleware(map[string]string{"echo": "mcp.tool:server/echo"})(func(context.Context, string, mcp.Request) (mcp.Result, error) {
		called = true
		return &mcp.CallToolResult{}, nil
	})
	result, err := gate(ctx, "tools/call", &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Name: "echo", Arguments: json.RawMessage(`{}`)}})
	if err != nil || called || !result.(*mcp.CallToolResult).IsError {
		t.Fatal("denied MCP call reached upstream", err)
	}
	delete(m.runs, s.ID)
	if _, ok := gatewayPolicyContext(r); ok {
		t.Fatal("ended run retained scoped authorization")
	}
}
func TestPhase4bObservedSpendAndEngineRecovery(t *testing.T) {
	m := isolatePhase4b(t)
	limit, observed := 10.0, 11.0
	savePolicyRules(t, policy.Rule{ID: "cap", Scope: "global", Subject: "spend.provider", Decision: policy.Allow, Conditions: policy.Conditions{ProviderID: "p", MaxCost: &limit}})
	m.keys["p"] = "key"
	identity := providerBalanceIdentity{ID: "p", Endpoint: "https://provider.test/v1", Credential: sha256.Sum256([]byte("key"))}
	m.balances = &providerBalanceCache{items: map[string]providerBalanceEntry{"p": {identity: identity, at: time.Now(), result: ProviderBalance{PeriodUsage: ProviderPeriodUsage{Month: &observed}}}}}
	p := CloudProvider{ID: "p", Endpoint: identity.Endpoint, Model: "model"}
	if err := authorizeProviderTurn(t.Context(), p); err == nil {
		t.Fatal("known monthly cap not enforced")
	}
	harness := &acpAdapter{agent: acpAgent{ID: "fixture"}, sessions: m, session: RuntimeSession{ID: "session", RuntimeID: "fixture", ProviderID: p.ID, Endpoint: p.Endpoint}}
	if _, err := harness.Run(t.Context(), RuntimeTurn{}, func(StreamEvent) bool { return true }); err == nil || !strings.Contains(err.Error(), "spend.provider") {
		t.Fatal("harness source bypassed the observed provider cap", err)
	}
	m.keys["p"] = "rotated"
	if err := authorizeProviderTurn(t.Context(), p); err != nil {
		t.Fatal("unknown usage treated as known", err)
	}
	oldNode, oldClient := currentEngineNode(), nodeClient
	t.Cleanup(func() { _ = setEngineNode(oldNode); nodeClient = oldClient })
	if err := setEngineNode(&engineNode{Direct: true, URL: "https://engine.test", V1: "https://engine.test/v1", Kind: "llama.cpp"}); err != nil {
		t.Fatal(err)
	}
	online := false
	nodeClient = &http.Client{Transport: phase4Transport{http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !online {
			w.WriteHeader(503)
			return
		}
		sendJSON(w, 200, map[string]any{"status": "ok"})
	})}}
	if c := doctorEngine(t.Context()); c.Status != "warn" || !capabilityDisabled(currentEngineCapabilityOwner(), "chat") {
		t.Fatal(c)
	}
	if hasRuntimeCapability((llamaRuntimeAdapter{}).Descriptor(), "chat") {
		t.Fatal("engine descriptor retained failed feature")
	}
	online = true
	if c := doctorEngine(t.Context()); c.Status != "ok" || capabilityDisabled(currentEngineCapabilityOwner(), "chat") {
		t.Fatal(c)
	}
}
func TestPhase4bNodeDenialBeforeInstall(t *testing.T) {
	isolatePhase4b(t)
	savePolicyRules(t, policy.Rule{ID: "deny-install", Scope: "machine", ScopeID: "local", Subject: "node.install", Decision: policy.Deny})
	called := false
	if err := startLcJob("install", func() { called = true }); err == nil || called {
		t.Fatal("installer started despite denial", err)
	}
	v := &vllmState{job: "install"}
	if err := v.installOrUpdate(false); err == nil || v.job != "" || v.err == "" {
		t.Fatal("vLLM policy denial not settled", err)
	}
	if _, err := os.Stat(vllmDir()); !os.IsNotExist(err) {
		t.Fatal("vLLM touched its install directory before authorization", err)
	}
}
func TestPhase4bMemoryProviderSelectionDenied(t *testing.T) {
	m := isolatePhase4b(t)
	savePolicyRules(t, policy.Rule{ID: "deny-memory", Scope: "global", Subject: "memory.consolidate", Decision: policy.Deny})
	m.keys["p"] = "key"
	if err := putStoreJSON(bkProviders, "p", CloudProvider{ID: "p", Endpoint: "https://provider.test/v1"}); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/api/brain/memory/settings", strings.NewReader(`{"model":{"provider_id":"p","model":"test-model"}}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	(&brainService{}).memorySettingsHTTP(w, r)
	if !strings.Contains(w.Body.String(), "policy denied") || ReadConfig()["brain.consolidation_consent"] != "" {
		t.Fatal("denied destination was saved", w.Body.String())
	}
}
func TestPhase4bDegradedConfigOptionsPreserveDiscovery(t *testing.T) {
	isolatePhase4b(t)
	f := agent.HarnessFeatures{Models: true, Effort: true, ConfigOptions: []string{"model", "reasoning_effort"}}
	d := RuntimeDescriptor{ID: "fixture", Kind: "harness", Capabilities: []string{"models", "reasoning-effort"}, Features: &f}
	observeCapability("agent:fixture", "models", false, "catalog_failed")
	if got := degradedDescriptor(d); got.Features.Models || len(got.Features.ConfigOptions) != 1 || got.Features.ConfigOptions[0] != "reasoning_effort" {
		t.Fatal(got)
	}
	observeCapability("agent:fixture", "models", true, "probe_succeeded")
	if got := degradedDescriptor(d); !got.Features.Models || len(got.Features.ConfigOptions) != 2 || got.Features.ConfigOptions[0] != "model" {
		t.Fatal("degradation mutated the native discovery options", got.Features)
	}
}
func TestPhase4bEngineConfirmationBindsDestination(t *testing.T) {
	m := isolatePhase4b(t)
	oldNode, oldClient := currentEngineNode(), http.DefaultClient
	t.Cleanup(func() { _ = setEngineNode(oldNode); http.DefaultClient = oldClient })
	original := &engineNode{Direct: true, V1: "https://approved.test", APIKey: "approved-key", Model: "approved-model"}
	if err := setEngineNode(original); err != nil {
		t.Fatal(err)
	}
	savePolicyRules(t, policy.Rule{ID: "confirm-engine", Scope: "global", Subject: "data.send_provider", Decision: policy.Confirm})
	type destination struct{ host, key, model string }
	sent := make(chan destination, 1)
	http.DefaultClient = &http.Client{Transport: phase4Transport{http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		sent <- destination{r.URL.Host, r.Header.Get("Authorization"), body.Model}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	})}}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := runChat(ctx, []Message{{Role: "user", Content: "private discussion"}}, 0.5, Caps{}, func(StreamEvent) bool { return true })
		done <- err
	}()
	var pending RuntimeSession
	for ctx.Err() == nil {
		for _, task := range m.tasks(time.Now()) {
			s, _ := m.get(task.DiscussionID)
			if len(s.PendingRequests) > 0 {
				pending = s
				break
			}
		}
		if pending.ID != "" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if pending.ID == "" {
		t.Fatal("no destination confirmation")
	}
	if err := setEngineNode(&engineNode{Direct: true, V1: "https://different.test", APIKey: "different-key", Model: "different-model"}); err != nil {
		t.Fatal(err)
	}
	if err := m.answerRequest(pending.ID, pending.PendingRequests[0].ID, agent.RequestAnswer{Decision: "allow_once"}); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got := <-sent; got != (destination{"approved.test", "Bearer approved-key", "approved-model"}) {
		t.Fatal("confirmation redirected the transcript or mixed credentials", got)
	}
}
func TestPhase4bNotificationSummaryWaitsForConfirmation(t *testing.T) {
	m := isolatePhase4b(t)
	savePolicyRules(t, policy.Rule{ID: "summary", Scope: "global", Subject: "notification.summary", Decision: policy.Confirm})
	oldClient := notificationClient
	t.Cleanup(func() { notificationClient = oldClient })
	sent := make(chan string, 1)
	notificationClient = &http.Client{Transport: phase4Transport{http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		sent <- string(body)
		w.WriteHeader(200)
	})}}
	cfg := notify.Default()
	cfg.Rules.IncludeSummaries = true
	cfg.Ntfy.Enabled, cfg.Ntfy.ServerURL, cfg.Ntfy.Topic = true, "https://notify.test", "topic"
	done := make(chan error, 1)
	go func() {
		done <- m.notifications(nil).deliver(t.Context(), cfg, events.Event{Type: events.TaskCompleted, Status: "completed", Summary: "PRIVATE_SUMMARY"}, "")
	}()
	deadline := time.Now().Add(time.Second)
	var session RuntimeSession
	for time.Now().Before(deadline) {
		for _, task := range m.tasks(time.Now()) {
			if task.Status == "waiting_approval" {
				session, _ = m.get(task.DiscussionID)
			}
		}
		if len(session.PendingRequests) > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(session.PendingRequests) == 0 {
		t.Fatal("summary did not raise a request")
	}
	select {
	case <-sent:
		t.Fatal("summary sent before confirmation")
	default:
	}
	if err := m.answerRequest(session.ID, session.PendingRequests[0].ID, agent.RequestAnswer{Decision: "allow_once"}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("notification did not resume")
	}
	if body := <-sent; !strings.Contains(body, "PRIVATE_SUMMARY") {
		t.Fatal(body)
	}
}
func savePolicyRules(t *testing.T, rules ...policy.Rule) {
	t.Helper()
	if err := putStoreJSON(bkState, policyStateKey, policy.Document{Version: 1, Migrated: true, Rules: rules}); err != nil {
		t.Fatal(err)
	}
}
func TestPhase4bMigrationAndAPI(t *testing.T) {
	m := isolatePhase4b(t)
	s, err := m.create("", "", false)
	if err != nil {
		t.Fatal(err)
	}
	s.RuntimeID, s.ProviderID, s.Endpoint, s.Model, s.Permission = "openai-compatible", "provider", "https://provider.test/v1", "model", "edits"
	if err = putStoreJSON(bkRuntimeSessions, s.ID, s); err != nil {
		t.Fatal(err)
	}
	d, err := policyDocument()
	if err != nil || !d.Migrated {
		t.Fatal(d, err)
	}
	for _, test := range []struct {
		permission, subject string
		want                policy.Decision
	}{{"edits", "agent.file_write", policy.Allow}, {"edits", "agent.command", policy.Confirm}, {"full", "agent.command", policy.Allow}, {"ask", "agent.file_write", policy.Confirm}} {
		in := policy.Input{Subject: test.subject, LegacyPermission: test.permission, Fallback: policy.Confirm}
		if got := policy.Evaluate(d.Rules, in); got.Decision != test.want {
			t.Fatal(test, got)
		}
	}
	in := sessionPolicyInput(s, "data.send_provider", policy.Allow)
	if got := policy.Evaluate(d.Rules, in); got.Decision != policy.Allow {
		t.Fatal(got)
	}
	if got := policy.Evaluate(d.Rules, sessionPolicyInput(s, "data.send_provider", policy.Confirm)); got.Decision != policy.Confirm {
		t.Fatal("migrated route removed fresh selection consent", got)
	}
	in.Endpoint = "https://different.test/v1"
	in.Fallback = policy.Confirm
	if got := policy.Evaluate(d.Rules, in); got.Decision != policy.Confirm {
		t.Fatal(got)
	}
	if err := putStoreJSON(bkRuntimeSessions, "later", RuntimeSession{RuntimeID: "new-agent"}); err != nil {
		t.Fatal(err)
	}
	again, err := policyDocument()
	if err != nil || len(again.Rules) != len(d.Rules) {
		t.Fatal("migration ran twice")
	}
	w := httptest.NewRecorder()
	handlePolicy(w, httptest.NewRequest("GET", "/api/policy", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"version":1`) {
		t.Fatal(w.Body.String())
	}
	w = httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/policy/evaluate", strings.NewReader(`{"subject":"agent.command","command":"PRIVATE_PROMPT"}`))
	req.Header.Set("Content-Type", "application/json")
	handlePolicyEvaluate(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"decision":"confirm"`) {
		t.Fatal(w.Body.String())
	}
	w = httptest.NewRecorder()
	handlePolicyAudit(w, httptest.NewRequest("GET", "/api/policy/audit", nil))
	if strings.Contains(w.Body.String(), "PRIVATE_PROMPT") || !strings.Contains(w.Body.String(), `"dry_run":true`) {
		t.Fatal(w.Body.String())
	}
	if err := putBytes(bkState, policyStateKey, []byte("corrupt")); err != nil {
		t.Fatal(err)
	}
	if evaluatePolicy(policy.Input{Subject: "agent.command", Fallback: policy.Allow}, false).Decision != policy.Deny {
		t.Fatal("corrupt policy failed open")
	}
}
func TestPhase4bConfirmACPAnsweredFromPhone(t *testing.T) {
	m := isolatePhase4b(t)
	a := fakeACPAdapter(t)
	isolateRuntimeRegistry(t, a)
	s := createACPSession(t, m, a, "full")
	savePolicyRules(t, policy.Rule{ID: "confirm-write", Scope: "agent", ScopeID: a.agent.ID, Subject: "agent.file_write", Decision: policy.Confirm})
	if err := m.start(s.ID, "policy-confirm-turn", "PRIVATE_PROMPT"); err != nil {
		t.Fatal(err)
	}
	answered := map[string]bool{}
	count := 0
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		current, _ := m.get(s.ID)
		if current.Status != "running" {
			if current.Status != "complete" {
				t.Fatal(current.Error)
			}
			break
		}
		for _, req := range current.PendingRequests {
			if answered[req.ID] {
				continue
			}
			answered[req.ID] = true
			count++
			cfg := notify.Default()
			cfg.PublicBaseURL = "https://loom.test"
			svc := m.notifications(nil)
			msg := svc.message(cfg, events.Event{Type: events.TaskWaiting, DiscussionID: s.ID, RequestID: req.ID, Status: "waiting_approval"})
			if len(msg.Actions) == 0 {
				t.Fatal("missing phone answer")
			}
			u, _ := url.Parse(msg.Actions[0].URL)
			answer := httptest.NewRequest("POST", msg.Actions[0].URL, nil)
			answer.SetPathValue("token", strings.TrimPrefix(u.Path, "/api/notify/answer/"))
			w := httptest.NewRecorder()
			svc.handleAnswer(w, answer)
			if w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	finished := waitACPTurn(t, m, s.ID)
	if count == 0 || len(finished.PendingRequests) != 0 {
		t.Fatal("policy did not confirm")
	}
	if data, err := os.ReadFile(filepath.Join(s.Workdir, "loom-fake-acp.txt")); err != nil || string(data) != "fixture\n" {
		t.Fatal("approved write missing", err)
	}
	raw, _ := json.Marshal(policyAudit.Snapshot())
	if bytes.Contains(raw, []byte("PRIVATE_PROMPT")) || bytes.Contains(raw, []byte(s.Workdir)) {
		t.Fatal("audit leaked context")
	}
}
func TestPhase4bDenyOverridesFullACP(t *testing.T) {
	m := isolatePhase4b(t)
	a := fakeACPAdapter(t)
	isolateRuntimeRegistry(t, a)
	s := createACPSession(t, m, a, "full")
	savePolicyRules(t, policy.Rule{ID: "deny-write", Scope: "global", Subject: "agent.file_write", Decision: policy.Deny})
	if err := m.start(s.ID, "policy-deny-turn", "fixture"); err != nil {
		t.Fatal(err)
	}
	waitACPTurn(t, m, s.ID)
	if _, err := os.Stat(filepath.Join(s.Workdir, "loom-fake-acp.txt")); !os.IsNotExist(err) {
		t.Fatal("denied tool wrote a file", err)
	}
}
func TestPhase4bStandaloneCanonicalTask(t *testing.T) {
	m := isolatePhase4b(t)
	savePolicyRules(t, policy.Rule{ID: "terminal-confirm", Scope: "machine", ScopeID: "local", Subject: "node.terminal", Decision: policy.Confirm})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- m.authorizePolicy(ctx, policy.Input{Subject: "node.terminal", MachineID: "local", Command: "PRIVATE_COMMAND", Fallback: policy.Allow}, false)
	}()
	var pending RuntimeSession
	for ctx.Err() == nil {
		for _, task := range m.tasks(time.Now()) {
			s, _ := m.get(task.DiscussionID)
			if len(s.PendingRequests) > 0 {
				pending = s
				break
			}
		}
		if pending.ID != "" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if pending.ID == "" {
		t.Fatal("no canonical policy task")
	}
	if len(m.tasks(time.Now())) != 1 || m.tasks(time.Now())[0].Status != "waiting_approval" {
		t.Fatal(m.tasks(time.Now()))
	}
	if err := m.answerRequest(pending.ID, pending.PendingRequests[0].ID, agent.RequestAnswer{Decision: "allow_once"}); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if task := m.tasks(time.Now())[0]; task.Status != "done" {
		t.Fatal(task)
	}
}
func TestPhase4bDoctorFakesAndDegradedRecovery(t *testing.T) {
	m := isolatePhase4b(t)
	a := fakeACPAdapter(t)
	a.agent.Custom = true
	isolateRuntimeRegistry(t, a)
	oldClient := nodeClient
	t.Cleanup(func() { nodeClient = oldClient })
	online := true
	nodeClient = &http.Client{Transport: phase4Transport{http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !online {
			w.WriteHeader(503)
			return
		}
		if r.URL.Path == "/api/node/info" {
			sendJSON(w, 200, map[string]any{"ok": true, "role": "engine-node", "version": Version, "id": "node", "handshake": nodeHandshake, "modules": []string{"terminal", "engine"}})
			return
		}
		sendJSON(w, 200, map[string]any{"status": "ok"})
	})}}
	machine := RemoteMachine{ID: "doctor-machine", NodeID: "node", Modules: []string{"engine", "terminal"}}
	if err := putStoreJSON(bkState, remoteMachinesState, []RemoteMachine{machine}); err != nil {
		t.Fatal(err)
	}
	if err := putStoreJSON(bkState, machineNodePrefix+machine.ID, engineNode{URL: "https://node.test", WebKey: "secret", Role: "engine-node"}); err != nil {
		t.Fatal(err)
	}
	p := acpProbe{At: time.Now().UnixMilli(), Error: "PRIVATE_FAILED_PROBE", Compatibility: &runtimeCompatibilityRecord{Runtime: a.agent.ID, Version: "new", Warning: "untested"}}
	if err := putStoreJSON(bkState, acpProbeKey+a.agent.ID, p); err != nil {
		t.Fatal(err)
	}
	report, err := runDoctor(context.Background(), "agent."+a.agent.ID)
	if err != nil || len(report.Checks) != 1 || report.Checks[0].Detail != "handshake_failed" {
		t.Fatal(report, err)
	}
	if !capabilityDisabled("agent:"+a.agent.ID, "resume") {
		t.Fatal("failed probe not projected")
	}
	if !hasRuntimeCapability(a.Descriptor(), "chat") || hasRuntimeCapability(a.Descriptor(), "resume") {
		t.Fatal("whole agent degraded or resume retained")
	}
	online = false
	report, err = runDoctor(context.Background(), "machine."+machine.ID)
	if err != nil || report.Checks[0].Status != "fail" || !capabilityDisabled("machine:"+machine.ID, "terminal") {
		t.Fatal(report, err)
	}
	online = true
	report, err = runDoctor(context.Background(), "machine."+machine.ID)
	if err != nil || report.Checks[0].Status != "ok" || capabilityDisabled("machine:"+machine.ID, "terminal") {
		t.Fatal(report, err)
	}
	p.Error = ""
	p.CapabilityChecks = []capability.Probe{{Capability: "models", OK: false, Reason: "catalog_incompatible"}}
	observeAgentProbe(a.agent.ID, p)
	if capabilityDisabled("agent:"+a.agent.ID, "resume") || !capabilityDisabled("agent:"+a.agent.ID, "models") {
		t.Fatal("feature recovery failed")
	}
	before, _, _ := m.events.Snapshot(0)
	observeAgentProbe(a.agent.ID, p)
	after, _, _ := m.events.Snapshot(0)
	if len(before) != len(after) {
		t.Fatal("repeated failed probe churned events")
	}
	p.CapabilityChecks = nil
	observeAgentProbe(a.agent.ID, p)
	p.Error = "catalog request failed"
	p.CapabilityChecks = []capability.Probe{{Capability: "models", OK: false, Reason: "catalog_probe_failed"}}
	observeAgentProbe(a.agent.ID, p)
	if capabilityDisabled("agent:"+a.agent.ID, "resume") || !capabilityDisabled("agent:"+a.agent.ID, "models") {
		t.Fatal("catalog-only failure disabled another feature")
	}
	p.Error, p.CapabilityChecks = "", nil
	observeAgentProbe(a.agent.ID, p)
	list, _, _ := m.events.Snapshot(0)
	restored := false
	for _, e := range list {
		restored = restored || e.Type == events.CapabilityRestored
	}
	if !restored {
		t.Fatal("no restoration event")
	}
	if notify.Default().Rules.Allows(events.Event{Type: events.CapabilityDegraded}, time.Now()) {
		t.Fatal("degraded notifications on by default")
	}
}
func TestPhase4bBundleBoundaryAndRedaction(t *testing.T) {
	m := isolatePhase4b(t)
	a := fakeACPAdapter(t)
	isolateRuntimeRegistry(t, a)
	secret := "KNOWN_BUNDLE_SECRET_123"
	m.keys["provider"] = secret
	if err := SetConfigKey("API_KEY", secret); err != nil {
		t.Fatal(err)
	}
	if err := putStoreJSON(bkState, "agent_compat_"+a.agent.ID, runtimeCompatibilityRecord{Runtime: a.agent.ID, Version: secret, Executable: "/home/private/bin/agent", Warning: "PRIVATE_PROMPT"}); err != nil {
		t.Fatal(err)
	}
	if err := putStoreJSON(bkState, acpProbeKey+a.agent.ID, acpProbe{At: time.Now().UnixMilli(), Error: "PRIVATE_PROMPT " + secret}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(LoomHome(), serviceName()+".log"), []byte("PRIVATE_DISCUSSION "+secret+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	m.events.Publish(events.Event{Type: events.TaskFailed, Title: "PRIVATE_TITLE", Summary: "PRIVATE_PROMPT"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/doctor/bundle", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	handleDoctorBundle(w, req)
	if w.Code != 200 || w.Header().Get("Content-Type") != "application/zip" {
		t.Fatal(w.Code, w.Body.String())
	}
	reader, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range reader.File {
		f, _ := file.Open()
		data, _ := io.ReadAll(f)
		f.Close()
		for _, forbidden := range []string{secret, "PRIVATE_PROMPT", "PRIVATE_DISCUSSION", "PRIVATE_TITLE", "/home/private"} {
			if bytes.Contains(data, []byte(forbidden)) {
				t.Fatalf("%s leaked %s", file.Name, forbidden)
			}
		}
	}
}
