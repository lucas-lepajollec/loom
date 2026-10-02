package loom

import (
"bytes"
"encoding/json"
"net/http"
"net/http/httptest"
"os"
"path/filepath"
"runtime"
"testing"
"time"
)

func writeMCPTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	// Deterministic change detection on filesystems with coarse timestamp precision.
	when := time.Now().Add(time.Second)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatal(err)
	}
}

func TestMCPFileMigration(t *testing.T) {
	testHome(t)
	legacy := []byte(`{"old":{"command":"node","args":["server.js"],"env":{"TOKEN":"secret"},"enabled":false,"disabledTools":["write"],"vendor":{"x":1}}}`)
	if err := putBytes(bkState, "mcp", legacy); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadMCPConfig()
	if err != nil || cfg["old"].Command != "node" || cfg["old"].Enabled || cfg["old"].Env["TOKEN"] != "secret" || !cfg["old"].ToolDisabled("write") {
		t.Fatal("migration failed", err)
	}
	data, err := os.ReadFile(mcpFilePath())
	if err != nil || !bytes.Contains(data, []byte(`"mcpServers"`)) || !bytes.Contains(data, []byte(`"vendor"`)) {
		t.Fatal("migration lost data", err)
	}
	if !bytes.Equal(getBytes(bkState, "mcp"), legacy) {
		t.Fatal("migration recovery backup lost")
	}
	info, _ := os.Stat(mcpFilePath())
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("unsafe mode %o", info.Mode().Perm())
	}
	if err := DeleteMCPServer("old"); err != nil {
		t.Fatal(err)
	}
	// Simulate restart: the obsolete database value must never come back.
	mcpConfigMu.Lock()
	mcpFileCache.path = ""
	mcpConfigMu.Unlock()
	cfg, err = LoadMCPConfig()
	if err != nil || len(cfg) != 0 {
		t.Fatal("migration repeated", err)
	}
	if err := os.Remove(mcpFilePath()); err != nil {
		t.Fatal(err)
	}
	mcpConfigMu.Lock()
	mcpFileCache.path = ""
	mcpConfigMu.Unlock()
	cfg, err = LoadMCPConfig()
	if err != nil || len(cfg) != 0 {
		t.Fatal("deleted file reimported backup", err)
	}
}

func TestMCPFileFailedMigrationKeepsBackup(t *testing.T) {
	testHome(t)
	legacy := []byte(`{"old":{"command":"node"}}`)
	if err := putBytes(bkState, "mcp", legacy); err != nil {
		t.Fatal(err)
	}
	original := memWriteFileAtomic
	memWriteFileAtomic = func(string, []byte, os.FileMode) error { return os.ErrPermission }
	_, err := LoadMCPConfig()
	memWriteFileAtomic = original
	if err == nil || getBool(bkState, mcpFileMigrated) || !bytes.Equal(getBytes(bkState, "mcp"), legacy) {
		t.Fatal("failed migration consumed backup")
	}
	cfg, err := LoadMCPConfig()
	if err != nil || cfg["old"].Command != "node" {
		t.Fatal("migration retry failed", err)
	}
}

func TestMCPFileRoundTripAndExternalEdit(t *testing.T) {
	testHome(t)
	writeMCPTestFile(t, mcpFilePath(), `{"version":17,"vendor":{"preserve":true},"mcpServers":{"local":{"command":"node","env":{"TOKEN":"secret"},"type":"stdio","custom":{"flag":true}},"remote":{"url":"https://example.test/mcp","extra":[1,2]}}}`)
	cfg, err := LoadMCPConfig()
	if err != nil || !cfg["local"].Enabled {
		t.Fatal("standard default", err)
	}
	cfg["local"].Env["TOKEN"] = "mutated"
	again, _ := LoadMCPConfig()
	if again["local"].Env["TOKEN"] != "secret" {
		t.Fatal("caller mutated cache")
	}
	if err := SetMCPServerEnabled("local", false); err != nil {
		t.Fatal(err)
	}
	if err := SetMCPToolEnabled("local", "write", false); err != nil {
		t.Fatal(err)
	}
	if err := SetMCPServer("remote", MCPServerConfig{URL: "https://changed.test/mcp", Enabled: false}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(mcpFilePath())
	var top map[string]json.RawMessage
	_ = json.Unmarshal(data, &top)
	var entries map[string]map[string]json.RawMessage
	_ = json.Unmarshal(top["mcpServers"], &entries)
	if string(top["version"]) != "17" || top["vendor"] == nil || entries["local"]["custom"] == nil || entries["remote"]["extra"] == nil || string(entries["local"]["type"]) != `"stdio"` {
		t.Fatal("unknown fields lost")
	}
	// Hold an inert session to verify external changes invalidate pooled sessions.
	inert := MCPServerConfig{Command: "loom-binaire-inexistant-pour-test"}
	session := mcpMgr.ensure("local", inert)
	t.Cleanup(mcpCloseAll)
	writeMCPTestFile(t, mcpFilePath(), `{"mcpServers":{"local":{"command":"changed","enabled":false}}}`)
	cfg, err = LoadMCPConfig()
	if err != nil || cfg["local"].Command != "changed" || len(cfg) != 1 {
		t.Fatal("external edit not loaded", err)
	}
	if mcpMgr.ensure("local", inert) == session {
		t.Fatal("external edit retained stale session")
	}
}

func TestMCPBrokenFileIsKeptAndReported(t *testing.T) {
	testHome(t)
	if err := SetMCPServer("good", MCPServerConfig{Command: "node", Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadMCPConfig(); err != nil {
		t.Fatal(err)
	}
	broken := `{"mcpServers":{"good":{"env":{"SECRET":"never-expose"}},`
	writeMCPTestFile(t, mcpFilePath(), broken)
	cfg, err := LoadMCPConfig()
	if err != nil || cfg["good"].Command != "node" {
		t.Fatal("last good config not served", err)
	}
	rr := httptest.NewRecorder()
	handleMCPFile(rr, httptest.NewRequest(http.MethodGet, "/api/mcp/file", nil))
	var status MCPFileStatus
	if err := json.Unmarshal(rr.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status.Error == "" || status.Path != mcpFilePath() || status.Mtime == nil || bytes.Contains(rr.Body.Bytes(), []byte("never-expose")) {
		t.Fatal("invalid file metadata")
	}
	if SetMCPServerEnabled("good", true) == nil || DeleteMCPServer("good") == nil || SetMCPToolEnabled("good", "tool", false) == nil || SetMCPServer("other", MCPServerConfig{Command: "node"}) == nil {
		t.Fatal("broken file accepted write")
	}
	data, _ := os.ReadFile(mcpFilePath())
	if string(data) != broken {
		t.Fatal("broken file overwritten")
	}
	writeMCPTestFile(t, mcpFilePath(), `{"mcpServers":{"fixed":{"command":"node","enabled":false}}}`)
	cfg, err = LoadMCPConfig()
	if err != nil || len(cfg) != 1 || cfg["fixed"].Command != "node" || mcpFileStatus().Error != "" {
		t.Fatal("repair not picked up", err)
	}
}

func TestMCPInitiallyBrokenFileDoesNotMigrateOverIt(t *testing.T) {
	testHome(t)
	if err := putJSON(bkState, "mcp", map[string]MCPServerConfig{"old": {Command: "node"}}); err != nil {
		t.Fatal(err)
	}
	writeMCPTestFile(t, mcpFilePath(), "{broken")
	if _, err := LoadMCPConfig(); err == nil {
		t.Fatal("initial parse error missing")
	}
	if SetMCPServer("new", MCPServerConfig{Command: "node"}) == nil {
		t.Fatal("broken file writable")
	}
	data, _ := os.ReadFile(mcpFilePath())
	if string(data) != "{broken" || getBool(bkState, mcpFileMigrated) {
		t.Fatal("broken file replaced by migration")
	}
}
