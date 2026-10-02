package loom

import "github.com/lucas-lepajollec/loom/internal/loom/tools"

// The application owns one pool for the native tool loop and its service lifetime.
var mcpMgr = &mcpManager{tools.NewMCPManager(nativeMCPConfig{}, mcpConnect)}

type nativeMCPConfig struct{}

func (nativeMCPConfig) LoadMCPConfig() (map[string]MCPServerConfig, error) { return LoadMCPConfig() }

// MCPPrewarm keeps background startup policy in Loom.
func MCPPrewarm() {
	servers, err := LoadMCPConfig()
	if err != nil || len(servers) == 0 {
		return
	}
	go mcpEnsureAll(servers)
}
