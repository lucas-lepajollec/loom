package loom

import (
	"bytes"
	"encoding/json"

	"github.com/lucas-lepajollec/loom/internal/loom/tools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The application owns one pool for the native tool loop and its service lifetime.
var mcpMgr = &mcpManager{tools.NewMCPManager(nativeMCPConfig{}, mcpConnect)}

type nativeMCPConfig struct{}

func (nativeMCPConfig) LoadMCPConfig() (map[string]MCPServerConfig, error) { return LoadMCPConfig() }

const catalogBucket = "mcp_tool_catalog"
const catalogSizeLimit = 1 << 20

type catalogRecord struct {
	Hash  string      `json:"hash"`
	Tools []*mcp.Tool `json:"tools"`
}

func (nativeMCPConfig) LoadCatalog(name, hash string) []*mcp.Tool {
	raw := getBytes(catalogBucket, name)
	var record catalogRecord
	if len(raw) > catalogSizeLimit || json.Unmarshal(raw, &record) != nil || record.Hash != hash {
		return nil
	}
	for _, t := range record.Tools {
		if t == nil || t.Name == "" {
			return nil
		}
	}
	return record.Tools
}
func (nativeMCPConfig) SaveCatalog(name, hash string, cfg MCPServerConfig, definitions []*mcp.Tool) {
	metadata := make([]*mcp.Tool, 0, len(definitions))
	for _, t := range definitions {
		metadata = append(metadata, &mcp.Tool{Name: t.Name, Title: t.Title, Description: t.Description, InputSchema: t.InputSchema, Annotations: t.Annotations})
	}
	raw, err := json.Marshal(catalogRecord{hash, metadata})
	if err != nil || len(raw) > catalogSizeLimit {
		return
	}
	// Some upstreams echo credentials in schema defaults or descriptions.
	for _, values := range []map[string]string{cfg.Env, cfg.Headers} {
		for _, value := range values {
			quoted, _ := json.Marshal(value)
			if value != "" && bytes.Contains(raw, quoted[1:len(quoted)-1]) {
				_ = putBytes(catalogBucket, name, nil)
				return
			}
		}
	}
	_ = putBytes(catalogBucket, name, raw)
}
func (nativeMCPConfig) ForgetCatalog(name string) { _ = putBytes(catalogBucket, name, nil) }

func MCPPrewarm() { _, _, _ = mcpMgr.GatewayTools() }
