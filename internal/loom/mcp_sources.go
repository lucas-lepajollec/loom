package loom

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const mcpSourcesState = "mcp_sources"

type MCPSource struct {
	Path  string `json:"path"`
	Label string `json:"label"`
}

type MCPSourceServer struct {
	Source      string   `json:"source"`
	Label       string   `json:"label"`
	Name        string   `json:"name"`
	Project     string   `json:"project,omitempty"`
	Transport   string   `json:"transport"`
	EnvNames    []string `json:"env_names"`
	HeaderNames []string `json:"header_names"`
	ReadOnly    bool     `json:"read_only"`
}

type MCPSourceStatus struct {
	MCPSource
	Servers []MCPSourceServer `json:"servers"`
	Error   string            `json:"error,omitempty"`
}

type linkedMCPServer struct {
	row MCPSourceServer
	cfg MCPServerConfig
}

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

func mcpStringKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// Claude's project scope is part of the selector so duplicate names are never
// conflated. The file remains a single linked source in the persisted list.
func mcpSourceSelector(path, project string) string {
	if project == "" {
		return path
	}
	return path + "#project=" + url.QueryEscape(project)
}

func readMCPSource(source MCPSource) ([]linkedMCPServer, error) {
	file, err := os.Open(source.Path)
	if err != nil {
		return nil, fmt.Errorf("fichier source MCP inaccessible")
	}
	defer file.Close()
	const limit = 4 << 20
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, fmt.Errorf("lecture du fichier source MCP impossible")
	}
	if len(data) > limit {
		return nil, fmt.Errorf("fichier source MCP trop volumineux (maximum 4 Mio)")
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, mcpJSONError(err)
	}
	if top == nil {
		return nil, fmt.Errorf("source MCP invalide : objet attendu")
	}
	result := []linkedMCPServer{}
	add := func(raw json.RawMessage, project string) error {
		var entries map[string]json.RawMessage
		if err := json.Unmarshal(raw, &entries); err != nil {
			return mcpJSONError(err)
		}
		if entries == nil {
			return fmt.Errorf("source MCP invalide : serveurs doivent être un objet")
		}
		names := make([]string, 0, len(entries))
		for n := range entries {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, name := range names {
			var cfg MCPServerConfig
			if err := json.Unmarshal(entries[name], &cfg); err != nil {
				return mcpJSONError(err)
			}
			transport := cfg.Type
			if transport == "" {
				transport = cfg.Transport()
			}
			result = append(result, linkedMCPServer{row: MCPSourceServer{Source: mcpSourceSelector(source.Path, project), Label: source.Label, Name: name, Project: project, Transport: transport, EnvNames: mcpStringKeys(cfg.Env), HeaderNames: mcpStringKeys(cfg.Headers), ReadOnly: true}, cfg: cfg})
		}
		return nil
	}
	recognized := false
	for _, key := range []string{"mcpServers", "servers"} {
		if raw, ok := top[key]; ok {
			recognized = true
			if err := add(raw, ""); err != nil {
				return nil, err
			}
		}
	}
	if raw, ok := top["projects"]; ok {
		recognized = true
		var projects map[string]map[string]json.RawMessage
		if err := json.Unmarshal(raw, &projects); err != nil {
			return nil, mcpJSONError(err)
		}
		dirs := make([]string, 0, len(projects))
		for dir := range projects {
			dirs = append(dirs, dir)
		}
		sort.Strings(dirs)
		for _, dir := range dirs {
			if raw, ok := projects[dir]["mcpServers"]; ok {
				if err := add(raw, dir); err != nil {
					return nil, err
				}
			}
		}
	}
	if !recognized {
		return nil, fmt.Errorf("format de source MCP non reconnu")
	}
	return result, nil
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
				status.Servers = append(status.Servers, entry.row)
			}
		}
		statuses = append(statuses, status)
	}
	suggested := []MCPSource{}
	candidates := []MCPSource{}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, MCPSource{filepath.Join(home, ".claude.json"), "Claude Code"}, MCPSource{filepath.Join(home, ".cursor", "mcp.json"), "Cursor"})
	}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, MCPSource{filepath.Join(cwd, ".mcp.json"), "Projet (MCP)"}, MCPSource{filepath.Join(cwd, ".vscode", "mcp.json"), "VS Code"})
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

// Sharing environment values requires explicit with_env. Headers always become
// empty placeholders: with_env is not consent to copy other credentials.
func adoptedMCPDefinition(cfg MCPServerConfig, withEnv bool) (MCPServerConfig, error) {
	if err := cfg.Validate(); err != nil {
		return MCPServerConfig{}, err
	}
	cfg.Enabled = false
	if !withEnv {
		env := map[string]string{}
		for k := range cfg.Env {
			env[k] = ""
		}
		cfg.Env = env
	}
	headers := map[string]string{}
	for k := range cfg.Headers {
		headers[k] = ""
	}
	cfg.Headers = headers
	return cfg, nil
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
			if entry.row.Source == source && entry.row.Name == name {
				if found != nil {
					return "", nil, fmt.Errorf("serveur source MCP ambigu")
				}
				cfg := entry.cfg
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
