package resources

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// Never echo JSON decoder errors: unknown field names may themselves be secrets.
func MCPJSONError(err error) error {
	if e, ok := err.(*json.SyntaxError); ok {
		return fmt.Errorf("JSON MCP invalide (octet %d)", e.Offset)
	}
	return fmt.Errorf("configuration MCP invalide : objet ou types de champs incorrects")
}

func ParseMCPFile(data []byte) (map[string]json.RawMessage, map[string]json.RawMessage, map[string]MCPServerConfig, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, nil, nil, MCPJSONError(err)
	}
	if top == nil {
		return nil, nil, nil, fmt.Errorf("configuration MCP invalide : objet attendu")
	}
	raw, ok := top["mcpServers"]
	if !ok {
		return nil, nil, nil, fmt.Errorf("configuration MCP invalide : mcpServers manquant")
	}
	var entries map[string]json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, nil, nil, MCPJSONError(err)
	}
	if entries == nil {
		return nil, nil, nil, fmt.Errorf("configuration MCP invalide : mcpServers doit être un objet")
	}
	servers := map[string]MCPServerConfig{}
	for name, entry := range entries {
		var object map[string]json.RawMessage
		if err := json.Unmarshal(entry, &object); err != nil {
			return nil, nil, nil, MCPJSONError(err)
		}
		if object == nil {
			return nil, nil, nil, fmt.Errorf("configuration MCP invalide : serveur doit être un objet")
		}
		var cfg MCPServerConfig
		if err := json.Unmarshal(entry, &cfg); err != nil {
			return nil, nil, nil, MCPJSONError(err)
		}
		if err := cfg.Validate(); err != nil {
			return nil, nil, nil, err
		}
		servers[name] = cfg
	}
	return top, entries, servers, nil
}

func SameMCPFile(a, b os.FileInfo) bool {
	return a != nil && b != nil && a.ModTime().Equal(b.ModTime()) && a.Size() == b.Size() && os.SameFile(a, b)
}

// MCPFileStatus contains metadata only, never configuration secrets.
type MCPFileStatus struct {
	Path  string     `json:"path"`
	Error string     `json:"error,omitempty"`
	Mtime *time.Time `json:"mtime"`
}

// EncodeMCPFile updates only Loom's known fields, preserving extension fields.
func EncodeMCPFile(previousTop, previousEntries map[string]json.RawMessage, servers map[string]MCPServerConfig) ([]byte, error) {
	top := map[string]json.RawMessage{}
	for k, v := range previousTop {
		top[k] = v
	}
	entries := map[string]json.RawMessage{}
	for name, cfg := range servers {
		fields := map[string]json.RawMessage{}
		if old := previousEntries[name]; old != nil {
			_ = json.Unmarshal(old, &fields)
		}
		for _, k := range []string{"command", "args", "env", "url", "headers", "enabled", "disabledTools", "type"} {
			delete(fields, k)
		}
		data, err := json.Marshal(cfg)
		if err != nil {
			return nil, err
		}
		var known map[string]json.RawMessage
		_ = json.Unmarshal(data, &known)
		for k, v := range known {
			fields[k] = v
		}
		entries[name], err = json.Marshal(fields)
		if err != nil {
			return nil, err
		}
	}
	data, err := json.Marshal(entries)
	if err != nil {
		return nil, err
	}
	top["mcpServers"] = data
	data, err = json.MarshalIndent(top, "", "  ")
	if err != nil {
		return nil, err
	}
	return data, nil
}
