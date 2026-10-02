package loom

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	"github.com/lucas-lepajollec/loom/internal/loom/resources"
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
			err = fmt.Errorf("could not read MCP migration")
		} else {
			legacy := []byte(`{}`)
			if len(migrated) == 0 {
				legacy, readErr = getBytesErr(bkState, "mcp")
				if len(legacy) == 0 {
					legacy = []byte(`{}`)
				}
			}
			if readErr != nil {
				err = fmt.Errorf("could not read MCP backup")
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
		return fmt.Errorf("the MCP file changed during editing; try again")
	}
	data, err := resources.EncodeMCPFile(mcpFileCache.top, mcpFileCache.entries, servers)
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
