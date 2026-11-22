package loom

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestCleanTerminalOutput(t *testing.T) {
	out, trimmed := cleanTerminalOutput([]byte("\x1b[1;32mok\x1b[0m\r\n\x1b]0;title\x07line two\r\n"))
	if out != "ok\nline two" || trimmed {
		t.Fatalf("%q %v", out, trimmed)
	}
	big := strings.Repeat("é", machineRunOutputLimit)
	out, trimmed = cleanTerminalOutput([]byte(big + "end"))
	if !trimmed || !strings.HasSuffix(out, "end") || len(out) > machineRunOutputLimit || !strings.HasPrefix(out, "é") {
		t.Fatalf("trimmed=%v len=%d", trimmed, len(out))
	}
}

func TestAgentsUpstreamNeedsSavedCodemanAndCLI(t *testing.T) {
	gatewayTestHome(t)
	t.Setenv("PATH", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	if got, _ := (codemanMCPSource{}).LoadMCPConfig(); len(got) != 0 {
		t.Fatal("default discovery address must not start the agents upstream", got)
	}
	if err := saveCodemanConfig(codemanConfig{URL: "http://127.0.0.1:3000", Username: "admin"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := (codemanMCPSource{}).LoadMCPConfig(); len(got) != 0 {
		t.Fatal("no codeman command: no upstream", got)
	}
	if !strings.Contains(agentsStatus(), "not installed") {
		t.Fatal(agentsStatus())
	}
	bin := filepath.Join(t.TempDir(), "codeman")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", filepath.Dir(bin))
	got, _ := (codemanMCPSource{}).LoadMCPConfig()
	cfg, ok := got[agentsUpstream]
	if !ok || cfg.Command != bin || strings.Join(cfg.Args, " ") != "mcp --url http://127.0.0.1:3000" || !cfg.Enabled {
		t.Fatal(got)
	}
	if cfg.Env["CODEMAN_API_URL"] != "http://127.0.0.1:3000" || cfg.Env["CODEMAN_MUX"] != "" {
		t.Fatal(cfg.Env)
	}
	for _, a := range cfg.Args {
		if strings.Contains(strings.ToLower(a), "password") {
			t.Fatal("credential in arguments", cfg.Args)
		}
	}
}

func TestMachineRunLocal(t *testing.T) {
	if runtime.GOOS == "windows" || !ptySupported {
		t.Skip("local PTY required")
	}
	gatewayTestHome(t)
	t.Setenv("SHELL", "/bin/sh")
	res, err := runMachineCommand(context.Background(), machineRunArgs{Command: "printf 'a\\nb\\n'; exit 4", TimeoutSeconds: 20})
	if err != nil || res.ExitCode == nil || *res.ExitCode != 4 || res.Output != "a\nb" || res.TimedOut {
		t.Fatalf("%+v %v", res, err)
	}
	res, err = runMachineCommand(context.Background(), machineRunArgs{Command: "echo started; sleep 30", TimeoutSeconds: 1})
	if err != nil || !res.TimedOut || res.ExitCode != nil || res.Output != "started" {
		t.Fatalf("%+v %v", res, err)
	}
	if _, err := runMachineCommand(context.Background(), machineRunArgs{Command: " "}); err == nil {
		t.Fatal("empty command accepted")
	}
	terminals.Lock()
	left := len(terminals.byID)
	terminals.Unlock()
	if left != 0 {
		t.Fatal("terminal left open", left)
	}
}

func TestResolveAgentMachine(t *testing.T) {
	gatewayTestHome(t)
	if err := putJSON(bkState, remoteMachinesState, []RemoteMachine{
		{ID: "m1", Name: "NAS", Host: "192.168.0.200"},
		{ID: "m2", Name: "lab"}, {ID: "m3", Hostname: "lab"},
	}); err != nil {
		t.Fatal(err)
	}
	for in, want := range map[string]string{"": "local", " local ": "local", "m1": "m1", "nas": "m1", "192.168.0.200": "m1"} {
		if got, err := resolveAgentMachine(in); err != nil || got != want {
			t.Fatalf("%q: %q %v", in, got, err)
		}
	}
	if _, err := resolveAgentMachine("lab"); err == nil || !strings.Contains(err.Error(), "several") {
		t.Fatal("ambiguous name accepted", err)
	}
	if _, err := resolveAgentMachine("nowhere"); err == nil || !strings.Contains(err.Error(), "loom_machines") {
		t.Fatal(err)
	}
}

func TestGatewayUseResetsWithANewToken(t *testing.T) {
	gatewayTestHome(t)
	noteGatewayUse("claude-code") // no token: nothing to record
	if s, _ := loadGatewayState(); s.LastClient != "" {
		t.Fatal(s)
	}
	if _, err := rotateGatewayToken(); err != nil {
		t.Fatal(err)
	}
	noteGatewayUse("claude-code")
	noteGatewayUse("") // a tool call keeps the client name
	if s, _ := loadGatewayState(); s.LastClient != "claude-code" || s.LastUsed == 0 {
		t.Fatal(s)
	}
	if _, err := rotateGatewayToken(); err != nil {
		t.Fatal(err)
	}
	if s, _ := loadGatewayState(); s.LastClient != "" || s.LastUsed != 0 {
		t.Fatal("new token kept the old use", s)
	}
}

func TestGatewayUsageReadsTheHandshakeClient(t *testing.T) {
	gatewayTestHome(t)
	if _, err := rotateGatewayToken(); err != nil {
		t.Fatal(err)
	}
	next := func(context.Context, string, mcp.Request) (mcp.Result, error) { return nil, nil }
	req := &mcp.ServerRequest[*mcp.InitializeParams]{Params: &mcp.InitializeParams{ClientInfo: &mcp.Implementation{Name: "codex-mcp-client"}}}
	if _, err := gatewayUsageMiddleware(next)(context.Background(), "initialize", req); err != nil {
		t.Fatal(err)
	}
	if s, _ := loadGatewayState(); s.LastClient != "codex-mcp-client" {
		t.Fatal("older-protocol handshake client not recorded", s)
	}
}
