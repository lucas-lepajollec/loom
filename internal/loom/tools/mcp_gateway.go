package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type MCPGatewayTool struct {
	Server      string
	Tool        *mcpsdk.Tool
	SchemaBytes int
}
type MCPGatewayStatus struct {
	Name      string `json:"name"`
	Enabled   bool   `json:"enabled"`
	Connected bool   `json:"connected"`
	Tools     int    `json:"tools"`
	Error     string `json:"error,omitempty"`
	Exposure  string `json:"-"`
}

func (m *MCPManager) GatewayTools() ([]MCPGatewayTool, []MCPGatewayStatus, error) {
	rows, out, err := m.StatusWithTools()
	if err != nil {
		return nil, nil, err
	}
	status := make([]MCPGatewayStatus, 0, len(rows))
	counts := map[string]int{}
	for _, t := range out {
		counts[t.Server]++
	}
	for _, r := range rows {
		status = append(status, MCPGatewayStatus{Name: r.Name, Enabled: r.Enabled, Exposure: r.Exposure, Connected: r.Connected, Error: r.Error, Tools: counts[r.Name]})
	}
	return out, status, nil
}

func (m *MCPManager) GatewayCall(ctx context.Context, server, tool string, args json.RawMessage) (*mcpsdk.CallToolResult, error) {
	return m.GatewayCallChecked(ctx, server, tool, args, nil)
}

// GatewayCallChecked binds an approval to the selected live tool and config.
// A change while the caller is waiting for consent requires a new invocation.
func (m *MCPManager) GatewayCallChecked(ctx context.Context, server, tool string, args json.RawMessage, authorize func(context.Context) error) (*mcpsdk.CallToolResult, error) {

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
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s := m.schedule(server, cfg)
	discovery, stop := context.WithTimeout(ctx, 60*time.Second)
	defer stop()
	select {
	case <-discovery.Done():
		return nil, discovery.Err()
	case <-s.ready:
	}
	if s.err != nil || s.sess == nil {
		return nil, fmt.Errorf("MCP upstream unavailable")
	}
	m.mu.Lock()
	definitions := s.tools
	m.mu.Unlock()
	if !slices.ContainsFunc(definitions, func(t *mcpsdk.Tool) bool { return t.Name == tool }) {
		return nil, fmt.Errorf("MCP tool unavailable")
	}
	if authorize != nil {
		if err := authorize(ctx); err != nil {
			return nil, err
		}
	}
	// Re-read after discovery and approval so a concurrent edit takes effect.
	servers, err = m.config.LoadMCPConfig()
	if err != nil {
		return nil, err
	}
	cfg, ok = servers[server]
	m.mu.Lock()
	current := m.sessions[server] == s
	m.mu.Unlock()
	if !current || !ok || !cfg.Enabled || cfg.ToolDisabled(tool) || configHash(cfg) != s.hash {
		return nil, fmt.Errorf("MCP tool unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	result, err := s.sess.CallTool(ctx, &mcpsdk.CallToolParams{Name: tool, Arguments: args})
	if err != nil && ctx.Err() == nil {
		m.mu.Lock()
		if m.sessions[server] == s {
			retireSession(s)
			delete(m.sessions, server)
		}
		m.mu.Unlock()
		return nil, fmt.Errorf("MCP upstream call failed")
	}
	return result, err
}
