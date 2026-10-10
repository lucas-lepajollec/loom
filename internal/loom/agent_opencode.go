package loom

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/capability"
	agent "github.com/lucas-lepajollec/loom/internal/loom/runtime"
	"github.com/lucas-lepajollec/loom/internal/loom/runtime/opencodehttp"
)

var openCodeServer opencodehttp.Server

func openCodeClient(ctx context.Context) (*opencodehttp.Client, error) {
	argv, err := harnessNativeArgv([]string{"opencode"})
	if err != nil {
		return nil, err
	}
	return openCodeServer.Client(ctx, argv, openCodeLaunchEnv())
}

// openCodeActive counts OpenCode turns in flight (see opencodehttp.Server.Busy).
var openCodeActive atomic.Int32

func init() { openCodeServer.Busy = func() bool { return openCodeActive.Load() > 0 } }

// openCodeLaunchEnv gives the shared OpenCode server Loom's local models and
// cloud providers, with their keys, only when Loom models are enabled for it.
func openCodeLaunchEnv() []string {
	env := []string{"PATH=" + lifecycleLocalPath()}
	if !modelSinkEnabled("opencode") {
		return env
	}
	key := engineAPIKey()
	if key == "" {
		key = "loom"
	}
	env = append(env, "LOOM_API_KEY="+key, "OPENCODE_CONFIG_CONTENT="+openCodeConfig())
	for _, p := range chatSources() {
		if k := workspaceSessions.providerKey(p.ID); k != "" {
			env = append(env, providerKeyEnv(p.ID)+"="+k)
		}
	}
	return env
}
func openCodeCaps() []string {
	return harnessFeatureCaps(harnessFeatures(acpAgent{ID: "opencode"}, "opencode-http", acpProbe{}))
}
func openCodeCompatibility(a acpAgent, version string) *agent.CompatibilityRecord {
	path, _ := lifecycleLookPath("opencode")
	// Preserve the historical display name and schema baseline.
	a.Name = "OpenCode"
	return harnessCompatibility(a, "opencode-http", path, version, openCodeCaps(), "opencode", "loom", opencodehttp.TestedVersion)
}
func probeOpenCode(ctx context.Context, a acpAgent) acpProbe {
	out := acpProbe{At: time.Now().UnixMilli(), Caps: map[string]any{"loadSession": true}}
	c, err := openCodeClient(ctx)
	if err != nil {
		out.Error = err.Error()
		return out
	}
	version, err := c.Health(ctx)
	out.Compatibility = openCodeCompatibility(a, version)
	_ = putStoreJSON(bkState, "agent_compat_"+a.ID, out.Compatibility)
	if err != nil {
		out.Error = err.Error()
		return out
	}
	dir, err := os.MkdirTemp("", "loom-opencode-probe-")
	if err != nil {
		out.Error = err.Error()
		return out
	}
	defer os.RemoveAll(dir)
	models, err := c.Models(ctx, dir)
	if err != nil {
		out.Error = err.Error()
		out.CapabilityChecks = append(out.CapabilityChecks, capability.Probe{Capability: "models", OK: false, Reason: "catalog_probe_failed"})
		return out
	}
	sort.Slice(models, func(i, j int) bool { return string(models[i]) < string(models[j]) })
	out.CapabilityChecks = append(out.CapabilityChecks, capability.Probe{Capability: "models", OK: true})
	out.NativeModels = models
	out.Agent = map[string]any{"name": a.Name, "version": version}
	options := []any{}
	for _, raw := range models {
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		options = append(options, map[string]any{"value": m["model"], "name": m["name"]})
	}
	out.Config = []map[string]any{{"id": "model", "name": "Model", "category": "model", "type": "select", "options": options}}
	return out
}
func (m *runtimeSessions) runOpenCode(ctx context.Context, a acpAgent, s RuntimeSession, turn RuntimeTurn, emit ChatCallback) ([]Message, error) {
	openCodeActive.Add(1)
	defer openCodeActive.Add(-1)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if len(turn.Messages) == 0 {
		return nil, errors.New("agent message required")
	}
	dir, err := acpDirectory(s.Workdir)
	if err != nil || dir != s.Workdir {
		return nil, errors.New("agent working directory inaccessible or changed")
	}
	if s.MCPServers != nil && len(*s.MCPServers) > 0 {
		return nil, errors.New("Loom MCP selection is not supported by this native adapter yet; configure MCP in the CLI")
	}
	if strings.HasPrefix(s.Model, acpLoomModelPrefix) || strings.HasPrefix(s.Model, acpProviderModelPrefix) || (modelSinkEnabled("opencode") && strings.HasPrefix(s.Model, "loom/")) {
		return nil, errors.New("Loom model sources require the OpenCode ACP launcher; the shared HTTP server uses native credentials")
	}
	c, err := openCodeClient(ctx)
	if err != nil {
		return nil, err
	}
	state := cloneACPState(s.ACPState)
	state.NativeRuntimeID = a.ID
	prefix := acpContextHash(turn.Messages[:len(turn.Messages)-1])
	resume := s.NativeRuntimeID == a.ID && s.NativeSessionID != "" && s.NativeContext == prefix && !capabilityDisabled("agent:"+a.ID, "resume")
	if !resume {
		state.NativeSessionID = ""
	}
	var mu sync.Mutex
	answer := agent.NewAnswerAccumulator(agent.ReplaceItem)
	var usage RuntimeUsage
	projection := newAgentProjection()
	sink := func(e agent.AgentEvent) bool {
		mu.Lock()
		defer mu.Unlock()
		snapshot := answer.Apply(e)
		if e.Type == "turn.completed" {
			for _, closed := range projection.closeItems(e) {
				row := projection.event(closed)
				emit(StreamEvent{ACPEvent: row, AgentEvent: &closed})
			}
		}
		row := projection.event(e)
		event := StreamEvent{ACPEvent: row, AgentEvent: &e}
		event.AssistantSnapshot = snapshot
		if e.Type == "usage.spent" && e.Usage != nil && e.Usage.Input != nil && e.Usage.Output != nil {
			u := e.Usage
			usage.Input += *u.Input
			usage.Output += *u.Output
			if u.Cached != nil {
				usage.Cached += *u.Cached
			}
			if u.Reasoning != nil {
				usage.Thinking += *u.Reasoning
			}
			if u.Total != nil {
				usage.Total += *u.Total
			}
			copy := usage
			event.Usage = &copy
		}
		return emit(event)
	}
	broker := agent.NewRequestBroker(a.ID, sink)
	nativePolicyBroker(broker, s)
	m.acpMu.Lock()
	if m.requests == nil {
		m.requests = map[string]*agent.RequestBroker{}
	}
	m.requests[s.ID] = broker
	m.acpMu.Unlock()
	defer func() { broker.Cancel(); m.acpMu.Lock(); delete(m.requests, s.ID); m.acpMu.Unlock() }()
	version, err := c.Health(ctx)
	if err != nil {
		return nil, err
	}
	record := openCodeCompatibility(a, version)
	_ = putStoreJSON(bkState, "agent_compat_"+a.ID, record)
	if record.Warning != "" {
		sink(agent.AgentEvent{Type: "warning", Runtime: a.ID, Message: record.Warning, Raw: agent.JSON(record)})
	}
	prompt, _ := turn.Messages[len(turn.Messages)-1].Content.(string)
	if !resume {
		raw, _ := json.Marshal(turn.Messages)
		prompt = "Loom portable discussion (role/content; tools are not replayed):\n" + string(raw)
	}
	native := &opencodehttp.Session{Client: c, Broker: broker, Emit: sink}
	err = native.Turn(ctx, opencodehttp.TurnConfig{SessionID: state.NativeSessionID, Workdir: s.Workdir, Model: s.Model}, prompt, func(id string) {
		state.NativeSessionID = id
		copy := cloneACPState(state)
		emit(StreamEvent{ACPState: &copy})
	})
	if err != nil {
		mu.Lock()
		ended := projection.ended
		mu.Unlock()
		if !ended {
			status := "failed"
			if ctx.Err() != nil {
				status = "cancelled"
			}
			sink(agent.AgentEvent{Type: "error", Runtime: a.ID, ThreadID: state.NativeSessionID, Error: err.Error(), Raw: agent.JSON(map[string]any{"error": err.Error()})})
			sink(agent.AgentEvent{Type: "turn.completed", Runtime: a.ID, ThreadID: state.NativeSessionID, Status: status, Error: err.Error(), Raw: agent.JSON(map[string]any{"error": err.Error()})})
		}
	}
	mu.Lock()
	result := answer.Text()
	mu.Unlock()
	history := append(append([]Message{}, turn.Messages...), Message{Role: "assistant", Content: result})
	state.NativeContext = acpContextHash(history)
	copy := cloneACPState(state)
	emit(StreamEvent{ACPState: &copy})
	return []Message{{Role: "assistant", Content: result}}, err
}

func listOpenCodeSessions(ctx context.Context, a acpAgent) ([]acpSessionInfo, error) {
	c, err := openCodeClient(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := c.List(ctx, "")
	if err != nil {
		return nil, err
	}
	out := []acpSessionInfo{}
	for _, raw := range rows {
		var r struct {
			ID        string `json:"id"`
			Directory string `json:"directory"`
			Title     string `json:"title"`
			Time      struct {
				Updated int64 `json:"updated"`
			} `json:"time"`
		}
		if json.Unmarshal(raw, &r) != nil {
			continue
		}
		out = append(out, acpSessionInfo{SessionID: r.ID, Cwd: r.Directory, Title: r.Title, UpdatedAt: time.UnixMilli(r.Time.Updated).UTC().Format(time.RFC3339)})
	}
	return markNativeImports(a, out), nil
}
func readOpenCodeHistory(ctx context.Context, id, dir string) ([]Message, error) {
	c, err := openCodeClient(ctx)
	if err != nil {
		return nil, err
	}
	var rows []struct {
		Info struct {
			Role string `json:"role"`
		} `json:"info"`
		Parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"parts"`
	}
	if err = c.Call(ctx, "GET", "/session/"+url.PathEscape(id)+"/message", dir, nil, &rows); err != nil {
		return nil, err
	}
	out := []Message{}
	for _, row := range rows {
		if row.Info.Role != "user" && row.Info.Role != "assistant" {
			continue
		}
		text := ""
		for _, part := range row.Parts {
			if part.Type == "text" {
				text += part.Text
			}
		}
		if text != "" {
			out = append(out, Message{Role: row.Info.Role, Content: text})
		}
	}
	return out, nil
}
