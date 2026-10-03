package loom

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"os"
	"sync"
)

//go:embed harness/acp_agents.json
var acpAgentsJSON []byte

type acpAgent struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Logo    string   `json:"logo"`
	Command string   `json:"command"`
	Args    []string `json:"args"`
	Detect  []string `json:"detect"`
	Docs    string   `json:"docs"`
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
	// Antigravity speaks no ACP: Loom's own bridge (loom agy-acp) drives agy.
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
	caps := []string{"chat", "stream", "cancel", "tools", "approvals", "plan", "usage", "workdir", "mcp"}
	if a.agent.Remote {
		caps = []string{"chat", "stream", "cancel", "tools", "approvals", "plan", "usage", "workdir", "remote"}
	}
	a.negotiatedMu.RLock()
	if a.loadSession {
		caps = append(caps, "resume")
	}
	a.negotiatedMu.RUnlock()
	if harnessHasQuota(a.agent) {
		caps = append(caps, "quota")
	}
	cli, hint := a.agent.Command, a.agent.Command+" "+joinACPArgs(a.agent.Args)
	switch a.agent.ID {
	case "codex":
		caps = append(caps, "quota")
	case "antigravity":
		// Driven headless through Loom's bridge: agy cannot ask for permission
		// (access is a mode) and receives no MCP servers from Loom.
		caps = []string{"chat", "stream", "cancel", "tools", "plan", "usage", "workdir", "resume", "quota"}
		cli, hint = "agy", "agy"
	}
	caps = append(caps, "connect")
	available := a.agent.available()
	connected := harnessConnected(a.agent)
	return RuntimeDescriptor{ID: a.agent.ID, Name: a.agent.Name, Kind: "harness", Logo: a.agent.Logo, CLI: cli, Description: acpDescription(a.agent), Consent: "Confirm sharing the conversation, instructions and selected folder with this harness.", Implemented: true, Available: &available, InstallHint: hint, Capabilities: caps, Docs: a.agent.Docs, Custom: a.agent.Custom, MachineID: a.agent.Machine, Connected: &connected, FilesystemPolicies: harnessFilesystemPolicies(a.agent), Machine: machineName(a.agent.Machine)}
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
	if a.sessions == nil || a.session.ID == "" {
		return nil, errors.New("discussion and ACP directory required")
	}
	result, err := a.sessions.runACP(ctx, a.agent, a.session, turn, emit)
	if ctx.Err() != nil {
		err = ctx.Err()
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
	if a.ID == "antigravity" {
		return "Google agent, controlled by Loom in headless mode with native tools, working folder and modes."
	}
	return "Coding agent via ACP, using the harness's native authentication and tools."
}
