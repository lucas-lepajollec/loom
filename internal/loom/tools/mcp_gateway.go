package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type MCPGatewayTool struct {
	Server string
	Tool   *mcpsdk.Tool
}
type MCPGatewayStatus struct {
	Name      string `json:"name"`
	Enabled   bool   `json:"enabled"`
	Connected bool   `json:"connected"`
	Tools     int    `json:"tools"`
	Error     string `json:"error,omitempty"`
}

func (m *MCPManager) GatewayTools() ([]MCPGatewayTool, []MCPGatewayStatus, error) {
	servers, err := m.config.LoadMCPConfig()
	if err != nil {
		return nil, nil, err
	}
	m.EnsureAll(servers)
	out := []MCPGatewayTool{}
	status := []MCPGatewayStatus{}
	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		cfg := servers[name]
		st := MCPGatewayStatus{Name: name, Enabled: cfg.Enabled}
		if cfg.Enabled {
			s := m.Ensure(name, cfg)
			if s.err != nil {
				st.Error = "upstream unavailable"
				slog.Warn("MCP gateway upstream unavailable", "server", name)
			} else if s.sess != nil {
				st.Connected = true
				for _, tool := range s.tools {
					if !cfg.ToolDisabled(tool.Name) {
						out = append(out, MCPGatewayTool{name, tool})
						st.Tools++
					}
				}
			}
		}
		status = append(status, st)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Server != out[j].Server {
			return out[i].Server < out[j].Server
		}
		return out[i].Tool.Name < out[j].Tool.Name
	})
	return out, status, nil
}

func (m *MCPManager) GatewayCall(ctx context.Context, server, tool string, args json.RawMessage) (*mcpsdk.CallToolResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	servers, err := m.config.LoadMCPConfig()
	if err != nil {
		return nil, err
	}
	cfg, ok := servers[server]
	if !ok || !cfg.Enabled || cfg.ToolDisabled(tool) {
		return nil, fmt.Errorf("MCP tool unavailable")
	}
	// Ensure shares the native pool's bounded connection attempt. Cancellation
	// bounds the caller even while another client establishes the connection.
	ready := make(chan *MCPSession, 1)
	go func() { ready <- m.Ensure(server, cfg) }()
	var s *MCPSession
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case s = <-ready:
	}
	if s.err != nil || s.sess == nil {
		return nil, fmt.Errorf("MCP upstream unavailable")
	}
	result, err := s.sess.CallTool(ctx, &mcpsdk.CallToolParams{Name: tool, Arguments: args})
	if err != nil && ctx.Err() == nil {
		m.Invalidate(server)
	}
	return result, err
}
