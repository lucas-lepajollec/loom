package loom

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
)

const portableMCPPath = ".loom/mcp.json"
const portableMCPLimit = 4 << 20

func portableMCPRoot() (*os.Root, error) {
	_, dir, err := primarySecondBrain()
	if err != nil {
		return nil, err
	}
	return os.OpenRoot(dir)
}
func readPortableMCP(root *os.Root) (map[string]MCPServerConfig, error) {
	f, err := root.Open(portableMCPPath)
	if os.IsNotExist(err) {
		return map[string]MCPServerConfig{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, portableMCPLimit+1))
	if err != nil {
		return nil, err
	}
	if len(data) > portableMCPLimit {
		return nil, errors.New("portable MCP file exceeds 4 MiB")
	}
	var doc struct {
		Servers map[string]MCPServerConfig `json:"mcpServers"`
	}
	if err = json.Unmarshal(data, &doc); err != nil || doc.Servers == nil {
		return nil, errors.New("invalid portable MCP file")
	}
	return doc.Servers, nil
}
func portableMCPConfig(cfg MCPServerConfig, importing bool) MCPServerConfig {
	marker := "${secret}"
	if importing {
		marker = ""
		cfg.Enabled = false
	}
	env, headers := map[string]string{}, map[string]string{}
	for k := range cfg.Env {
		env[k] = marker
	}
	for k := range cfg.Headers {
		headers[k] = marker
	}
	cfg.Env, cfg.Headers = env, headers
	return cfg
}

// Missing portable entries survive adoption on a fresh installation. Explicit
// local deletions remove their previous export, without erasing other entries.
func syncPortableMCP() {
	mcpConfigMu.Lock()
	defer mcpConfigMu.Unlock()
	if definitions, err := loadMCPConfigLocked(); err == nil {
		exportPortableMCP(definitions, nil)
	}
}

func exportPortableMCP(servers, previous map[string]MCPServerConfig) {
	root, err := portableMCPRoot()
	if err != nil {
		return
	}
	defer root.Close()
	portable, err := readPortableMCP(root)
	if err != nil {
		slog.Warn("portable MCP export unavailable")
		return
	}
	for name := range previous {
		if _, ok := servers[name]; !ok {
			delete(portable, name)
		}
	}
	for name, cfg := range servers {
		portable[name] = portableMCPConfig(cfg, false)
	}
	// Also sanitize entries adopted from the portable file itself.
	for name, cfg := range portable {
		portable[name] = portableMCPConfig(cfg, false)
	}
	data, err := json.MarshalIndent(struct {
		Servers map[string]MCPServerConfig `json:"mcpServers"`
	}{portable}, "", "  ")
	if len(data) > portableMCPLimit {
		err = errors.New("portable MCP file exceeds 4 MiB")
	}
	if err == nil {
		err = gatewayRootWrite(root, portableMCPPath, append(data, '\n'))
	}
	if err != nil {
		slog.Warn("portable MCP export failed")
	}
}

type portableMCPEntry struct {
	Name   string          `json:"name"`
	Config MCPServerConfig `json:"config"`
}

func missingPortableMCP() ([]portableMCPEntry, error) {
	root, err := portableMCPRoot()
	if err != nil {
		return nil, err
	}
	defer root.Close()
	portable, err := readPortableMCP(root)
	if err != nil {
		return nil, err
	}
	local, err := LoadMCPConfig()
	if err != nil {
		return nil, err
	}
	out := []portableMCPEntry{}
	for _, name := range sortedServerNames(portable) {
		if _, ok := local[name]; !ok {
			out = append(out, portableMCPEntry{name, portableMCPConfig(portable[name], false)})
		}
	}
	return out, nil
}
func importPortableMCP(names []string) ([]string, error) {
	if len(names) == 0 || len(names) > 128 {
		return nil, errors.New("select 1–128 portable MCP servers")
	}
	root, err := portableMCPRoot()
	if err != nil {
		return nil, err
	}
	defer root.Close()
	portable, err := readPortableMCP(root)
	if err != nil {
		return nil, err
	}
	mcpConfigMu.Lock()
	defer mcpConfigMu.Unlock()
	local, err := loadMCPConfigForWriteLocked()
	if err != nil {
		return nil, err
	}
	imported := []string{}
	seen := map[string]bool{}
	for _, name := range names {
		if strings.TrimSpace(name) != name || name == "" || strings.Contains(name, "__") {
			return nil, errors.New("invalid MCP server name")
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		if _, ok := local[name]; ok {
			return nil, errors.New("MCP server already exists locally")
		}
		cfg, ok := portable[name]
		if !ok {
			return nil, errors.New("portable MCP server not found")
		}
		cfg = portableMCPConfig(cfg, true)
		if err := cfg.Validate(); err != nil {
			return nil, err
		}
		local[name] = cfg
		imported = append(imported, name)
	}
	if err := saveMCPConfigLocked(local); err != nil {
		return nil, err
	}
	return imported, nil
}
func handleMCPPortable(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, "GET") {
		return
	}
	entries, err := missingPortableMCP()
	brainResponse(w, map[string]any{"servers": entries}, err)
}
func handleMCPPortableImport(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, "POST") {
		return
	}
	var req struct {
		Names []string `json:"names"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	names, err := importPortableMCP(req.Names)
	brainResponse(w, map[string]any{"ok": true, "imported": names}, err)
}
