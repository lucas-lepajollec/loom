package loom

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHarnessMCPBindingAndDisabledServers(t *testing.T) {
	testHome(t)
	if err := SetMCPServer("on", MCPServerConfig{Command: "a", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	_ = SetMCPServer("off", MCPServerConfig{Command: "b", Enabled: false})
	_ = SetMCPServer("other", MCPServerConfig{Command: "c", Enabled: true})
	s := RuntimeSession{RuntimeID: "codex"}
	defs, err := acpSessionMCPDefinitions(s)
	if err != nil || len(defs) != 2 || defs["off"].Command != "" {
		t.Fatalf("default must pass enabled servers only: %v %v", defs, err)
	}
	only := []string{"other"}
	if err := setHarnessMCPBinding("codex", &only); err != nil {
		t.Fatal(err)
	}
	defs, _ = acpSessionMCPDefinitions(s)
	if len(defs) != 1 || defs["other"].Command != "c" {
		t.Fatalf("harness binding ignored: %v", defs)
	}
	defs, _ = acpSessionMCPDefinitions(RuntimeSession{RuntimeID: "claude-code"})
	if len(defs) != 2 {
		t.Fatalf("binding leaked to another harness: %v", defs)
	}
	bad := []string{"missing"}
	if setHarnessMCPBinding("codex", &bad) == nil {
		t.Fatal("unknown server accepted")
	}
}

func TestSkillTargetBindingExcludesOneFolder(t *testing.T) {
	testHome(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	c, err := saveCapability(Capability{Name: "Revue", Instructions: "x"})
	if err != nil {
		t.Fatal(err)
	}
	list := loadSkillSinks()
	for i := range list {
		list[i].Enabled = true
	}
	_ = saveSkillSinks(list)
	_ = putStoreJSON(bkState, skillBindingsState, map[string]map[string]bool{c.ID: {"claude": false}})
	syncSkillSinks()
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills", "loom-revue")); !os.IsNotExist(err) {
		t.Fatal("excluded skill written for Claude Code")
	}
	if _, err := os.Stat(filepath.Join(home, ".agents", "skills", "loom-revue", "SKILL.md")); err != nil {
		t.Fatal("skill missing in the other folder")
	}
}

func TestAdoptedMCPNeverCopiesSecretsAndStartsDisabled(t *testing.T) {
	cfg, err := adoptedMCP(HarnessMCP{Name: "gh", Command: "node", Args: []string{"s.js"}, EnvNames: []string{"GITHUB_TOKEN", "PATH"}})
	if err != nil || cfg.Enabled || cfg.Env["GITHUB_TOKEN"] != "" || len(cfg.Env) != 1 || cfg.Command != "node" {
		t.Fatalf("%+v %v", cfg, err)
	}
	if _, err := adoptedMCP(HarnessMCP{Name: "x"}); err == nil {
		t.Fatal("incomplete definition adopted")
	}
	if c, _ := adoptedMCP(HarnessMCP{URL: "https://m"}); c.URL != "https://m" {
		t.Fatal("http server")
	}
}
