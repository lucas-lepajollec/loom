package loom

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"

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
	used := map[string]bool{
		"loom_gateway_status": true, "loom_overview": true, "loom_machines": true, "loom_machine_run": true,
		"loom_subagent_catalog": true, "loom_subagent_spawn": true, "loom_subagent_list": true, "loom_subagent_send": true,
		"loom_subagent_wait": true, "loom_subagent_read": true, "loom_subagent_stop": true,
		"loom_preview_open": true, "loom_board_post": true, "loom_board_read": true,
		"brain_search": true, "brain_pack": true, "brain_read": true, "brain_write": true, "brain_edit": true,
		"remember": true, "update_memory": true, "forget_memory": true, "list_memory": true,
	}
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

// Each transport owns its reader and cached server. Rebuild the SDK registry
// only when the advertised definitions or instructions change.
func cachedGatewayServer(reader brain.Reader) func(*http.Request) *mcp.Server {
	var mu sync.Mutex
	var previous mcpHub
	var previousAgents []tools.MCPGatewayTool
	var server *mcp.Server
	return func(*http.Request) *mcp.Server {
		mu.Lock()
		defer mu.Unlock()
		hub := externalMCPHub()
		agents, _, _ := agentsMCP.GatewayTools()
		if server == nil || !slices.Equal(hub.servers, previous.servers) || !slices.Equal(hub.direct, previous.direct) || !slices.Equal(agents, previousAgents) {
			server = gatewayServer(reader, hub, agents)
			previous, previousAgents = hub, agents
		}
		return server
	}
}
func gatewayServer(reader brain.Reader, hub mcpHub, agentTools []tools.MCPGatewayTool) *mcp.Server {
	instructions := agentGuide
	if note := hub.note(); note != "" {
		instructions += "\n" + note
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "loom-gateway", Version: Version}, &mcp.ServerOptions{Instructions: instructions})
	brain.RegisterMCPTools(server, reader)
	registerLoomAgentTools(server)
	registerPreviewTool(server)
	registerLoomOrchestrationTools(server)
	registerMCPHubTools(server, hub)
	// Upstreams: the person's MCP servers, then Codeman's agent tools started
	// by Loom itself. Each tool remembers which pool serves it.
	type source struct {
		mgr  *tools.MCPManager
		item tools.MCPGatewayTool
	}
	var sources []source
	var upstream []tools.MCPGatewayTool
	for _, item := range hub.direct {
		sources = append(sources, source{mcpMgr.MCPManager, item})
		upstream = append(upstream, item)
	}

	for _, item := range agentTools {
		sources = append(sources, source{agentsMCP, item})
		upstream = append(upstream, item)
	}
	names := gatewayToolNames(upstream)
	subjects := map[string]string{}
	for i, src := range sources {
		tool := *src.item.Tool
		tool.Name = names[i]
		subjects[tool.Name] = "mcp.tool:" + src.item.Server + "/" + src.item.Tool.Name
		if tool.InputSchema == nil {
			tool.InputSchema = map[string]any{"type": "object"}
		}
		server.AddTool(&tool, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return src.mgr.GatewayCall(ctx, src.item.Server, src.item.Tool.Name, req.Params.Arguments)
		})
	}
	mcp.AddTool(server, &mcp.Tool{Name: "loom_gateway_status", Description: "Read the enabled, connected and tool-count state of Loom MCP upstreams, including Codeman's agent tools (agents); unavailable upstreams do not prevent discovery.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, gatewayStatusResult, error) {
		_, status, err := mcpMgr.GatewayTools()
		if _, agents, agentsErr := agentsMCP.GatewayTools(); agentsErr == nil {
			status = append(status, agents...)
		}
		return nil, gatewayStatusResult{status}, err
	})
	server.AddReceivingMiddleware(gatewayUsageMiddleware, gatewayPolicyMiddleware(subjects), cachedListSchemas)
	return server
}

// The SDK handles pagination and protocol-specific response fields. Cache the
// immutable wire schemas for this server revision so listing built-in tools
// doesn't repeatedly validate and reflect over the same inferred Go schemas.
func cachedListSchemas(next mcp.MethodHandler) mcp.MethodHandler {
	var mu sync.Mutex
	definitions := map[*mcp.Tool]*mcp.Tool{}
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		res, err := next(ctx, method, req)
		list, ok := res.(*mcp.ListToolsResult)
		if method != "tools/list" || err != nil || !ok {
			return res, err
		}
		mu.Lock()
		defer mu.Unlock()
		for i, t := range list.Tools {
			wire := definitions[t]
			if wire == nil {
				clone := *t
				input, err := json.Marshal(t.InputSchema)
				if err != nil {
					return nil, err
				}
				clone.InputSchema = json.RawMessage(input)
				if t.OutputSchema != nil {
					output, err := json.Marshal(t.OutputSchema)
					if err != nil {
						return nil, err
					}
					clone.OutputSchema = json.RawMessage(output)
				}
				wire = &clone
				definitions[t] = wire
			}
			list.Tools[i] = wire
		}
		return res, nil
	}
}

// gatewayUsageMiddleware remembers which agent client last used the gateway.
// The client names itself in the handshake (older protocol) or in each
// request (2026-07-28 protocol); both reach the session's InitializeParams.
func gatewayUsageMiddleware(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		var info *mcp.Implementation
		if p, ok := req.GetParams().(*mcp.InitializeParams); ok && p != nil {
			info = p.ClientInfo
		} else if ss, ok := req.GetSession().(*mcp.ServerSession); ok && ss != nil {
			if p := ss.InitializeParams(); p != nil {
				info = p.ClientInfo
			}
		}
		client := ""
		if info != nil {
			client = info.Name
		}
		noteGatewayUse(client)
		return next(ctx, method, req)
	}
}

func requireGatewayAuth(next http.HandlerFunc) http.HandlerFunc {
	fallback := requireWebAuth(next)
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/mcp/loom" || r.URL.Path == "/mcp/brain" {
			if scope, ok := lookupScopedBearer(r); ok {
				next(w, r.WithContext(withSessionScope(r.Context(), scope)))
				return
			}
			state, err := loadGatewayState()
			if err != nil {
				webAuthUnavailable(w)
				return
			}
			if state.TokenHash != "" && checkBearer(r, state.TokenHash) {
				if sid := strings.TrimSpace(r.Header.Get("X-Loom-Session-Id")); sid != "" {
					r = r.WithContext(withSessionScope(r.Context(), orchResolveScope(sid)))
				}
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
