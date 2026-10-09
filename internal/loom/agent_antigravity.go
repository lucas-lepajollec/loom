package loom

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/harness"
	agent "github.com/lucas-lepajollec/loom/internal/loom/runtime"
	"github.com/lucas-lepajollec/loom/internal/loom/runtime/antigravity"
)

func antigravityCaps() []string {
	return harnessFeatureCaps(harnessFeatures(acpAgent{ID: "antigravity"}, "agy-stream-json", acpProbe{}))
}

func antigravityCompatibility(a acpAgent, version string) *agent.CompatibilityRecord {
	path, _ := lifecycleLookPath("agy")
	// Tested versions live in harness/tested_versions.json, edited by the watch.
	r := &agent.CompatibilityRecord{Runtime: a.ID, Executable: path, Version: version, AgentVersion: version, Protocol: antigravity.Protocol, AdapterVersion: agentAdapterVersion, AdapterPackage: "agy", TestedVersion: harness.LatestTestedVersion("antigravity"), TestedVersions: harness.TestedVersions("antigravity"), Capabilities: antigravityCaps()}
	if version != "" && !harness.VersionTested("antigravity", version) {
		r.Warning = "Antigravity " + version + " differs from tested " + r.TestedVersion + "; protocol compatibility is unverified"
	}
	return r
}

func recordAntigravityCompatibility(ctx context.Context, a acpAgent) *agent.CompatibilityRecord {
	version := ""
	if output, err := agyRead(ctx, "--version"); err == nil {
		version = boundedBytes(strings.TrimSpace(string(output)), 200)
	}
	r := antigravityCompatibility(a, version)
	_ = putStoreJSON(bkState, "agent_compat_"+a.ID, r)
	return r
}

func antigravityOptions(models []string) []map[string]any {
	options := []any{}
	for _, id := range models {
		options = append(options, map[string]any{"value": id, "name": id})
	}
	efforts := []any{}
	for _, effort := range antigravity.Efforts {
		efforts = append(efforts, map[string]any{"value": effort, "name": effort})
	}
	return []map[string]any{
		{"id": "model", "name": "Model", "category": "model", "type": "select", "options": options},
		{"id": "reasoning_effort", "name": "Reasoning effort", "category": "thought_level", "type": "select", "options": efforts},
		{"id": "sandbox", "name": "Native terminal sandbox", "type": "boolean", "currentValue": false},
	}
}

func antigravityModes() []map[string]any {
	return []map[string]any{
		{"id": "accept-edits", "name": "Auto edits"},
		{"id": "plan", "name": "Plan"},
		{"id": "full", "name": "Allow everything", "description": "Explicit launch-scoped permission bypass."},
	}
}

func probeAntigravity(ctx context.Context, a acpAgent) acpProbe {
	out := acpProbe{At: time.Now().UnixMilli(), Compatibility: recordAntigravityCompatibility(ctx, a), Caps: map[string]any{"loadSession": true}, Mode: "accept-edits", Modes: antigravityModes()}
	out.Agent = map[string]any{"name": a.Name, "version": out.Compatibility.Version}
	catalog, err := agyRead(ctx, "models")
	if err != nil {
		out.Error = err.Error()
		return out
	}
	models, err := antigravity.ParseModels(catalog)
	if err != nil {
		out.Error = err.Error()
		return out
	}
	out.Config = antigravityOptions(models)
	// Preserve labels from the same native catalog without weakening ID parsing.
	names := map[string]string{}
	for _, line := range strings.Split(string(catalog), "\n") {
		id, name, ok := strings.Cut(line, "\t")
		if ok && strings.TrimSpace(name) != "" {
			names[strings.TrimSpace(id)] = boundedBytes(strings.TrimSpace(name), 200)
		}
	}
	for _, value := range out.Config[0]["options"].([]any) {
		option := value.(map[string]any)
		if name := names[option["value"].(string)]; name != "" {
			option["name"] = name
		}
	}
	return out
}

func (m *runtimeSessions) runAntigravity(ctx context.Context, a acpAgent, s RuntimeSession, turn RuntimeTurn, emit ChatCallback) ([]Message, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	if len(turn.Messages) == 0 {
		return nil, errors.New("agent message required")
	}
	dir, err := acpDirectory(s.Workdir)
	if err != nil || dir != s.Workdir {
		return nil, errors.New("agent working directory inaccessible or changed")
	}
	if s.MCPServers != nil && len(*s.MCPServers) > 0 {
		return nil, errors.New("Loom MCP selection is not supported by Antigravity; configure MCP in the CLI")
	}
	if strings.HasPrefix(s.Model, acpLoomModelPrefix) || strings.HasPrefix(s.Model, acpProviderModelPrefix) {
		return nil, errors.New("Antigravity requires its native model catalog")
	}
	if _, err = harnessFilesystemMode(a, s.FilesystemPolicy); err != nil {
		return nil, err
	}
	state := cloneACPState(s.ACPState)
	state.NativeRuntimeID = a.ID
	prefix := acpContextHash(turn.Messages[:len(turn.Messages)-1])
	resume := state.NativeSessionID != "" && s.NativeRuntimeID == a.ID && s.NativeContext == prefix
	if !resume {
		state.NativeSessionID = ""
		state.NativeSessionFile = ""
	}
	effort := s.ReasoningEffort
	if raw, exists := s.ConfigOptions["reasoning_effort"]; exists {
		value, ok := raw.(string)
		if !ok {
			return nil, errors.New("Antigravity reasoning effort requires a string")
		}
		if value != "" {
			effort = value
		}
	}
	sandbox := false
	if raw, exists := s.ConfigOptions["sandbox"]; exists {
		var ok bool
		sandbox, ok = raw.(bool)
		if !ok {
			return nil, errors.New("Antigravity sandbox requires a boolean")
		}
	}
	if s.Mode == "" || s.Mode == "default" {
		s.Mode = "accept-edits"
		state.Mode = s.Mode
	}
	config := antigravity.TurnConfig{ConversationID: state.NativeSessionID, Model: s.Model, Effort: effort, Mode: s.Mode, Permission: s.Permission, Sandbox: sandbox, AdditionalDirs: s.AdditionalDirs}
	args, err := config.Args()
	if err != nil {
		return nil, err
	}
	path, err := lifecycleLookPath("agy")
	if err != nil {
		return nil, err
	}
	state.AvailableModes = antigravityModes()
	if state.Mode == "" {
		state.Mode = "default"
	}
	if probe, ok := loadACPProbe(a.ID); ok {
		state.AvailableConfigOptions = cloneACPState(ACPState{AvailableConfigOptions: probe.Config}).AvailableConfigOptions
	}
	for _, option := range state.AvailableConfigOptions {
		switch option["id"] {
		case "model":
			option["currentValue"] = s.Model
		case "reasoning_effort":
			option["currentValue"] = effort
		case "sandbox":
			option["currentValue"] = sandbox
		}
	}
	prompt, _ := turn.Messages[len(turn.Messages)-1].Content.(string)
	if !resume {
		raw, err := json.Marshal(turn.Messages)
		if err != nil {
			return nil, err
		}
		prompt = "Loom portable discussion (role/content; tools are not replayed):\n" + string(raw)
	}
	answer := ""
	projection := newAgentProjection()
	sink := func(e agent.AgentEvent) bool {
		if e.Type == "content.delta" && e.Stream == "assistant_text" {
			if e.Replace {
				answer = ""
			}
			answer += e.Delta
		}
		if e.Type == "turn.completed" {
			for _, closed := range projection.closeItems(e) {
				emit(StreamEvent{ACPEvent: projection.event(closed), AgentEvent: &closed})
			}
		}
		event := StreamEvent{ACPEvent: projection.event(e), AgentEvent: &e}
		if e.Type == "content.delta" && e.Stream == "assistant_text" {
			copy := answer
			event.AssistantSnapshot = &copy
		}
		if e.Type == "usage.spent" && e.Usage != nil && e.Usage.Input != nil && e.Usage.Output != nil && e.Usage.Total != nil {
			u := e.Usage
			usage := RuntimeUsage{Input: *u.Input, Output: *u.Output, Total: *u.Total}
			if u.Cached != nil {
				usage.Cached = *u.Cached
			}
			if u.Reasoning != nil {
				usage.Thinking = *u.Reasoning
			}
			event.Usage = &usage
		}
		return emit(event)
	}
	record := recordAntigravityCompatibility(ctx, a)
	if record.Warning != "" {
		if !sink(agent.AgentEvent{Type: "warning", Runtime: a.ID, Message: record.Warning, Raw: agent.JSON(record)}) {
			return nil, context.Canceled
		}
	}
	if !sink(agent.AgentEvent{Type: "warning", Runtime: a.ID, Message: "Antigravity headless mode has no permission/question reply channel; native rules apply and actions requiring approval are denied.", Raw: agent.JSON(map[string]any{"protocol": antigravity.Protocol})}) {
		return nil, context.Canceled
	}
	publishState := func() { copy := cloneACPState(state); emit(StreamEvent{ACPState: &copy}) }
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = s.Workdir
	cmd.Env = append(os.Environ(), "PATH="+lifecycleLocalPath()) // No Loom provider keys.
	err = antigravity.RunStream(ctx, cmd, prompt, state.NativeSessionID, s.LastRequestID, sink, func(id string) {
		if state.NativeSessionID != id {
			state.NativeSessionID = id
			publishState()
		}
	})
	history := append(append([]Message{}, turn.Messages...), Message{Role: "assistant", Content: answer})
	state.NativeContext = acpContextHash(history)
	publishState()
	return []Message{{Role: "assistant", Content: answer}}, err
}
