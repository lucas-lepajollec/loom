package loom

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const mcpSourcesState = "mcp_sources"

func mcpSourcePath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("chemin MCP requis")
	}
	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if path == "~" {
			path = home
		} else {
			path = filepath.Join(home, path[2:])
		}
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	// Canonicalize existing symlinks; linked files are never written by Loom.
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	return filepath.Clean(path), nil
}

func loadMCPSourcesLocked() ([]MCPSource, error) {
	data, err := getBytesErr(bkState, mcpSourcesState)
	if err != nil {
		return nil, fmt.Errorf("lecture des sources MCP impossible")
	}
	sources := []MCPSource{}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &sources); err != nil {
			return nil, fmt.Errorf("liste des sources MCP invalide")
		}
	}
	return sources, nil
}

func linkMCPSource(source MCPSource, unlink bool) error {
	path, err := mcpSourcePath(source.Path)
	if err != nil {
		return err
	}
	if !unlink {
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("fichier source MCP inaccessible")
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("la source MCP doit être un fichier")
		}
		if path == mcpFilePath() {
			return fmt.Errorf("le fichier MCP Loom ne peut pas être une source liée")
		}
	}
	mcpConfigMu.Lock()
	defer mcpConfigMu.Unlock()
	sources, err := loadMCPSourcesLocked()
	if err != nil {
		return err
	}
	next := []MCPSource{}
	for _, s := range sources {
		if s.Path != path {
			next = append(next, s)
		}
	}
	if !unlink {
		source.Path = path
		source.Label = strings.TrimSpace(source.Label)
		if source.Label == "" {
			source.Label = filepath.Base(path)
		}
		next = append(next, source)
	}
	return putJSON(bkState, mcpSourcesState, next)
}

func mcpSourcesSnapshot() ([]MCPSourceStatus, []MCPSource, error) {
	mcpConfigMu.Lock()
	sources, err := loadMCPSourcesLocked()
	mcpConfigMu.Unlock()
	if err != nil {
		return nil, nil, err
	}
	statuses := []MCPSourceStatus{}
	linked := map[string]bool{}
	for _, s := range sources {
		linked[s.Path] = true
		status := MCPSourceStatus{MCPSource: s, Servers: []MCPSourceServer{}}
		entries, err := readMCPSource(s)
		if err != nil {
			status.Error = err.Error()
		} else {
			for _, entry := range entries {
				status.Servers = append(status.Servers, entry.Row)
			}
		}
		statuses = append(statuses, status)
	}
	suggested := []MCPSource{}
	candidates := []MCPSource{}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, MCPSource{Path: filepath.Join(home, ".claude.json"), Label: "Claude Code"}, MCPSource{Path: filepath.Join(home, ".cursor", "mcp.json"), Label: "Cursor"})
	}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, MCPSource{Path: filepath.Join(cwd, ".mcp.json"), Label: "Projet (MCP)"}, MCPSource{Path: filepath.Join(cwd, ".vscode", "mcp.json"), Label: "VS Code"})
	}
	for _, candidate := range candidates {
		path, err := mcpSourcePath(candidate.Path)
		if err != nil || linked[path] {
			continue
		}
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			candidate.Path = path
			suggested = append(suggested, candidate)
			linked[path] = true
		}
	}
	return statuses, suggested, nil
}

func adoptLinkedMCP(source, name string, withEnv bool) (string, []string, error) {
	mcpConfigMu.Lock()
	sources, err := loadMCPSourcesLocked()
	mcpConfigMu.Unlock()
	if err != nil {
		return "", nil, err
	}
	var found *MCPServerConfig
	for _, s := range sources {
		// Read only the explicitly selected linked file, never an arbitrary path.
		if source != s.Path && !strings.HasPrefix(source, s.Path+"#project=") {
			continue
		}
		entries, err := readMCPSource(s)
		if err != nil {
			return "", nil, err
		}
		for _, entry := range entries {
			if entry.Row.Source == source && entry.Row.Name == name {
				if found != nil {
					return "", nil, fmt.Errorf("serveur source MCP ambigu")
				}
				cfg := entry.Config
				found = &cfg
			}
		}
	}
	if found == nil {
		return "", nil, fmt.Errorf("serveur introuvable dans les sources MCP liées")
	}
	cfg, err := adoptedMCPDefinition(*found, withEnv)
	if err != nil {
		return "", nil, err
	}
	return storeAdoptedMCP(name, cfg)
}

// Shared with harness adoption. Collision checking and writing hold one lock.
func storeAdoptedMCP(name string, cfg MCPServerConfig) (string, []string, error) {
	name = strings.Trim(mcpNameRe.ReplaceAllString(name, "-"), "-")
	// '__' is reserved by the MCP tool namespace.
	for strings.Contains(name, "__") {
		name = strings.ReplaceAll(name, "__", "_")
	}
	if name == "" {
		return "", nil, fmt.Errorf("nom de serveur MCP vide après normalisation")
	}
	mcpConfigMu.Lock()
	servers, err := loadMCPConfigForWriteLocked()
	if err == nil {
		if _, exists := servers[name]; exists {
			err = errMCPAdoptConflict
		} else {
			servers[name] = cfg
			err = saveMCPConfigLocked(servers)
		}
	}
	mcpConfigMu.Unlock()
	if err != nil {
		return "", nil, err
	}
	mcpInvalidate(name)
	missing := []string{}
	for k, v := range cfg.Env {
		if v == "" {
			missing = append(missing, k)
		}
	}
	sort.Strings(missing)
	return name, missing, nil
}

var errMCPAdoptConflict = fmt.Errorf("un serveur MCP Loom porte déjà ce nom")
