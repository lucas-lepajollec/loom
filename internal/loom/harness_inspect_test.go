package loom

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHarnessInspectParsers(t *testing.T) {
	claude := parseMCP("claude", "Checking MCP server health…\n\nclaude.ai Docs: https://api.example/mcp - ✔ Connected\nplugin:x:y: https://mcp.x (HTTP) - ! Needs authentication\nlocal: node s.js - ✗ Failed to connect\n")
	if len(claude) != 3 || claude[0].Status != "connected" || claude[1].Status != "needs-auth" || claude[2].Status != "error" || claude[1].Name != "plugin:x:y" {
		t.Fatalf("%+v", claude)
	}
	codex := parseMCP("codex", `[{"name":"a","enabled":true,"transport":{"type":"stdio","command":"/usr/bin/node"}},{"name":"b","enabled":false,"transport":{"type":"http","url":"https://m"}}]`)
	if len(codex) != 2 || codex[0].Target != "node" || codex[0].Status != "enabled" || codex[1].Target != "https://m" || *codex[1].Enabled {
		t.Fatalf("%+v", codex)
	}
	if got := parseMCP("lines", "No MCP servers configured.\n"); len(got) != 0 {
		t.Fatalf("%+v", got)
	}
}

func TestHarnessSkillsMarkLoomCopies(t *testing.T) {
	dir := t.TempDir()
	for name, desc := range map[string]string{"loom-review": "Read again", "mine": "Perso"} {
		_ = os.MkdirAll(filepath.Join(dir, name), 0o755)
		_ = os.WriteFile(filepath.Join(dir, name, "SKILL.md"), []byte("---\nname: "+name+"\ndescription: \""+desc+"\"\n---\nbody"), 0o644)
	}
	skills := readSkills([]string{dir})
	if len(skills) != 2 || !skills[0].FromLoom || skills[0].Description != "Read again" || skills[1].FromLoom {
		t.Fatalf("%+v", skills)
	}
}
