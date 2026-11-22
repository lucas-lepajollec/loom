package loom

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lucas-lepajollec/loom/internal/loom/resources"
	"github.com/lucas-lepajollec/loom/internal/loom/tools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Agents driving Loom. The authenticated gateway (/mcp/loom) gives an agent
// the same reach as the person using Loom:
//
//   - Loom's own tools (server "loom"): what Loom knows and does itself, i.e.
//     its machines and running a command on one of them.
//   - Codeman's own MCP server (server "agents"), started by Loom with the
//     connection it already holds: start agent sessions in any installed CLI,
//     prompt them, wait for them, read them, stop them. Loom does not re-drive
//     agent CLIs itself; Codeman's tested behaviour and fixes apply unchanged.
//
// Every call goes through the gateway's policy middleware, so a confirm rule in
// Settings › Permissions turns a tool into a request waiting for the person.

const agentsUpstream = "agents"

var agentsMCP = tools.NewMCPManager(codemanMCPSource{}, mcpConnect)

// codemanMCPSource describes `codeman mcp` for the connected Codeman. The
// credential is passed in the child's environment only, never written.
type codemanMCPSource struct{}

func (codemanMCPSource) LoadMCPConfig() (map[string]resources.MCPServerConfig, error) {
	// Only a connection the person saved: the default address is for discovery.
	saved, bin := savedCodemanConfig(), codemanCLI()
	if saved == nil || saved.URL == "" || bin == "" {
		return map[string]resources.MCPServerConfig{}, nil
	}
	cfg := *saved
	env := map[string]string{
		"CODEMAN_API_URL": cfg.URL, "CODEMAN_USERNAME": cfg.Username,
		// Loom may itself run inside a Codeman pane (development): the agents
		// server must not take that pane for the calling session.
		"CODEMAN_MUX": "", "CODEMAN_SESSION_ID": "",
	}
	if p := codemanPassword(); p != "" {
		env["CODEMAN_PASSWORD"] = p
	}
	return map[string]resources.MCPServerConfig{agentsUpstream: {Command: bin, Args: []string{"mcp", "--url", cfg.URL}, Env: env, Enabled: true}}, nil
}

// codemanCLI finds the Codeman command on this machine: PATH first, then the
// user installation folder (a service often starts without the login PATH).
func codemanCLI() string {
	if p, err := exec.LookPath("codeman"); err == nil {
		return p
	}
	if home, err := os.UserHomeDir(); err == nil {
		p := filepath.Join(home, ".local", "bin", "codeman")
		if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			return p
		}
	}
	return ""
}

type agentMachine struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Kind      string   `json:"kind"` // this (the machine serving Loom), node or ssh
	Host      string   `json:"host,omitempty"`
	OS        string   `json:"os,omitempty"`
	Modules   []string `json:"modules,omitempty"`
	Terminal  bool     `json:"terminal"`
	CheckedAt int64    `json:"checked_at,omitempty"`
}

func agentMachines() []agentMachine {
	host, _ := os.Hostname()
	out := []agentMachine{{ID: "local", Name: host, Kind: "this", Terminal: ptySupported}}
	for _, m := range loadRemoteMachines() {
		kind := "ssh"
		if m.NodeID != "" {
			kind = "node"
		}
		name := m.Name
		if name == "" {
			name = m.Hostname
		}
		out = append(out, agentMachine{ID: m.ID, Name: name, Kind: kind, Host: m.Host, OS: m.OS, Modules: m.Modules, Terminal: machineTerminalsSupported(m), CheckedAt: m.CheckedAt})
	}
	return out
}

type agentOverview struct {
	Loom     string            `json:"loom"`
	Machines []agentMachine    `json:"machines"`
	Roles    map[string]string `json:"roles"`
	Agents   string            `json:"agents"`
	Guide    string            `json:"guide"`
}

// agentsState: available, codeman_not_connected or codeman_cli_missing.
func agentsState() string {
	cfg := savedCodemanConfig()
	switch {
	case cfg == nil || cfg.URL == "":
		return "codeman_not_connected"
	case codemanCLI() == "":
		return "codeman_cli_missing"
	}
	return "available"
}

func agentsStatus() string {
	switch agentsState() {
	case "codeman_not_connected":
		return "unavailable: Codeman is not connected in Loom (Settings › Agents)"
	case "codeman_cli_missing":
		return "unavailable: the codeman command is not installed on the machine serving Loom"
	}
	return "available through the agents__* tools (Codeman at " + savedCodemanConfig().URL + ")"
}

type machineRunArgs struct {
	Machine        string `json:"machine" jsonschema:"machine id from loom_machines; local is the machine serving Loom"`
	Command        string `json:"command" jsonschema:"shell command, run by the machine's login shell"`
	Dir            string `json:"dir,omitempty" jsonschema:"absolute working folder (optional)"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" jsonschema:"stop the command after this many seconds (default 60, at most 600)"`
}
type machineRunResult struct {
	Machine  string `json:"machine"`
	ExitCode *int   `json:"exit_code,omitempty"`
	TimedOut bool   `json:"timed_out,omitempty"`
	Output   string `json:"output"`
	Trimmed  bool   `json:"trimmed,omitempty"`
}

const machineRunOutputLimit = 64 << 10

var ansiSequence = regexp.MustCompile(`\x1b(\[[0-9;?]*[ -/]*[@-~]|\][^\x07\x1b]*(\x07|\x1b\\)|[()][A-Za-z0-9]|[=>78])`)

// cleanTerminalOutput turns a PTY transcript into plain text for a model.
func cleanTerminalOutput(b []byte) (string, bool) {
	s := ansiSequence.ReplaceAllString(string(b), "")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.ToValidUTF8(s, "")
	trimmed := false
	if len(s) > machineRunOutputLimit {
		s, trimmed = s[len(s)-machineRunOutputLimit:], true
		for !utf8.ValidString(s) && len(s) > 0 {
			s = s[1:]
		}
	}
	return strings.TrimSpace(s), trimmed
}

// runMachineCommand runs one command through Loom's terminals (local PTY, saved
// SSH machine or paired node with the terminal module), waits for it and
// returns its output. A timeout ends the command. The terminal is removed.
func runMachineCommand(ctx context.Context, a machineRunArgs) (machineRunResult, error) {
	machine := strings.TrimSpace(a.Machine)
	if machine == "" {
		machine = "local"
	}
	command := strings.TrimSpace(a.Command)
	if command == "" {
		return machineRunResult{}, errors.New("command is required")
	}
	timeout := time.Duration(a.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	timeout = min(timeout, 600*time.Second)
	t, err := openTerminalContext(ctx, machine, strings.TrimSpace(a.Dir), command, "agent: "+command)
	if err != nil {
		return machineRunResult{}, err
	}
	defer func() {
		_ = t.proc.Close()
		select {
		case <-t.done:
		case <-time.After(3 * time.Second):
		}
		terminals.Lock()
		delete(terminals.byID, t.ID)
		terminals.Unlock()
	}()
	res := machineRunResult{Machine: machine}
	wait := time.NewTimer(timeout)
	defer wait.Stop()
	select {
	case <-t.done:
		code := t.snapshot().ExitCode
		res.ExitCode = &code
	case <-wait.C:
		res.TimedOut = true
	case <-ctx.Done():
		return machineRunResult{}, ctx.Err()
	}
	t.mu.Lock()
	out := append([]byte(nil), t.scroll...)
	t.mu.Unlock()
	res.Output, res.Trimmed = cleanTerminalOutput(out)
	return res, nil
}

const agentGuide = "Loom connects this agent to the person's machines and agent sessions. " +
	"loom_machines lists the machines; loom_machine_run runs a command on one and returns its output. " +
	"agents__* tools (when available) start Codeman agent sessions in any installed CLI, send them prompts, wait for them and read their answers: delete the sessions you start once done. " +
	"brain_* tools read and write the person's second brain. A call can wait for the person's approval when Loom's permissions ask for it."

func registerLoomAgentTools(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{Name: "loom_overview", Description: "Start here: what this Loom offers to agents, its machines, which machine provides each brick (engine, voice, codeman, files) and whether agent sessions are available.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}},
		func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, agentOverview, error) {
			return nil, agentOverview{Loom: Version, Machines: agentMachines(), Roles: machineRoles(), Agents: agentsStatus(), Guide: agentGuide}, nil
		})
	mcp.AddTool(server, &mcp.Tool{Name: "loom_machines", Description: "List the machines Loom can reach: this machine (id local), paired Loom nodes and saved SSH machines, with their modules and whether a command can run there.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}},
		func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, struct {
			Machines []agentMachine `json:"machines"`
		}, error) {
			return nil, struct {
				Machines []agentMachine `json:"machines"`
			}{agentMachines()}, nil
		})
	mcp.AddTool(server, &mcp.Tool{Name: "loom_machine_run", Description: "Run one shell command on a machine (see loom_machines) and return its exit code and output (plain text, last 64 KiB). Non-interactive: the command must not wait for input. Stopped after timeout_seconds (default 60, at most 600).", Annotations: &mcp.ToolAnnotations{DestructiveHint: ptr(true), OpenWorldHint: ptr(true)}},
		func(ctx context.Context, req *mcp.CallToolRequest, args machineRunArgs) (*mcp.CallToolResult, machineRunResult, error) {
			res, err := runMachineCommand(ctx, args)
			return nil, res, err
		})
}

func ptr[T any](v T) *T { return &v }
