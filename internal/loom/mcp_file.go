package loom

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"time"
)

const mcpFileMigrated = "mcp_file_migrated"

// The old state/mcp value is retained as a recovery backup. Only the file is
// authoritative after a successful migration; deleting it never reimports it.
var mcpFileCache struct {
	path    string
	info    os.FileInfo
	top     map[string]json.RawMessage
	entries map[string]json.RawMessage
	servers map[string]MCPServerConfig
	err     error
}

func mcpFilePath() string { return filepath.Join(LoomHome(), "mcp.json") }

// Never echo JSON decoder errors: unknown field names may themselves be secrets.
func mcpJSONError(err error) error {
	if e, ok := err.(*json.SyntaxError); ok {
		return fmt.Errorf("JSON MCP invalide (octet %d)", e.Offset)
	}
	return fmt.Errorf("configuration MCP invalide : objet ou types de champs incorrects")
}

func parseMCPFile(data []byte) (map[string]json.RawMessage, map[string]json.RawMessage, map[string]MCPServerConfig, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, nil, nil, mcpJSONError(err)
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
		return nil, nil, nil, mcpJSONError(err)
	}
	if entries == nil {
		return nil, nil, nil, fmt.Errorf("configuration MCP invalide : mcpServers doit être un objet")
	}
	servers := map[string]MCPServerConfig{}
	for name, entry := range entries {
		var object map[string]json.RawMessage
		if err := json.Unmarshal(entry, &object); err != nil {
			return nil, nil, nil, mcpJSONError(err)
		}
		if object == nil {
			return nil, nil, nil, fmt.Errorf("configuration MCP invalide : serveur doit être un objet")
		}
		var cfg MCPServerConfig
		if err := json.Unmarshal(entry, &cfg); err != nil {
			return nil, nil, nil, mcpJSONError(err)
		}
		if err := cfg.Validate(); err != nil {
			return nil, nil, nil, err
		}
		servers[name] = cfg
	}
	return top, entries, servers, nil
}

func sameMCPFile(a, b os.FileInfo) bool {
	return a != nil && b != nil && a.ModTime().Equal(b.ModTime()) && a.Size() == b.Size() && os.SameFile(a, b)
}

func loadMCPConfigLocked() (map[string]MCPServerConfig, error) {
	path := mcpFilePath()
	if mcpFileCache.path != path {
		mcpFileCache.path = path
		mcpFileCache.info = nil
		mcpFileCache.top = nil
		mcpFileCache.entries = nil
		mcpFileCache.servers = nil
		mcpFileCache.err = nil
	}
	info, err := os.Stat(path)
	if os.IsNotExist(err) && mcpFileCache.servers == nil {
		migrated, readErr := getBytesErr(bkState, mcpFileMigrated)
		if readErr != nil {
			err = fmt.Errorf("lecture de migration MCP impossible")
		} else {
			legacy := []byte(`{}`)
			if len(migrated) == 0 {
				legacy, readErr = getBytesErr(bkState, "mcp")
				if len(legacy) == 0 {
					legacy = []byte(`{}`)
				}
			}
			if readErr != nil {
				err = fmt.Errorf("lecture de sauvegarde MCP impossible")
			} else {
				data := append(append([]byte(`{"mcpServers":`), legacy...), '}')
				_, _, _, err = parseMCPFile(data)
				if err == nil {
					err = memWriteFileAtomic(path, append(data, '\n'), 0600)
				}
				if err == nil {
					err = putBool(bkState, mcpFileMigrated, true)
				}
				if err == nil {
					info, err = os.Stat(path)
				}
			}
		}
	}
	if err == nil && !sameMCPFile(info, mcpFileCache.info) {
		var data []byte
		data, err = os.ReadFile(path)
		if err == nil {
			var top, entries map[string]json.RawMessage
			var servers map[string]MCPServerConfig
			top, entries, servers, err = parseMCPFile(data)
			if err == nil {
				// Restrict the owned secret-bearing file; preserve its contents.
				err = os.Chmod(path, 0600)
				if err == nil {
					err = putBool(bkState, mcpFileMigrated, true)
				}
				if err == nil {
					for name, old := range mcpFileCache.servers {
						if next, ok := servers[name]; !ok || !reflect.DeepEqual(old, next) {
							mcpInvalidate(name)
						}
					}
					mcpFileCache.top, mcpFileCache.entries, mcpFileCache.servers = top, entries, servers
					mcpFileCache.info = info
				}
			}
		}
	}
	mcpFileCache.err = err
	if mcpFileCache.servers == nil {
		return map[string]MCPServerConfig{}, err
	}
	// Detached snapshot: callers may mutate maps/slices without changing the cache.
	data, _ := json.Marshal(mcpFileCache.servers)
	var result map[string]MCPServerConfig
	_ = json.Unmarshal(data, &result)
	return result, nil
}

func loadMCPConfigForWriteLocked() (map[string]MCPServerConfig, error) {
	servers, err := loadMCPConfigLocked()
	if err == nil {
		err = mcpFileCache.err
	}
	return servers, err
}

func saveMCPConfigLocked(servers map[string]MCPServerConfig) error {
	// Also used directly by existing tests: initialize/check the current file first.
	before := mcpFileCache.info
	path := mcpFilePath()
	checkRevision := mcpFileCache.path == path && before != nil
	if _, err := loadMCPConfigForWriteLocked(); err != nil {
		return err
	}
	if checkRevision && !sameMCPFile(before, mcpFileCache.info) {
		return fmt.Errorf("le fichier MCP a changé pendant la modification ; réessayez")
	}
	top := map[string]json.RawMessage{}
	for k, v := range mcpFileCache.top {
		top[k] = v
	}
	entries := map[string]json.RawMessage{}
	for name, cfg := range servers {
		fields := map[string]json.RawMessage{}
		if old := mcpFileCache.entries[name]; old != nil {
			_ = json.Unmarshal(old, &fields)
		}
		for _, k := range []string{"command", "args", "env", "url", "headers", "enabled", "disabledTools", "type"} {
			delete(fields, k)
		}
		data, err := json.Marshal(cfg)
		if err != nil {
			return err
		}
		var known map[string]json.RawMessage
		_ = json.Unmarshal(data, &known)
		for k, v := range known {
			fields[k] = v
		}
		entries[name], err = json.Marshal(fields)
		if err != nil {
			return err
		}
	}
	data, err := json.Marshal(entries)
	if err != nil {
		return err
	}
	top["mcpServers"] = data
	data, err = json.MarshalIndent(top, "", "  ")
	if err != nil {
		return err
	}
	if err = memWriteFileAtomic(mcpFilePath(), append(data, '\n'), 0600); err != nil {
		return err
	}
	// Keep the successfully written configuration as the latest good snapshot,
	// even if an editor immediately replaces it with invalid JSON.
	top, entries, saved, _ := parseMCPFile(data)
	mcpFileCache.top, mcpFileCache.entries, mcpFileCache.servers = top, entries, saved
	mcpFileCache.info, _ = os.Stat(path)
	mcpFileCache.err = nil
	return nil
}

// MCPFileStatus contains metadata only, never configuration secrets.
type MCPFileStatus struct {
	Path  string     `json:"path"`
	Error string     `json:"error,omitempty"`
	Mtime *time.Time `json:"mtime"`
}

func mcpFileStatus() MCPFileStatus {
	mcpConfigMu.Lock()
	defer mcpConfigMu.Unlock()
	_, _ = loadMCPConfigLocked()
	status := MCPFileStatus{Path: mcpFilePath()}
	if info, err := os.Stat(status.Path); err == nil {
		t := info.ModTime().UTC()
		status.Mtime = &t
	}
	if mcpFileCache.err != nil {
		status.Error = mcpFileCache.err.Error()
	}
	return status
}
