package loom

import (
	agent "github.com/lucas-lepajollec/loom/internal/loom/runtime"
	"path/filepath"
	"slices"
	"strings"
)

// harnessFeatures is the one UI contract for the execution path actually used.
// ACP modes/options are opaque: expose only session announcements, never guesses.
func harnessFeatures(a acpAgent, protocol string, p acpProbe) agent.HarnessFeatures {
	f := agent.HarnessFeatures{Protocol: protocol, ModelSources: []string{"native"}, Permissions: []string{}, Modes: []string{}, ConfigOptions: []string{}, FilesystemPolicies: []string{"native"}, Workdir: true}
	switch protocol {
	case "app-server":
		f.Models, f.Effort, f.Approvals, f.Questions, f.Forms, f.Plan = true, true, true, true, true, true
		f.Permissions = []string{"ask", "full"}
		f.ModelSources = append(f.ModelSources, "loom")
		f.Resume, f.SessionList, f.HistoryImport = true, true, true
		f.FilesystemPolicies = []string{"native", "workspace-write", "full-access"}
		f.AdditionalDirs = true
	case "pi-rpc":
		f.Models, f.Effort, f.Questions = true, true, true
		f.ModelSources = append(f.ModelSources, "loom")
		f.Resume, f.SessionList, f.HistoryImport = true, true, true
	case "opencode-http":
		f.ModelSources = append(f.ModelSources, "loom")
		f.LoomProtocol = "acp" // enabling the source selects the launch-scoped ACP adapter
		f.Models, f.Approvals, f.Questions, f.Plan = true, true, true, true
		f.Resume, f.SessionList, f.HistoryImport = true, true, true
	case "agy-stream-json":
		f.Models, f.Effort, f.Plan, f.Sandbox, f.Resume, f.AdditionalDirs = true, true, true, true, true, true
		f.Modes = []string{"accept-edits", "plan", "full"}
		f.DefaultMode = "accept-edits"
		f.ConfigOptions = []string{"model", "reasoning_effort", "sandbox"}
	case "acp":
		f.Approvals, f.MCPSelection = true, !a.Remote
		f.Permissions = []string{"ask", "edits", "full"}
		f.Questions, f.Forms = claudeACPAgent(a), !antigravityACPAgent(a) && !deepseekACPAgent(a)
		f.Plan = true // standard ACP plan updates are mapped when supplied
		f.Resume, _ = p.Caps["loadSession"].(bool)
		sc, _ := p.Caps["sessionCapabilities"].(map[string]any)
		f.SessionList = acpCapabilityAvailable(sc["list"])
		f.AdditionalDirs = acpCapabilityAvailable(sc["additionalDirectories"])
		f.AdditionalDirs = f.AdditionalDirs && !a.Remote
		f.HistoryImport = f.Resume && f.SessionList
		for _, m := range p.Modes {
			if id, ok := m["id"].(string); ok {
				f.Modes = append(f.Modes, id)
			}
		}
		for _, o := range p.Config {
			id, _ := o["id"].(string)
			if id != "" {
				f.ConfigOptions = append(f.ConfigOptions, id)
			}
			f.Models = f.Models || o["category"] == "model"
			f.Effort = f.Effort || o["category"] == "thought_level" || id == "reasoning_effort"
			f.Sandbox = f.Sandbox || id == "sandbox" || id == "sandbox_mode"
		}
		if !a.Remote {
			if _, ok := modelSinkFor(a.ID); ok {
				f.ModelSources = append(f.ModelSources, "loom")
			}
		}
		if a.ID == "codex" || a.Machine != "" && usageHarnessID(a) == "codex" {
			f.FilesystemPolicies = []string{"native", "workspace-write", "full-access"}
		}
		if deepseekACPAgent(a) {
			f.Models, f.Effort, f.Resume, f.SessionList = true, true, true, true
			f.HistoryImport, f.Forms, f.Questions, f.Plan = false, false, false, false
			f.AdditionalDirs, f.Sandbox = false, false
			f.Modes = []string{}
			f.ConfigOptions = []string{"model", "reasoning_effort"}
		}
		if antigravityACPAgent(a) {
			f.Approvals, f.Forms, f.Questions, f.MCPSelection = false, false, false, false
			f.Permissions = []string{}
			f.Modes, f.DefaultMode = []string{"accept-edits", "plan", "full"}, "accept-edits"
		}
	}
	if protocol != "acp" {
		if !slices.Contains(f.ConfigOptions, "model") {
			f.ConfigOptions = append(f.ConfigOptions, "model")
		}
		if f.Effort && !slices.Contains(f.ConfigOptions, "reasoning_effort") {
			f.ConfigOptions = append(f.ConfigOptions, "reasoning_effort")
		}
	}
	f.Usage = true // only native usage updates; absent counters remain unknown
	f.Quota = harnessHasQuota(a) || a.ID == "codex" || a.ID == "antigravity"
	f.Remote = a.Remote // ACP via the saved SSH launcher; native protocols are local
	f.Terminal = nativeResumeCommand(usageHarnessID(a), "session") != ""
	if !a.Remote {
		for _, h := range gatewayHarnessSpecs {
			f.MCPGateway = f.MCPGateway || h.id == a.ID
		}
		for _, h := range skillSinkDefs() {
			f.Skills = f.Skills || slices.Contains(h.Harnesses, a.ID)
		}
		for _, h := range brainAgentSpecs() {
			f.Memory = f.Memory || h.id == a.ID && h.file != ""
		}
	}
	return f
}

func acpCapabilityAvailable(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case map[string]any:
		return v != nil
	default:
		return false
	}
}

func (a *acpAdapter) features() agent.HarnessFeatures {
	protocol := nativeAgentProtocol(a.agent)
	if protocol == "" {
		protocol = "acp"
	}
	p, _ := loadACPProbe(a.agent.ID)
	a.negotiatedMu.RLock()
	if a.loadSession {
		if p.Caps == nil {
			p.Caps = map[string]any{}
		}
		p.Caps["loadSession"] = true
	}
	a.negotiatedMu.RUnlock()
	return harnessFeatures(a.agent, protocol, p)
}

func harnessFeatureCaps(f agent.HarnessFeatures) []string {
	caps := []string{"chat", "stream", "cancel", "tools", "connect"}
	for _, c := range []struct {
		name string
		on   bool
	}{{"models", f.Models}, {"reasoning-effort", f.Effort}, {"approvals", f.Approvals}, {"user-input", f.Questions}, {"elicitation", f.Forms}, {"plan", f.Plan}, {"usage", f.Usage}, {"quota", f.Quota}, {"workdir", f.Workdir}, {"resume", f.Resume}, {"session-list", f.SessionList}, {"history-import", f.HistoryImport}, {"mcp", f.MCPSelection}, {"mcp-gateway", f.MCPGateway}, {"skills", f.Skills}, {"memory", f.Memory}, {"terminal", f.Terminal}, {"remote", f.Remote}} {
		if c.on {
			caps = append(caps, c.name)
		}
	}
	if f.Protocol != "acp" {
		caps = append(caps, "native-events", "raw-events")
	}
	if f.Protocol == "app-server" {
		caps = append(caps, "reasoning-summary")
	}
	return caps
}

func localHarnessUsable(id string) bool {
	binary := id
	if id == "antigravity" {
		binary = "agy"
	}
	if id == "claude-code" {
		binary = "claude"
	}
	if id == "deepseek-harness" {
		binary = "dsh"
	}
	_, err := lifecycleLookPath(binary)
	return err == nil
}

func projectHarnessProbe(a acpAgent, p *acpProbe) {
	protocol := nativeAgentProtocol(a)
	if protocol == "" {
		protocol = "acp"
	}
	f := harnessFeatures(a, protocol, *p)
	p.Features = &f
	modes := []map[string]any{}
	for _, m := range p.Modes {
		id, _ := m["id"].(string)
		if slices.Contains(f.Modes, id) {
			modes = append(modes, m)
		}
	}
	p.Modes = modes
	options := []map[string]any{}
	for _, o := range p.Config {
		id, _ := o["id"].(string)
		if slices.Contains(f.ConfigOptions, id) {
			options = append(options, o)
		}
	}
	p.Config = options
	if f.DefaultMode != "" && !slices.Contains(f.Modes, p.Mode) {
		p.Mode = f.DefaultMode
	}
	if p.Compatibility != nil {
		p.Compatibility.Capabilities = harnessFeatureCaps(f)
	}
}

func deepseekACPAgent(a acpAgent) bool {
	if a.ID == "deepseek-harness" || filepath.Base(a.Command) == "dsh" {
		return true
	}
	for _, arg := range a.Args {
		if strings.HasPrefix(arg, "@deepseek-ai/dsh@") || arg == "@deepseek-ai/dsh" {
			return true
		}
	}
	return a.RegistryPackage == "@deepseek-ai/dsh"
}

func antigravityACPAgent(a acpAgent) bool {
	if a.ID == "antigravity" || usageHarnessID(a) == "antigravity" {
		return true
	}
	for _, arg := range a.Args {
		if arg == "agy-acp" {
			return true
		}
	}
	return false
}
