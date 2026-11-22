package loom

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
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
