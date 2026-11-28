package loom

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/policy"
	agent "github.com/lucas-lepajollec/loom/internal/loom/runtime"
	"github.com/lucas-lepajollec/loom/internal/loom/tools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func onDemandFixture(t testing.TB, calls *atomic.Int32) gatewayConfigFixture {
	t.Helper()
	config := gatewayConfigFixture{
		"home":    {Command: "fixture", Enabled: true, Exposure: "on_demand", DisabledTools: []string{"hidden"}},
		"proxmox": {Command: "fixture", Enabled: true, Exposure: "on_demand"},
		"direct":  {Command: "fixture", Enabled: true, Exposure: "direct"},
		"off":     {Command: "fixture", Exposure: "on_demand"},
	}
	old := mcpMgr
	pool := tools.NewMCPManager(config, func(ctx context.Context, serverName string, _ MCPServerConfig) (*mcp.ClientSession, error) {
		s := mcp.NewServer(&mcp.Implementation{Name: "source"}, nil)
		add := func(name, desc string, schema map[string]any) {
			s.AddTool(&mcp.Tool{Name: name, Description: desc, InputSchema: schema, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}},
				func(_ context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
					calls.Add(1)
					return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(r.Params.Arguments)}, &mcp.ImageContent{MIMEType: "image/png", Data: []byte{1}}}}, nil
				})
		}
		empty := map[string]any{"type": "object"}
		switch serverName {
		case "home":
			add("light_level", "Read the light level. "+strings.Repeat("More details. ", 40), map[string]any{
				"type": "object", "properties": map[string]any{
					"room_id":    map[string]any{"type": "string", "description": "A room"},
					"brightness": map[string]any{"type": "number"},
				}, "required": []string{"room_id"},
			})
			add("list_devices", "List a light and other devices", empty)
			add("hidden", "Never exposed", empty)
		case "proxmox":
			add("inspect", "Inspect VMs", map[string]any{
				"type": "object", "properties": map[string]any{"cluster_id": map[string]any{"type": "string"}},
			})
		case "direct":
			add("echo", "Legacy tool", empty)
		default:
			t.Errorf("disabled upstream connected: %s", serverName)
		}
		st, ct := mcp.NewInMemoryTransports()
		peer, err := s.Connect(ctx, st, nil)
		if err != nil {
			return nil, err
		}
		t.Cleanup(func() { _ = peer.Close() })
		return mcp.NewClient(&mcp.Implementation{Name: "fixture-client"}, nil).Connect(ctx, ct, nil)
	})
	pool.EnsureAll(config)
	mcpMgr = &mcpManager{pool}
	t.Cleanup(func() {
		pool.CloseAll()
		mcpMgr = old
	})
	return config
}

func connectOnDemandGateway(t *testing.T) *mcp.ClientSession {
	t.Helper()
	mux := http.NewServeMux()
	registerBrainRoutes(mux, nil)
	token, err := rotateGatewayToken()
	if err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "on-demand-client"}, nil)
	s, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{
		Endpoint: "http://localhost/mcp/loom", HTTPClient: &http.Client{Transport: brainMuxTransport{mux, token}},
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func callMCPFixture(ctx context.Context, s *mcp.ClientSession, name, args string) (*mcp.CallToolResult, error) {
	return s.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: json.RawMessage(args)})
}

func TestOnDemandDiscoveryAndDiscussion(t *testing.T) {
	gatewayTestHome(t)
	var calls atomic.Int32
	onDemandFixture(t, &calls)
	s := connectOnDemandGateway(t)
	list, err := s.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	names := make(map[string]*mcp.Tool)
	for _, tool := range list.Tools {
		names[tool.Name] = tool
	}
	for _, tool := range []string{mcpSearchTool, mcpDescribeTool, mcpCallTool, mcpRegisterTool, "direct__echo", "loom_gateway_status"} {
		if names[tool] == nil {
			t.Fatalf("missing %s", tool)
		}
	}
	if names["home__light_level"] != nil || !names[mcpSearchTool].Annotations.ReadOnlyHint ||
		!names[mcpDescribeTool].Annotations.ReadOnlyHint || names[mcpCallTool].Annotations != nil {
		t.Fatal("unexpected discovery schemas")
	}
	if note := s.InitializeResult().Instructions; !strings.Contains(note, "home (2 tools), proxmox (1 tools)") {
		t.Fatal(note)
	}
	if note := baseSystemPrompt(Caps{MCP: true, Mem: MemOff}); !strings.Contains(note, "loom_mcp_search") {
		t.Fatal(note)
	}
	for _, defs := range [][]Tool{mcpTools(), (cloudTools{mcp: true}).Definitions().([]Tool)} {
		if len(defs) != 5 || defs[0].Function.Name != "mcp__direct__echo" || defs[1].Function.Name != mcpSearchTool {
			t.Fatal(defs)
		}
	}
	res, err := callMCPFixture(t.Context(), s, mcpSearchTool, `{"query":"LIGHT"}`)
	if err != nil || res.IsError {
		t.Fatal(res, err)
	}
	var result mcpSearchResult
	if err := json.Unmarshal([]byte(res.Content[0].(*mcp.TextContent).Text), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Matches) != 2 || result.TotalAvailable != 3 || len(result.Servers) != 2 {
		t.Fatal(result)
	}
	best := result.Matches[0]
	if best.Tool != "light_level" || !slices.Equal(best.Required, []string{"room_id"}) ||
		!slices.Equal(best.Args, []string{"brightness", "room_id"}) || len([]rune(best.Description)) != 160 {
		t.Fatal(best)
	}
	hub := externalMCPHub()
	prop := hub.search(mcpSearchArgs{Query: "cluster"})
	if len(prop.Matches) != 1 || prop.Matches[0].Tool != "inspect" {
		t.Fatal(prop)
	}
	wild := hub.search(mcpSearchArgs{Query: "*", Server: "HOME", Limit: 1})
	if len(wild.Matches) != 1 || wild.TotalAvailable != 2 || len(wild.Servers) != 1 {
		t.Fatal(wild)
	}
	empty := hub.search(mcpSearchArgs{Query: "hidden"})
	if len(empty.Matches) != 0 {
		t.Fatal(empty)
	}

	const target = `{"server":"home","tool":"light_level"}`
	res, err = callMCPFixture(t.Context(), s, mcpDescribeTool, target)
	if err != nil || res.IsError {
		t.Fatal(res, err)
	}
	var desc mcpDescription
	if err := json.Unmarshal([]byte(res.Content[0].(*mcp.TextContent).Text), &desc); err != nil {
		t.Fatal(err)
	}
	if len(desc.Description) <= 160 || desc.InputSchema.(map[string]any)["type"] != "object" || !desc.Annotations.ReadOnlyHint {
		t.Fatal(desc)
	}
	const upstream = `{"room_id":"living room","brightness":9007199254740993}`
	const call = `{"server":"home","tool":"light_level","arguments":` + upstream + `}`
	res, err = callMCPFixture(t.Context(), s, mcpCallTool, call)
	if err != nil || res.IsError || res.Content[0].(*mcp.TextContent).Text != upstream || len(res.Content[1].(*mcp.ImageContent).Data) != 1 {
		t.Fatal(res, err)
	}
	if got := discussionMCPResult(t.Context(), mcpCallTool, json.RawMessage(call)); !strings.Contains(got, upstream) {
		t.Fatal(got)
	}
	if got, err := (cloudTools{mcp: true}).ExecuteJSON(t.Context(), mcpCallTool, json.RawMessage(call)); err != nil || !strings.Contains(got, upstream) {
		t.Fatal(got, err)
	}
	if got, err := (cloudTools{mcp: true}).Execute(t.Context(), mcpDescribeTool, map[string]any{"server": "home", "tool": "light_level"}); err != nil || !strings.Contains(got, "room_id") {
		t.Fatal(got, err)
	}
	for _, bad := range []mcpTarget{{"home", "hidden"}, {"off", "light_level"}, {"direct", "echo"}, {"home", "unknown"}} {
		for _, name := range []string{mcpDescribeTool, mcpCallTool} {
			raw, _ := json.Marshal(mcpCallArgs{mcpTarget: bad, Arguments: json.RawMessage(`{}`)})
			res, err := callMCPFixture(t.Context(), s, name, string(raw))
			if err == nil && !res.IsError {
				t.Fatalf("%s reached unavailable %v", name, bad)
			}
		}
	}
	if calls.Load() != 3 {
		t.Fatal("unexpected upstream calls", calls.Load())
	}
}

func waitMCPApproval(t *testing.T, sessions *runtimeSessions, subject string) pendingPolicyRequest {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, p := range sessions.pendingPolicyRequests() {
			if p.Subject == subject {
				return p
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("approval request for %s did not open", subject)
	return pendingPolicyRequest{}
}

func TestOnDemandCallRequiresUpstreamPolicyApproval(t *testing.T) {
	sessions := isolatePhase4b(t)
	var calls atomic.Int32
	onDemandFixture(t, &calls)
	s := connectOnDemandGateway(t)
	const subject = "mcp.tool:home/light_level"
	savePolicyRules(t, policy.Rule{ID: "ask-light", Scope: "global", Subject: subject, Decision: policy.Confirm})
	const args = `{"server":"home","tool":"light_level","arguments":{"room_id":"kitchen"}}`

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	for _, answer := range []string{"deny", "allow_once"} {
		done := make(chan bool, 1)
		go func() {
			r, err := callMCPFixture(ctx, s, mcpCallTool, args)
			done <- err == nil && !r.IsError
		}()
		p := waitMCPApproval(t, sessions, subject)
		if p.Detail != `{"room_id":"kitchen"}` || calls.Load() != 0 {
			t.Fatal(p, calls.Load())
		}
		if err := sessions.answerRequest(p.SessionID, p.RequestID, agent.RequestAnswer{Decision: answer}); err != nil {
			t.Fatal(err)
		}
		if <-done != (answer == "allow_once") {
			t.Fatal("unexpected gateway decision", answer)
		}
	}
	if calls.Load() != 1 {
		t.Fatal(calls.Load())
	}

	done := make(chan string, 1)
	go func() { done <- discussionMCPResult(ctx, mcpCallTool, json.RawMessage(args)) }()
	p := waitMCPApproval(t, sessions, subject)
	if err := sessions.answerRequest(p.SessionID, p.RequestID, agent.RequestAnswer{Decision: "deny"}); err != nil {
		t.Fatal(err)
	}
	if result := <-done; !strings.Contains(result, "declined") || calls.Load() != 1 {
		t.Fatal(result, calls.Load())
	}
	savePolicyRules(t, policy.Rule{ID: "deny-light", Scope: "global", Subject: subject, Decision: policy.Deny})
	result, err := (cloudTools{mcp: true}).Execute(ctx, mcpCallTool, map[string]any{
		"server": "home", "tool": "light_level", "arguments": map[string]any{"room_id": "kitchen"},
	})
	if err != nil || !strings.Contains(result, "denied capability: "+subject) || calls.Load() != 1 {
		t.Fatal(result, err, calls.Load())
	}
}

func TestOnDemandRegisterRequiresApproval(t *testing.T) {
	sessions := isolatePhase4b(t)
	s := connectOnDemandGateway(t)
	const args = `{"name":"custom","transport":"stdio","command":"fixture-mcp-binary","args":["--mode","fixture"],"description":"Example","env_names":["API_KEY"]}`
	for _, invalid := range []string{
		`{"name":"bad__name","transport":"stdio","command":"fixture"}`,
		`{"name":"secret","transport":"stdio","command":"fixture","env":{"TOKEN":"private"}}`,
		`{"name":"secret","transport":"stdio","command":"fixture","env_names":["TOKEN=private"]}`,
		`{"name":"secret","transport":"http","url":"https://user:private@example.test/mcp"}`,
	} {
		if r, err := callMCPFixture(t.Context(), s, mcpRegisterTool, invalid); err == nil && !r.IsError {
			t.Fatal("invalid proposal accepted")
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	for _, answer := range []string{"deny", "allow_once"} {
		if answer == "allow_once" {
			savePolicyRules(t, policy.Rule{ID: "allow-management", Scope: "global", Subject: "mcp.manage", Decision: policy.Allow})
		}
		done := make(chan bool, 1)
		go func() {
			r, err := callMCPFixture(ctx, s, mcpRegisterTool, args)
			done <- err == nil && !r.IsError
		}()
		p := waitMCPApproval(t, sessions, "mcp.manage")
		if !strings.Contains(p.Detail, `["--mode","fixture"]`) {
			t.Fatal(p)
		}
		cfg, err := LoadMCPConfig()
		if err != nil || len(cfg) != 0 {
			t.Fatal("proposal persisted before approval", cfg, err)
		}
		if err := sessions.answerRequest(p.SessionID, p.RequestID, agent.RequestAnswer{Decision: answer}); err != nil {
			t.Fatal(err)
		}
		if <-done != (answer == "allow_once") {
			t.Fatal("unexpected registration decision", answer)
		}
	}
	cfg, err := LoadMCPConfig()
	custom := cfg["custom"]
	value, present := custom.Env["API_KEY"]
	if err != nil || !custom.Enabled || custom.Exposure != "on_demand" || value != "" || !present || custom.Description != "Example" {
		t.Fatal(cfg, err)
	}
	if info, err := os.Stat(mcpFilePath()); err != nil || runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatal(info, err)
	}
	savePolicyRules(t, policy.Rule{ID: "deny-management", Scope: "global", Subject: "mcp.manage", Decision: policy.Deny})
	if _, err := registerProposedMCP(ctx, json.RawMessage(args)); err == nil || !strings.Contains(err.Error(), "denied") {
		t.Fatal(err)
	}
	if err := setMCPServer("custom", MCPServerConfig{Command: "replacement"}, mcpSaveOptions{createOnly: true}); err != errMCPAdoptConflict {
		t.Fatal("registration can overwrite", err)
	}
}

func TestOnDemandBudgetAndMode(t *testing.T) {
	gatewayTestHome(t)
	var calls atomic.Int32
	config := onDemandFixture(t, &calls)
	config["home"] = MCPServerConfig{Command: "fixture", Enabled: true, DisabledTools: []string{"hidden"}}
	for _, mode := range []string{"direct", "smart"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/mcp/gateway/mode", strings.NewReader(fmt.Sprintf(`{"tool_mode":%q}`, mode)))
		req.Header.Set("Content-Type", "application/json")
		handleMCPGatewayMode(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatal(rec.Code, rec.Body.String())
		}
		mcpMgr.EnsureAll(config)
		hub := externalMCPHub()
		if (len(hub.lazy) == 3) != (mode == "smart") {
			t.Fatal(mode, hub)
		}
	}
	servers := []MCPServerStatus{
		{Name: "lazy", Enabled: true, Exposure: "on_demand"},
		{Name: "direct", Enabled: true, Exposure: "direct"},
	}
	items := []tools.MCPGatewayTool{
		{Server: "lazy", Tool: &mcp.Tool{Name: "long", Description: strings.Repeat("large schema", 800)}},
		{Server: "direct", Tool: &mcp.Tool{Name: "short", Description: "a"}},
	}
	first, _ := json.Marshal(items[0].Tool)
	second, _ := json.Marshal(items[1].Tool)
	items[0].SchemaBytes, items[1].SchemaBytes = len(first), len(second)
	budget := estimateMCPBudget(servers, items, "direct")
	if budget.OnDemandTools != 1 || budget.DirectTools != 1 || budget.DirectTokens != (len(first)+len(second))/4 ||
		budget.InjectedTokens != len(second)/4+mcpDiscoveryTokens || budget.SavedTokens != budget.DirectTokens-budget.InjectedTokens ||
		budget.SavedPercent != budget.SavedTokens*100/budget.DirectTokens {
		t.Fatal(budget)
	}
	if got := estimateMCPBudget(servers, items[1:], "smart"); got.SavedTokens != 0 || got.SavedPercent != 0 {
		t.Fatal(got)
	}
	rec := httptest.NewRecorder()
	handleMCP(rec, httptest.NewRequest(http.MethodGet, "/api/mcp", nil))
	var snapshot struct {
		Mode   string         `json:"tool_mode"`
		Budget mcpTokenBudget `json:"token_budget"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &snapshot); err != nil || snapshot.Mode != "smart" ||
		snapshot.Budget.OnDemandTools != 3 || snapshot.Budget.DirectTools != 1 {
		t.Fatal(rec.Body.String(), err)
	}
}

func TestMCPExplicitSSEConnectionSurvivesDiscoveryTimeoutContext(t *testing.T) {
	srv := mcp.NewServer(&mcp.Implementation{Name: "SSE fixture"}, nil)
	srv.AddTool(&mcp.Tool{Name: "echo", InputSchema: map[string]any{"type": "object"}},
		func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(req.Params.Arguments)}}}, nil
		})
	handler := mcp.NewSSEHandler(func(*http.Request) *mcp.Server { return srv }, nil)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer synthetic" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	defer s.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	session, err := mcpConnect(ctx, "sse", MCPServerConfig{URL: s.URL, Type: "sse", Headers: map[string]string{"Authorization": "Bearer synthetic"}})
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	callCtx, stop := context.WithTimeout(t.Context(), 3*time.Second)
	defer stop()
	result, err := callMCPFixture(callCtx, session, "echo", `{"value":42}`)
	if err != nil || result.IsError || result.Content[0].(*mcp.TextContent).Text != `{"value":42}` {
		t.Fatal(result, err)
	}
}

func TestMCPSavePreservesExistingSettingsAndDefaultsNewServer(t *testing.T) {
	gatewayTestHome(t)
	old := MCPServerConfig{
		Command: "fixture", Args: []string{"a value"}, Env: map[string]string{"TOKEN": "synthetic"},
		DisabledTools: []string{"hidden"}, Description: "Existing server",
	}
	if err := SetMCPServer("existing", old); err != nil {
		t.Fatal(err)
	}
	save := func(raw string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		handleMCPSave(rec, httptest.NewRequest(http.MethodPost, "/api/mcp/save", strings.NewReader(raw)))
		return rec
	}
	for _, req := range []string{
		`{"name":"existing","exposure":"on_demand","exposure_only":true}`,
		`{"name":"new","command":"fixture","enabled":false,"create_only":true}`,
	} {
		if res := save(req); res.Code != http.StatusOK {
			t.Fatal(res.Code, res.Body.String())
		}
	}
	cfg, err := LoadMCPConfig()
	if err != nil || cfg["new"].Exposure != "on_demand" || cfg["existing"].Exposure != "on_demand" ||
		cfg["existing"].Env["TOKEN"] != "synthetic" || !cfg["existing"].ToolDisabled("hidden") ||
		cfg["existing"].Enabled || cfg["existing"].Description != old.Description || !slices.Equal(cfg["existing"].Args, old.Args) {
		t.Fatal(cfg, err)
	}
	if res := save(`{"name":"existing","command":"replacement","create_only":true}`); res.Code != http.StatusConflict {
		t.Fatal(res.Code)
	}
	if res := save(`{"name":"existing","exposure":"","exposure_only":true}`); res.Code != http.StatusOK {
		t.Fatal(res.Code)
	}
	cfg, err = LoadMCPConfig()
	if err != nil || cfg["existing"].Exposure != "" || cfg["existing"].Command != old.Command {
		t.Fatal(cfg, err)
	}
}

func TestOnDemandCachedParallelDiscovery(t *testing.T) {
	gatewayTestHome(t)
	cfg := MCPServerConfig{Command: "fixture", Exposure: "on_demand", Enabled: true, Env: map[string]string{"KEY": "synthetic-credential"}}
	for _, name := range []string{"one", "two"} {
		if err := SetMCPServer(name, cfg); err != nil {
			t.Fatal(err)
		}
	}
	source := nativeMCPConfig{}
	// Seed through successful upstream discovery, then construct a new pool to
	// exercise a restart with both configured servers unavailable.
	upstream := mcp.NewServer(&mcp.Implementation{Name: "fixture"}, nil)
	upstream.AddTool(&mcp.Tool{Name: "cached", Description: "discoverable", InputSchema: map[string]any{"type": "object"}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return textResult([]byte("ok"), false), nil
	})
	first := tools.NewMCPManager(source, func(ctx context.Context, _ string, _ MCPServerConfig) (*mcp.ClientSession, error) {
		st, ct := mcp.NewInMemoryTransports()
		peer, err := upstream.Connect(ctx, st, nil)
		if err != nil {
			return nil, err
		}
		t.Cleanup(func() { _ = peer.Close() })
		return mcp.NewClient(&mcp.Implementation{Name: "fixture"}, nil).Connect(ctx, ct, nil)
	})
	servers, _ := source.LoadMCPConfig()
	first.EnsureAll(servers)
	first.CloseAll()
	if b := getBytes(catalogBucket, "one"); !strings.Contains(string(b), "discoverable") || strings.Contains(string(b), "synthetic-credential") {
		t.Fatal("incorrect snapshot")
	}
	release := make(chan struct{})
	started := make(chan string, 4)
	pool := tools.NewMCPManager(source, func(ctx context.Context, name string, _ MCPServerConfig) (*mcp.ClientSession, error) {
		started <- name
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-release:
			return nil, fmt.Errorf("redacted error")
		}
	})
	old := mcpMgr
	mcpMgr = &mcpManager{pool}
	t.Cleanup(func() { close(release); pool.CloseAll(); mcpMgr = old })
	s := connectOnDemandGateway(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	for range 2 {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("discovery serialized")
		}
	}
	list, err := s.ListTools(ctx, nil)
	if err != nil || !slices.ContainsFunc(list.Tools, func(tool *mcp.Tool) bool { return tool.Name == mcpListTool && tool.Annotations.ReadOnlyHint }) {
		t.Fatal(list, err)
	}
	res, err := callMCPFixture(ctx, s, mcpSearchTool, `{"query":"cached"}`)
	if err != nil || res.IsError || !strings.Contains(res.Content[0].(*mcp.TextContent).Text, `"total_available":2`) {
		t.Fatal(res, err)
	}
	for _, name := range []string{mcpListTool, "api"} {
		var raw string
		if name == "api" {
			rec := httptest.NewRecorder()
			handleMCP(rec, httptest.NewRequest("GET", "/api/mcp", nil))
			raw = rec.Body.String()
		} else {
			res, err := callMCPFixture(ctx, s, name, `{}`)
			if err != nil {
				t.Fatal(err)
			}
			raw = res.Content[0].(*mcp.TextContent).Text
		}
		if !strings.Contains(raw, `"cached":true`) || !strings.Contains(raw, `"env_names":["KEY"]`) || strings.Contains(raw, "synthetic-credential") {
			t.Fatal(raw)
		}
	}
	cfg.Args = []string{"new"}
	if err := SetMCPServer("one", cfg); err != nil {
		t.Fatal(err)
	}
	hub := externalMCPHub()
	if hits := hub.search(mcpSearchArgs{Query: "cached", Server: "one"}); len(hits.Matches) != 0 {
		t.Fatal(hits)
	}
}

func TestOnDemandManagementEditAndSecrets(t *testing.T) {
	sessions := isolatePhase4b(t)
	cfg := MCPServerConfig{URL: "https://example.test/mcp", Headers: map[string]string{"Authorization": "synthetic-private"}, Exposure: "direct"}
	if err := SetMCPServer("remote", cfg); err != nil {
		t.Fatal(err)
	}
	s := connectOnDemandGateway(t)
	done := make(chan bool, 1)
	update := func() {
		go func() {
			r, err := callMCPFixture(t.Context(), s, mcpRegisterTool, `{"action":"update","name":"remote","url":"https://example.test/new","disabled_tools":["write"],"exposure":"on_demand"}`)
			done <- err == nil && !r.IsError
		}()
	}
	for _, conflict := range []bool{true, false} {
		update()
		p := waitMCPApproval(t, sessions, "mcp.manage")
		if !strings.Contains(p.Detail, "https://example.test/new") || !strings.Contains(p.Detail, "Authorization") || strings.Contains(p.Detail, "synthetic-private") {
			t.Fatal(p.Detail)
		}
		if conflict {
			cfg.Description = "edited while pending"
			if err := SetMCPServer("remote", cfg); err != nil {
				t.Fatal(err)
			}
		}
		if err := sessions.answerRequest(p.SessionID, p.RequestID, agent.RequestAnswer{Decision: "allow_once"}); err != nil {
			t.Fatal(err)
		}
		if ok := <-done; ok == conflict {
			t.Fatal("revision check failed")
		}
	}
	rec := httptest.NewRecorder()
	handleMCPSave(rec, httptest.NewRequest("POST", "/api/mcp/save", strings.NewReader(`{"name":"remote","description":"owner edit","headers":{"Authorization":"","X-Mode":"new"}}`)))
	if rec.Code != 200 || strings.Contains(rec.Body.String(), "synthetic-private") {
		t.Fatal(rec.Body.String())
	}
	cfgs, err := LoadMCPConfig()
	got := cfgs["remote"]
	if err != nil || got.Headers["Authorization"] != "synthetic-private" || got.Headers["X-Mode"] != "new" || got.URL != "https://example.test/new" || !got.ToolDisabled("write") || got.Exposure != "on_demand" {
		t.Fatal("edit lost settings")
	}
	for _, action := range []string{`"action":"toggle","enabled":false`, `"action":"set_exposure","exposure":"direct"`, `"action":"delete"`} {
		go func() {
			r, err := callMCPFixture(t.Context(), s, mcpRegisterTool, `{"name":"remote",`+action+`}`)
			done <- err == nil && !r.IsError
		}()
		p := waitMCPApproval(t, sessions, "mcp.manage")
		if err := sessions.answerRequest(p.SessionID, p.RequestID, agent.RequestAnswer{Decision: "allow_once"}); err != nil {
			t.Fatal(err)
		}
		if !<-done {
			t.Fatal(action)
		}
	}
	cfgs, _ = LoadMCPConfig()
	if _, exists := cfgs["remote"]; exists {
		t.Fatal("delete failed")
	}
}

func TestOnDemandPayloadProjection(t *testing.T) {
	r := textResult([]byte(`[{"entity":"light","attributes":{"name":"Kitchen","hidden":"private"}}]`), false)
	out, err := compactCallResult(r, []string{"entity", "attributes.name"}, 12000)
	if err != nil || out.Content[0].(*mcp.TextContent).Text != `[{"attributes":{"name":"Kitchen"},"entity":"light"}]` {
		t.Fatal(out, err)
	}
	big := textResult([]byte(strings.Repeat("lamp", 50000)), false)
	out, err = compactCallResult(big, nil, 90)
	if err != nil || len(out.Content[0].(*mcp.TextContent).Text) > 90 || !strings.Contains(out.Content[0].(*mcp.TextContent).Text, `"trimmed":true`) {
		t.Fatal(out, err)
	}
}

// Run the warm request path through Loom's authenticated HTTP transport.
// Fixture handshakes, key rotation and initial SDK registration are untimed.
func BenchmarkMCPWarm(b *testing.B) {
	b.Setenv("LOOM_HOME", b.TempDir())
	b.Setenv("HOME", b.TempDir())
	var calls atomic.Int32
	onDemandFixture(b, &calls)
	mux := http.NewServeMux()
	registerBrainRoutes(mux, nil)
	webAPI(mux)("/api/mcp", handleMCP)
	token, err := rotateGatewayToken()
	if err != nil {
		b.Fatal(err)
	}
	if err := storeWebKey("fixture-control"); err != nil {
		b.Fatal(err)
	}
	for _, tc := range []struct{ name, path, body string }{
		{"Initialize", "/mcp/loom", `{"jsonrpc":"2.0","method":"initialize","id":1,"params":{"protocolVersion":"2025-03-26","clientInfo":{"name":"bench","version":"1"},"capabilities":{}}}`},
		{"ListTools", "/mcp/loom", `{"jsonrpc":"2.0","method":"tools/list","id":2,"params":{}}`},
		{"Search", "/mcp/loom", `{"jsonrpc":"2.0","method":"tools/call","id":3,"params":{"name":"loom_mcp_search","arguments":{"query":"light"}}}`},
		{"Status", "/api/mcp", ""},
	} {
		b.Run(tc.name, func(b *testing.B) {
			request := func() {
				method, key := "POST", token
				if tc.body == "" {
					method, key = "GET", "fixture-control"
				}
				req := httptest.NewRequest(method, "http://localhost"+tc.path, strings.NewReader(tc.body))
				req.Header.Set("Authorization", "Bearer "+key)
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Accept", "application/json, text/event-stream")
				w := httptest.NewRecorder()
				mux.ServeHTTP(w, req)
				if w.Code != 200 || strings.Contains(w.Body.String(), `"error":{"code"`) {
					b.Fatal(w.Code, w.Body.String())
				}
			}
			request()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				request()
			}
		})
	}
}
