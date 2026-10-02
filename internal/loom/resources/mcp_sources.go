package resources

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"sort"
)

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

type LinkedMCPServer struct {
	Row    MCPSourceServer
	Config MCPServerConfig
}

func MCPStringKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// Claude's project scope is part of the selector so duplicate names are never
// conflated. The file remains a single linked source in the persisted list.
func MCPSourceSelector(path, project string) string {
	if project == "" {
		return path
	}
	return path + "#project=" + url.QueryEscape(project)
}

// Sharing environment values requires explicit with_env. Headers always become
// empty placeholders: with_env is not consent to copy other credentials.
func AdoptedMCPDefinition(cfg MCPServerConfig, withEnv bool) (MCPServerConfig, error) {
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

func ReadMCPSource(source MCPSource) ([]LinkedMCPServer, error) {
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
		return nil, MCPJSONError(err)
	}
	if top == nil {
		return nil, fmt.Errorf("source MCP invalide : objet attendu")
	}
	result := []LinkedMCPServer{}
	add := func(raw json.RawMessage, project string) error {
		var entries map[string]json.RawMessage
		if err := json.Unmarshal(raw, &entries); err != nil {
			return MCPJSONError(err)
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
				return MCPJSONError(err)
			}
			transport := cfg.Type
			if transport == "" {
				transport = cfg.Transport()
			}
			result = append(result, LinkedMCPServer{Row: MCPSourceServer{Source: MCPSourceSelector(source.Path, project), Label: source.Label, Name: name, Project: project, Transport: transport, EnvNames: MCPStringKeys(cfg.Env), HeaderNames: MCPStringKeys(cfg.Headers), ReadOnly: true}, Config: cfg})
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
			return nil, MCPJSONError(err)
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

// AdoptHarnessMCP copies a reported launch definition without native credentials.
func AdoptHarnessMCP(command, url string, args, envNames []string) (MCPServerConfig, error) {
	if url != "" {
		return AdoptedMCPDefinition(MCPServerConfig{URL: url}, false)
	}
	if command == "" {
		return MCPServerConfig{}, fmt.Errorf("définition incomplète : ce harness ne dit pas comment lancer ce serveur")
	}
	env := map[string]string{}
	for _, n := range envNames {
		if n != "PATH" && n != "HOME" {
			env[n] = ""
		}
	}
	return AdoptedMCPDefinition(MCPServerConfig{Command: command, Args: args, Env: env}, false)
}
