package loom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/capability"
	agent "github.com/lucas-lepajollec/loom/internal/loom/runtime"
	"github.com/lucas-lepajollec/loom/internal/loom/runtime/agentstdio"
	"github.com/lucas-lepajollec/loom/internal/loom/runtime/codexapp"
	"github.com/lucas-lepajollec/loom/internal/loom/runtime/pirpc"
)

const agentAdapterVersion = "2.0.0"

var nativeProtocolCache = struct {
	sync.Mutex
	entries map[string]bool
}{entries: map[string]bool{}}

// Probe only a builtin local CLI's help, never an account or a turn. Missing
// protocol support permits ACP fallback; authentication/runtime failures do not.
func nativeAgentProtocol(a acpAgent) string {
	if a.Remote || a.Custom {
		return ""
	}
	if a.ID == "antigravity" {
		if len(a.Args) != 1 || a.Args[0] != "agy-acp" {
			return ""
		}
	} else if a.ID == "opencode" {
		// Launch-scoped Loom sources retain ACP.
		if modelSinkEnabled("opencode") {
			return ""
		}
		if a.Command != "opencode" {
			return ""
		}
	} else if a.Command != "npx" || (a.ID != "codex" && a.ID != "pi") {
		return ""
	}
	binary := a.ID
	if a.ID == "antigravity" {
		binary = "agy"
	}
	path, err := lifecycleLookPath(binary)
	if err != nil {
		return ""
	}
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	key := fmt.Sprintf("%s:%s:%d:%d", a.ID, path, info.ModTime().UnixNano(), info.Size())
	nativeProtocolCache.Lock()
	defer nativeProtocolCache.Unlock()
	supported, ok := nativeProtocolCache.entries[key]
	if !ok {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		argv, err := harnessNativeArgv([]string{path, "--help"})
		if err == nil {
			// Some CLIs (OpenCode, agy) print their help on stderr.
			cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
			cmd.Env = append(os.Environ(), "PATH="+lifecycleLocalPath())
			out, err := cmd.CombinedOutput()
			needle := "app-server"
			if a.ID == "antigravity" {
				needle = "--input-format"
			}
			if a.ID == "opencode" {
				needle = "opencode serve"
			}
			if a.ID == "pi" {
				needle = "rpc"
			}
			supported = err == nil && strings.Contains(string(out), needle)
			if a.ID == "antigravity" {
				supported = supported && strings.Contains(string(out), "stream-json") && strings.Contains(string(out), "--output-format") && strings.Contains(string(out), "--conversation")
			}
		}
		nativeProtocolCache.entries[key] = supported
	}
	if !supported {
		return ""
	}
	if a.ID == "antigravity" {
		return "agy-stream-json"
	}
	if a.ID == "opencode" {
		return "opencode-http"
	}
	if a.ID == "pi" {
		return "pi-rpc"
	}
	return "app-server"
}
func agentCompatibility(a acpAgent) *agent.CompatibilityRecord {
	r := agentCompatibilityMetadata(a)
	readAgentEvidence(a, r)
	return r
}

func agentCompatibilityMetadata(a acpAgent) *agent.CompatibilityRecord {
	protocol := nativeAgentProtocol(a)
	if protocol == "" {
		protocol = "acp"
	}
	var r agent.CompatibilityRecord
	if getStoreJSON(bkState, "agent_compat_"+a.ID, &r) && r.Runtime != "" && r.Protocol == protocol {
		if protocol == "acp" {
			return acpCompatibility(a, map[string]any{"version": r.Version}, r.Capabilities)
		}
		if protocol == "opencode-http" {
			return openCodeCompatibility(a, r.Version)
		}
		return nativeCompatibility(a, protocol, r.Executable, r.Version, r.Capabilities)
	}
	executable := a.Command
	caps := []string{}
	if protocol == "agy-stream-json" {
		return antigravityCompatibility(a, "")
	}
	if protocol == "opencode-http" {
		return openCodeCompatibility(a, "")
	}
	if protocol == "acp" {
		return acpCompatibility(a, nil, nil)
	}
	executable, _ = lifecycleLookPath(a.ID)
	caps = nativeAgentCaps(a.ID)
	return nativeCompatibility(a, protocol, executable, "", caps)
}

func recordAgentCompatibility(ctx context.Context, a acpAgent, protocol string, caps []string) *agent.CompatibilityRecord {
	executable := a.Command
	if protocol != "acp" {
		executable = a.ID
	}
	path, _ := lifecycleLookPath(executable)
	version := ""
	argv, err := harnessNativeArgv([]string{executable, "--version"})
	if err == nil {
		c, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(c, argv[0], argv[1:]...)
		cmd.Env = append(os.Environ(), "PATH="+lifecycleLocalPath())
		out, err := cmd.Output()
		if err == nil {
			version = boundedBytes(strings.TrimSpace(string(out)), 200)
		}
	}
	r := nativeCompatibility(a, protocol, path, version, caps)
	_ = putStoreJSON(bkState, "agent_compat_"+a.ID, r)
	return r
}
func nativeCompatibility(a acpAgent, protocol, executable, version string, caps []string) *agent.CompatibilityRecord {
	return harnessCompatibility(a, protocol, executable, version, caps, "", "loom", "")
}
func nativeAgentCaps(id string) []string {
	protocol := "app-server"
	switch id {
	case "pi":
		protocol = "pi-rpc"
	case "opencode":
		protocol = "opencode-http"
	case "antigravity":
		protocol = "agy-stream-json"
	}
	return harnessFeatureCaps(harnessFeatures(acpAgent{ID: id}, protocol, acpProbe{}))
}

func startNativeAgent(a acpAgent, s RuntimeSession, probe bool, checkOnly bool) (*agentstdio.Client, error) {
	args := []string{a.ID, "app-server"}
	var env []string
	if a.ID == "codex" {
		if src, ok := resolveHarnessSource(a.ID, s.Model); ok {
			// app-server consumes native -c overrides, not codex-acp's CODEX_CONFIG.
			args = append(args, "-c", `model_provider="loom"`, "-c", `model_providers.loom.name="Loom"`, "-c", "model_providers.loom.base_url="+string(agent.JSON(src.Base)), "-c", `model_providers.loom.wire_api="responses"`, "-c", `model_providers.loom.env_key="LOOM_API_KEY"`)
			env = []string{"LOOM_API_KEY=" + src.Key}
		} else if strings.HasPrefix(s.Model, acpLoomModelPrefix) || strings.HasPrefix(s.Model, acpProviderModelPrefix) {
			return nil, errors.New("selected Codex model source unavailable")
		}
	} else {
		args = []string{"pi", "--mode", "rpc"}
		if probe {
			args = append(args, "--no-session")
			// Pi hides a provider whose key reference is unset: the catalog read
			// needs every projected key, or Loom's cloud models never show up.
			if !checkOnly {
				_ = syncModelSinks()
				env = append(env, acpLaunchEnv("pi", "")...)
			}
		} else {
			_ = syncModelSinks()
			// Existing Pi model sink uses provider IDs; expose only the selected key.
			if modelSinkEnabled("pi") {
				provider, _, _ := strings.Cut(s.Model, "/")
				if provider == "loom" {
					key := engineAPIKey()
					if key == "" {
						key = "loom"
					}
					env = append(env, "LOOM_API_KEY="+key)
				} else {
					for _, p := range chatSources() {
						if providerSlug(p) == provider {
							if key := workspaceSessions.providerKey(p.ID); key != "" {
								env = append(env, providerKeyEnv(p.ID)+"="+key)
							}
							break
						}
					}
				}
			}
			if s.NativeSessionFile == "" && s.NativeSessionID != "" && s.NativeRuntimeID == a.ID {
				args = append(args, "--session", s.NativeSessionID)
			}
		}
	}
	argv, err := harnessNativeArgv(args)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = s.Workdir
	cmd.Env = append(os.Environ(), "PATH="+lifecycleLocalPath())
	cmd.Env = append(cmd.Env, env...)
	return agentstdio.New(cmd)
}
func probeNativeAgent(ctx context.Context, a acpAgent) acpProbe {
	record := recordAgentCompatibility(ctx, a, nativeAgentProtocol(a), nativeAgentCaps(a.ID))
	out := acpProbe{Compatibility: record, At: time.Now().UnixMilli(), Caps: map[string]any{"loadSession": true}}
	dir, err := os.MkdirTemp("", "loom-agent-probe-")
	if err != nil {
		out.Error = err.Error()
		return out
	}
	defer os.RemoveAll(dir)
	c, err := startNativeAgent(a, RuntimeSession{ACPState: ACPState{Workdir: dir}}, true, harnessProbeIsCheckOnly(ctx))
	if err != nil {
		out.Error = err.Error()
		return out
	}
	defer c.Close()
	sink := func(agent.AgentEvent) bool { return true }
	b := agent.NewRequestBroker(a.ID, sink)
	defer b.Cancel()
	var models []json.RawMessage
	catalogStage := false
	if a.ID == "codex" {
		s := codexapp.New(c, b, sink)
		err = c.Start()
		if err == nil {
			err = s.Initialize(ctx)
		}
		if err == nil {
			catalogStage = true
			models, err = s.Models(ctx)
		}
	} else {
		s := pirpc.New(c, b, sink)
		err = c.Start()
		if err == nil {
			_, err = s.State(ctx)
		}
		if err == nil {
			catalogStage = true
			models, err = s.Models(ctx)
		}
	}
	if err != nil {
		out.Error = err.Error()
		if catalogStage {
			out.CapabilityChecks = append(out.CapabilityChecks, capability.Probe{Capability: "models", OK: false, Reason: "catalog_probe_failed"})
		}
		return out
	}
	out.CapabilityChecks = append(out.CapabilityChecks, capability.Probe{Capability: "models", OK: true})
	out.NativeModels = models
	options := []any{}
	for _, raw := range models {
		var m struct {
			ID          string `json:"id"`
			Model       string `json:"model"`
			Name        string `json:"name"`
			DisplayName string `json:"displayName"`
			Description string `json:"description"`
			Provider    string `json:"provider"`
		}
		if json.Unmarshal(raw, &m) != nil {
			continue
		}
		id := firstNonEmpty(m.Model, m.ID)
		if a.ID == "pi" {
			id = m.Provider + "/" + m.ID
		}
		options = append(options, map[string]any{"value": id, "name": firstNonEmpty(m.DisplayName, m.Name, id), "description": m.Description})
	}
	out.Config = []map[string]any{{"id": "model", "name": "Model", "category": "model", "type": "select", "options": options}}

	efforts := []string{}
	if a.ID == "pi" {
		efforts = []string{"off", "minimal", "low", "medium", "high", "xhigh"}
	} else {
		for _, raw := range models {
			var m struct {
				Efforts []struct {
					ID string `json:"reasoningEffort"`
				} `json:"supportedReasoningEfforts"`
			}
			_ = json.Unmarshal(raw, &m)
			for _, e := range m.Efforts {
				if e.ID != "" && !slices.Contains(efforts, e.ID) {
					efforts = append(efforts, e.ID)
				}
			}
		}
	}
	if len(efforts) > 0 {
		values := []any{}
		for _, effort := range efforts {
			values = append(values, map[string]any{"value": effort, "name": effort})
		}
		out.Config = append(out.Config, map[string]any{"id": "reasoning_effort", "name": "Reasoning effort", "category": "thought_level", "type": "select", "options": values})
	}
	out.Agent = map[string]any{"name": a.Name, "version": record.Version}
	return out
}
func (m *runtimeSessions) runNativeAgent(ctx context.Context, a acpAgent, s RuntimeSession, turn RuntimeTurn, emit ChatCallback) ([]Message, error) {
	ctx, turnCancel := context.WithCancel(ctx)
	defer turnCancel()
	if len(turn.Messages) == 0 {
		return nil, errors.New("agent message required")
	}
	dir, err := acpDirectory(s.Workdir)
	if err != nil || dir != s.Workdir {
		return nil, errors.New("agent working directory inaccessible or changed")
	}
	// Native protocol configuration for MCP is owned by the installed CLI. Never
	// silently drop an explicit Loom MCP selection that this slice cannot apply.
	if s.MCPServers != nil && len(*s.MCPServers) > 0 {
		return nil, errors.New("Loom MCP selection is not supported by this native adapter yet; configure MCP in the CLI")
	}
	prefix := acpContextHash(turn.Messages[:len(turn.Messages)-1])
	resume := s.NativeRuntimeID == a.ID && s.NativeSessionID != "" && s.NativeContext == prefix && !capabilityDisabled("agent:"+a.ID, "resume")
	if !resume {
		s.NativeSessionID = ""
		s.NativeSessionFile = ""
	}
	c, err := startNativeAgent(a, s, false, false)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	var emitMu sync.Mutex
	state := cloneACPState(s.ACPState)
	state.NativeRuntimeID = a.ID
	if probe, ok := loadACPProbe(a.ID); ok {
		state.AvailableConfigOptions = cloneACPState(ACPState{AvailableConfigOptions: probe.Config}).AvailableConfigOptions
		for _, option := range state.AvailableConfigOptions {
			if option["category"] == "model" {
				option["currentValue"] = s.Model
			}
		}
	}
	answer := ""
	answerParts := map[string]string{}
	answerOrder := []string{}
	var usage RuntimeUsage
	projection := newAgentProjection()
	emitEvent := func(e agent.AgentEvent) bool {
		emitMu.Lock()
		defer emitMu.Unlock()
		if e.Type == "content.delta" && e.Stream == "assistant_text" {
			if _, ok := answerParts[e.ItemID]; !ok {
				answerOrder = append(answerOrder, e.ItemID)
			}
			if e.Replace {
				for _, id := range answerOrder {
					if strings.HasPrefix(id, e.ItemID+":") {
						answerParts[id] = ""
					}
				}
				answerParts[e.ItemID] = ""
			}
			answerParts[e.ItemID] += e.Delta
			answer = ""
			for _, id := range answerOrder {
				answer += answerParts[id]
			}
		}
		if e.Type == "turn.completed" {
			for _, closed := range projection.closeItems(e) {
				row := projection.event(closed)
				emit(StreamEvent{ACPEvent: row, AgentEvent: &closed})
			}
		}
		display := projection.event(e)
		event := StreamEvent{ACPEvent: display, AgentEvent: &e}
		if e.Type == "content.delta" && e.Stream == "assistant_text" {
			snapshot := answer
			event.AssistantSnapshot = &snapshot
		}
		if e.Usage != nil && e.Usage.Input != nil && e.Usage.Output != nil && e.Usage.Total != nil {
			u := e.Usage
			if a.ID == "codex" {
				usage = RuntimeUsage{Input: *u.Input, Output: *u.Output}
			} else {
				usage.Input += *u.Input
				usage.Output += *u.Output
			}
			if u.Cached != nil {
				if a.ID == "codex" {
					usage.Cached = *u.Cached
				} else {
					usage.Cached += *u.Cached
				}
			}
			if u.Reasoning != nil {
				usage.Thinking = *u.Reasoning
			}
			if a.ID == "codex" {
				usage.Total = *u.Total
			} else {
				usage.Total += *u.Total
			}
			copy := usage
			event.Usage = &copy
		}
		return emit(event)
	}
	b := agent.NewRequestBroker(a.ID, emitEvent)
	nativePolicyBroker(b, s)
	m.acpMu.Lock()
	if m.requests == nil {
		m.requests = map[string]*agent.RequestBroker{}
	}
	m.requests[s.ID] = b
	m.acpMu.Unlock()
	defer func() { b.Cancel(); m.acpMu.Lock(); delete(m.requests, s.ID); m.acpMu.Unlock() }()
	r := recordAgentCompatibility(ctx, a, nativeAgentProtocol(a), nativeAgentCaps(a.ID))
	if r.Warning != "" {
		emitEvent(agent.AgentEvent{Type: "warning", Runtime: a.ID, Message: r.Warning, Raw: agent.JSON(r)})
	}
	effort := s.ReasoningEffort
	if configured, ok := s.ConfigOptions["reasoning_effort"].(string); ok && configured != "" {
		effort = configured
	}
	prompt, _ := turn.Messages[len(turn.Messages)-1].Content.(string)
	if !resume && !strings.HasPrefix(strings.TrimSpace(prompt), "/") {
		raw, _ := json.Marshal(turn.Messages)
		prompt = "Loom portable discussion (role/content; tools are not replayed):\n" + string(raw)
	}
	publishState := func() {
		emitMu.Lock()
		defer emitMu.Unlock()
		copy := cloneACPState(state)
		emit(StreamEvent{ACPState: &copy})
	}
	if a.ID == "codex" {
		session := codexapp.New(c, b, emitEvent)
		if err = c.Start(); err == nil {
			initCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
			err = session.Initialize(initCtx)
			cancel()
		}
		sandbox := ""
		switch s.FilesystemPolicy {
		case "workspace-write":
			sandbox = "workspace-write"
		case "full-access":
			sandbox = "danger-full-access"
		}
		approval := "on-request"
		if s.Permission == "full" && !policyNeedsAgentApprovals(s) {
			approval = "never"
		}
		model := s.Model
		if src, ok := resolveHarnessSource(a.ID, model); ok {
			model = src.Model
		}
		if err == nil {
			err = session.Turn(ctx, codexapp.TurnConfig{ThreadID: s.NativeSessionID, Workdir: s.Workdir, Model: model, Effort: effort, Approval: approval, Sandbox: sandbox, AdditionalDirs: s.AdditionalDirs}, prompt, func(id string) {
				state.NativeSessionID = id
				if session.ActiveModel != "" {
					for _, option := range state.AvailableConfigOptions {
						if option["category"] == "model" {
							option["currentValue"] = session.ActiveModel
						}
					}
				}
				publishState()
			})
		}
	} else {
		session := pirpc.New(c, b, emitEvent)
		session.SetTurnID(s.LastRequestID)
		err = c.Start()
		provider, model, qualified := strings.Cut(s.Model, "/")
		if !qualified {
			model = s.Model
			provider = ""
		}
		if err == nil {
			err = session.Turn(ctx, s.NativeSessionFile, provider, model, effort, prompt, func(st pirpc.State) {
				state.NativeSessionID = st.SessionID
				state.NativeSessionFile = st.SessionFile
				var model struct {
					ID       string `json:"id"`
					Provider string `json:"provider"`
				}
				if json.Unmarshal(st.Model, &model) == nil && model.ID != "" {
					for _, option := range state.AvailableConfigOptions {
						if option["category"] == "model" {
							option["currentValue"] = model.Provider + "/" + model.ID
						}
					}
				}
				publishState()
			})
		}
	}
	b.Cancel()
	if err != nil {
		status := "failed"
		if ctx.Err() != nil || errors.Is(err, context.Canceled) {
			status = "cancelled"
		}
		emitMu.Lock()
		ended := projection.ended
		emitMu.Unlock()
		if !ended {
			emitEvent(agent.AgentEvent{Type: "turn.completed", Runtime: a.ID, ThreadID: state.NativeSessionID, Status: status, Error: err.Error(), Raw: agent.JSON(map[string]any{"error": err.Error()})})
		}
	}
	// Reuse native state after interruption too: the CLI owns its transcript. A
	// real route/context change still starts a fresh, explicit text handoff.
	history := append(append([]Message{}, turn.Messages...), Message{Role: "assistant", Content: answer})
	state.NativeContext = acpContextHash(history)
	publishState()
	return []Message{{Role: "assistant", Content: answer}}, err
}
func handleRuntimeCompatibility(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodGet) || !usageVaultAccess(w) {
		return
	}
	a, ok := acpAgentFor(r.PathValue("id"))
	if !ok {
		sendJSON(w, 404, map[string]any{"ok": false, "error": "runtime not found"})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "compatibility": agentCompatibility(a)})
}
