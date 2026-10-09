package loom

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/lucas-lepajollec/loom/internal/loom/brain"
	"github.com/lucas-lepajollec/loom/internal/loom/tools"
	"github.com/lucas-lepajollec/loom/internal/loom/web"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func gatewayURL() string {
	host := webBound.host
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		host = "127.0.0.1"
	}
	port := webBound.port
	if port == 0 {
		port = 2510
	}
	return "http://" + net.JoinHostPort(strings.Trim(host, "[]"), strconv.Itoa(port)) + "/mcp/loom"
}

func gatewayToolNames(upstream []tools.MCPGatewayTool) []string {
	used := map[string]bool{"loom_gateway_status": true, "brain_search": true, "brain_pack": true, "brain_read": true, "brain_write": true, "brain_edit": true, "remember": true, "update_memory": true, "forget_memory": true, "list_memory": true}
	names := make([]string, len(upstream))
	order := make([]int, len(upstream))
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(i, j int) bool {
		a, b := upstream[order[i]], upstream[order[j]]
		if a.Server != b.Server {
			return a.Server < b.Server
		}
		return a.Tool.Name < b.Tool.Name
	})
	for _, i := range order {
		item := upstream[i]
		server, tool := tools.MCPSanitize(item.Server), tools.MCPSanitize(item.Tool.Name)
		base := server + "__" + tool
		identity := hashWebKey(item.Server + "\x00" + item.Tool.Name)[:12]
		// Lossy names retain their identity when another upstream disappears.
		if server != item.Server || tool != item.Tool.Name || strings.Contains(server, "__") || len(base) > 64 {
			base = base[:min(len(base), 51)] + "_" + identity
		}
		name := base
		for n := 2; used[name]; n++ {
			suffix := fmt.Sprintf("_%s_%d", identity, n)
			name = base[:min(len(base), 64-len(suffix))] + suffix
		}
		used[name], names[i] = true, name
	}
	return names
}

type gatewayStatusResult struct {
	Upstreams []tools.MCPGatewayStatus `json:"upstreams"`
}

func gatewayServer(reader brain.Reader) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "loom-gateway", Version: Version}, nil)
	brain.RegisterMCPTools(server, reader)
	upstream, _, err := mcpMgr.GatewayTools()
	if err != nil {
		upstream = nil
	}
	names := gatewayToolNames(upstream)
	subjects := map[string]string{}
	for i, item := range upstream {
		tool := *item.Tool
		tool.Name = names[i]
		subjects[tool.Name] = "mcp.tool:" + item.Server + "/" + item.Tool.Name
		if tool.InputSchema == nil {
			tool.InputSchema = map[string]any{"type": "object"}
		}
		server.AddTool(&tool, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return mcpMgr.GatewayCall(ctx, item.Server, item.Tool.Name, req.Params.Arguments)
		})
	}
	mcp.AddTool(server, &mcp.Tool{Name: "loom_gateway_status", Description: "Read the enabled, connected and tool-count state of Loom MCP upstreams; unavailable upstreams do not prevent discovery.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, gatewayStatusResult, error) {
		_, status, err := mcpMgr.GatewayTools()
		return nil, gatewayStatusResult{status}, err
	})
	server.AddReceivingMiddleware(gatewayPolicyMiddleware(subjects))
	return server
}

func requireGatewayAuth(next http.HandlerFunc) http.HandlerFunc {
	fallback := requireWebAuth(next)
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/mcp/loom" || r.URL.Path == "/mcp/brain" {
			state, err := loadGatewayState()
			if err != nil {
				webAuthUnavailable(w)
				return
			}
			if ctx, ok := gatewayPolicyContext(r); ok {
				next(w, r.WithContext(ctx))
				return
			}
			if sessionGatewayAuthorized(r) {
				next(w, r)
				return
			}
			if state.TokenHash != "" && checkBearer(r, state.TokenHash) {
				next(w, r)
				return
			}
		}
		fallback(w, r)
	}
}

func registerMCPTransport(mux *http.ServeMux, path string, getServer func(*http.Request) *mcp.Server) {
	transport := mcp.NewStreamableHTTPHandler(getServer, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	authed := requireGatewayAuth(web.ProtectOrigin(transport).ServeHTTP)
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		r.Body = http.MaxBytesReader(w, r.Body, 128<<10)
		authed(w, r)
	})
}
