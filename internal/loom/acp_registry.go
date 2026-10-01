package loom

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
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
}

func (a acpAgent) available() bool {
	if _, err := exec.LookPath(a.Command); err != nil {
		return false
	}
	for _, binary := range a.Detect {
		if _, err := exec.LookPath(binary); err != nil {
			return false
		}
	}
	return true
}
func registerACPAgents() {
	var entries []acpAgent
	if json.Unmarshal(acpAgentsJSON, &entries) != nil {
		panic("registre ACP invalide")
	}
	for _, entry := range entries {
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
	a.negotiatedMu.RLock()
	if a.loadSession {
		caps = append(caps, "resume")
	}
	a.negotiatedMu.RUnlock()
	if a.agent.ID == "codex" {
		caps = append(caps, "quota")
	}
	return RuntimeDescriptor{ID: a.agent.ID, Name: a.agent.Name, Kind: "harness", Logo: a.agent.Logo, CLI: a.agent.Command, Description: "Agent de code via ACP. Authentification et outils natifs du harness.", Consent: "Confirmez le partage du fil, des instructions et du dossier choisi avec ce harness.", Implemented: true, Available: a.agent.available(), InstallHint: a.agent.Command + " " + joinACPArgs(a.agent.Args), Capabilities: caps, Docs: a.agent.Docs}
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
	if a.agent.ID != "codex" {
		return QuotaSnapshot{}, errors.New("quotas indisponibles")
	}
	return readCodexQuota(ctx)
}
func (a *acpAdapter) Run(ctx context.Context, turn RuntimeTurn, emit ChatCallback) ([]Message, error) {
	if a.sessions == nil || a.session.ID == "" {
		return nil, errors.New("discussion et dossier ACP requis")
	}
	result, err := a.sessions.runACP(ctx, a.agent, a.session, turn, emit)
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	return result, err
}
