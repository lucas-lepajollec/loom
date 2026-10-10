package loom

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"github.com/lucas-lepajollec/loom/internal/loom/policy"
	"os"
	"strings"
	"sync"
	"time"
)

//go:embed harness/acp_agents.json
var acpAgentsJSON []byte

type acpAgent struct {
	RegistryID      string   `json:"registry_id,omitempty"`
	RegistryVersion string   `json:"registry_version,omitempty"`
	RegistryPackage string   `json:"registry_package,omitempty"`
	RegistryKind    string   `json:"registry_kind,omitempty"`
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Logo            string   `json:"logo"`
	Command         string   `json:"command"`
	Args            []string `json:"args"`
	Detect          []string `json:"detect"`
	Docs            string   `json:"docs"`
	// Remote: the agent runs on another machine (e.g. through ssh); its folder
	// is not on this disk. Custom: defined by the user in Harnesses.
	Remote bool `json:"remote,omitempty"`
	Custom bool `json:"custom,omitempty"`
	// Machine and RemoteHome: for an agent on a connected machine, which one
	// and the absolute home folder there (default folder, probe folder).
	Machine    string `json:"machine,omitempty"`
	RemoteHome string `json:"remoteHome,omitempty"`
}

func (a acpAgent) available() bool {
	return a.unavailableReason() == ""
}

func (a acpAgent) unavailableReason() string {
	if a.ID == "deepseek-harness" {
		if _, err := lifecycleLookPath("dsh"); err == nil {
			return ""
		}
	}
	if nativeAgentProtocol(a) != "" {
		return ""
	}
	for _, binary := range a.Detect {
		if _, err := lifecycleLookPath(binary); err != nil {
			return "Native CLI not installed or not executable: " + binary
		}
	}
	if _, err := lifecycleLookPath(a.Command); err != nil {
		return "ACP launcher not installed or not executable: " + a.Command
	}
	return ""
}
func builtinACPAgents() []acpAgent {
	var entries []acpAgent
	if json.Unmarshal(acpAgentsJSON, &entries) != nil {
		panic("invalid ACP registry")
	}
	return entries
}

func registerACPAgents() {
	// Installed agy stream-json is preferred; Loom's ACP bridge remains the fallback.
	if executable, err := os.Executable(); err == nil {
		registerRuntime(&acpAdapter{agent: acpAgent{ID: "antigravity", Name: "Antigravity", Logo: "antigravity", Command: executable,
			Args: []string{"agy-acp"}, Detect: []string{"agy"}, Docs: "https://antigravity.google/"}})
	}
	for _, entry := range builtinACPAgents() {
		registerRuntime(&acpAdapter{agent: entry})
	}
	if os.Getenv("LOOM_DEV_FAKE_ACP") == "1" {
		executable, err := os.Executable()
		if err != nil {
			panic(err)
		}
		registerRuntime(&acpAdapter{agent: acpAgent{ID: "loom-fake-acp", Name: "Loom fake ACP", Logo: "loom", Command: executable, Args: []string{"loom-fake-acp"}}})
	}
}

type acpAdapter struct {
	negotiatedMu sync.RWMutex
	loadSession  bool
	agent        acpAgent
	sessions     *runtimeSessions
	session      RuntimeSession
}

func (a *acpAdapter) Descriptor() RuntimeDescriptor {
	features := a.features()
	caps := harnessFeatureCaps(features)
	cli, hint := a.agent.Command, a.agent.Command+" "+joinACPArgs(a.agent.Args)
	if features.Protocol != "acp" {
		cli, hint = a.agent.ID, a.agent.ID
	}
	if a.agent.ID == "antigravity" {
		cli, hint = "agy", "agy"
	}
	available := a.agent.available()
	connected := harnessConnected(a.agent)
	return degradedDescriptor(RuntimeDescriptor{Features: &features, DescriptionKey: harnessDescriptionKey(a.agent), Compatibility: agentCompatibility(a.agent), ID: a.agent.ID, Name: a.agent.Name, Kind: "harness", Logo: a.agent.Logo, CLI: cli, Description: acpDescription(a.agent), Consent: "Confirm sharing the conversation, instructions and selected folder with this harness.", Implemented: true, Available: &available, InstallHint: hint, Capabilities: caps, Docs: a.agent.Docs, Custom: a.agent.Custom, MachineID: a.agent.Machine, Connected: &connected, FilesystemPolicies: features.FilesystemPolicies, Machine: machineName(a.agent.Machine)})
}
func joinACPArgs(args []string) string {
	out := ""
	for i, s := range args {
		if i > 0 {
			out += " "
		}
		out += s
	}
	return out
}
func (a *acpAdapter) Quota(ctx context.Context) (QuotaSnapshot, error) {
	if harnessHasQuota(a.agent) {
		return readHarnessQuota(ctx, a.agent)
	}
	switch a.agent.ID {
	case "codex":
		return readCodexQuota(ctx)
	case "antigravity":
		return readAgyQuota(ctx)
	}
	return QuotaSnapshot{}, errors.New("quotas unavailable")
}
func (a *acpAdapter) Run(ctx context.Context, turn RuntimeTurn, emit ChatCallback) ([]Message, error) {
	if capabilityDisabled("agent:"+a.agent.ID, "chat") {
		return nil, errors.New("agent chat capability degraded; refresh its probe")
	}
	if a.sessions == nil || a.session.ID == "" {
		return nil, errors.New("discussion and ACP directory required")
	}
	if err := a.sessions.authorizePolicy(ctx, sessionPolicyInput(a.session, "data.send_provider", policy.Allow), false); err != nil {
		return nil, err
	}
	if a.session.ProviderID != "" {
		in := sessionPolicyInput(a.session, "spend.provider", policy.Allow)
		in.Cost = a.sessions.observedMonthlySpend(in.ProviderID, in.Endpoint)
		if err := a.sessions.authorizePolicy(ctx, in, false); err != nil {
			return nil, err
		}
	}
	if policyNeedsAgentApprovals(a.session) {
		f := a.features()
		if !f.Approvals {
			return nil, errors.New("agent protocol cannot enforce tool policy; choose an approval-capable executor")
		}
		// Opaque ACP bypass/auto modes can suppress requests. A restrictive
		// policy requires the agent's ordinary interactive mode.
		mode := strings.ToLower(a.session.Mode)
		if mode == "full" || mode == "bypasspermissions" || mode == "accept-edits" || mode == "acceptedits" || mode == "auto" {
			return nil, errors.New("agent mode bypasses approvals required by policy")
		}
	}
	// Capture only operation enums while the authorized session runs. Request
	// IDs are transient correlation keys and never enter the evidence store.
	var evidenceMu sync.Mutex
	requests := map[string]string{}
	observed := map[string]bool{}
	completed := false
	nextEmit := emit
	emit = func(event StreamEvent) bool {
		evidenceMu.Lock()
		if e := event.AgentEvent; e != nil {
			switch e.Type {
			case "content.delta":
				if e.Stream == "assistant_text" && e.Delta != "" {
					observed["stream"] = true
				}
			case "request.opened":
				if e.Request != nil && len(requests) < 64 {
					requests[e.Request.ID] = e.Request.Kind
				}
			case "request.resolved":
				if e.Outcome == "accepted" || e.Outcome == "declined" || e.Outcome == "answered" {
					if name := map[string]string{"approval": "approvals", "user_input": "user-input", "elicitation": "elicitation"}[requests[e.RequestID]]; name != "" {
						observed[name] = true
					}
				}
				delete(requests, e.RequestID)
			case "turn.completed":
				completed = e.Status == "completed" && e.Error == ""
			}
		}
		evidenceMu.Unlock()
		return nextEmit(event)
	}
	var result []Message
	var err error
	if nativeAgentProtocol(a.agent) == "agy-stream-json" {
		result, err = a.sessions.runAntigravity(ctx, a.agent, a.session, turn, emit)
	} else if nativeAgentProtocol(a.agent) == "opencode-http" {
		result, err = a.sessions.runOpenCode(ctx, a.agent, a.session, turn, emit)
	} else if nativeAgentProtocol(a.agent) != "" {
		result, err = a.sessions.runNativeAgent(ctx, a.agent, a.session, turn, emit)
	} else {
		result, err = a.sessions.runACP(ctx, a.agent, a.session, turn, emit)
	}
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	evidenceMu.Lock()
	names := []string{}
	if err == nil && completed {
		observed["chat"] = true
		for name := range observed {
			names = append(names, name)
		}
	}
	evidenceMu.Unlock()
	if len(names) > 0 {
		r := agentCompatibility(a.agent)
		recordSessionEvidence(a.agent, r, names, time.Now().UnixMilli())
	}
	return result, err
}

func acpDescription(a acpAgent) string {
	switch {
	case a.Remote:
		return "ACP harness on another machine, launched by " + a.Command + ". It uses its own files and tools."
	case a.Custom:
		return "Custom ACP harness launched by " + a.Command + "."
	}
	if a.ID == "deepseek-harness" {
		return "Official DeepSeek Harness via ACP. Developer preview; not yet verified with Loom."
	}
	if a.ID == "antigravity" {
		return "Google agent, controlled by Loom in headless mode with native tools, working folder and modes."
	}
	if protocol := nativeAgentProtocol(a); protocol != "" {
		return "Installed coding agent via " + protocol + ", using native authentication, sessions and tools."
	}
	return "Coding agent via ACP, using the harness's native authentication and tools."
}

func harnessDescriptionKey(a acpAgent) string {
	if a.Remote {
		return "agents.description.remote"
	}
	if a.Custom {
		return "agents.description.custom"
	}
	if a.ID == "deepseek-harness" {
		return "agents.description.deepseek"
	}
	if a.ID == "antigravity" {
		if nativeAgentProtocol(a) == "" {
			return "agents.description.antigravity-acp"
		}
		return "agents.description.antigravity"
	}
	if p := nativeAgentProtocol(a); p != "" {
		return "agents.description." + p
	}
	return "agents.description.acp"
}
