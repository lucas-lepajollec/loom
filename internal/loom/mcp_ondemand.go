package loom

import (
	"context"
	"encoding/json"
	"fmt"

	"slices"
	"sort"
	"strings"
	"unicode"

	"github.com/lucas-lepajollec/loom/internal/loom/resources"
	"github.com/lucas-lepajollec/loom/internal/loom/tools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	mcpSearchTool   = "loom_mcp_search"
	mcpDescribeTool = "loom_mcp_describe"
	mcpCallTool     = "loom_mcp_call"
	mcpRegisterTool = "loom_mcp_register"
)

func effectiveMCPMode(mode string) string {
	if mode == "smart" {
		return mode
	}
	return "direct"
}

func mcpExposure(exposure, mode string) string {
	if exposure != "" {
		return exposure
	}
	if mode == "smart" {
		return resources.MCPOnDemand
	}
	return resources.MCPDirect
}

type onDemandServer struct {
	Name       string `json:"name"`
	ToolsCount int    `json:"tools_count"`
	Exposure   string `json:"exposure"`
}

type mcpHub struct {
	direct, lazy []tools.MCPGatewayTool
	servers      []onDemandServer
	status       []tools.MCPGatewayStatus
}

func externalMCPHub() mcpHub {
	all, status, _ := mcpMgr.GatewayTools()
	settings, _ := loadGatewayState()
	hub := mcpHub{status: status}
	exposures := make(map[string]string, len(status))

	for _, st := range status {
		exposure := mcpExposure(st.Exposure, settings.ToolMode)
		exposures[st.Name] = exposure
		if st.Enabled && exposure == resources.MCPOnDemand {
			hub.servers = append(hub.servers, onDemandServer{st.Name, st.Tools, exposure})
		}
	}
	for _, t := range all {
		if exposures[t.Server] == resources.MCPOnDemand {
			hub.lazy = append(hub.lazy, t)
		} else {
			hub.direct = append(hub.direct, t)
		}
	}
	return hub
}

func (hub mcpHub) note() string {
	if len(hub.servers) == 0 {
		return ""
	}
	names := make([]string, 0, len(hub.servers))
	for _, s := range hub.servers {
		names = append(names, fmt.Sprintf("%s (%d tools)", strings.Join(strings.Fields(s.Name), " "), s.ToolsCount))
	}
	return "On-demand MCP servers (use loom_mcp_search, loom_mcp_describe for the schema, then loom_mcp_call with fields/max_chars or loom_mcp_execute for a pipeline): " + strings.Join(names, ", ") + "."
}

func (hub mcpHub) discussionTools() []Tool {
	out := make([]Tool, 0, len(hub.direct)+len(mcpMetaTools))
	for _, item := range hub.direct {
		t := item.Tool
		out = append(out, Tool{Type: "function", Function: tools.ToolFunction{
			Name:        tools.MCPExposedName(item.Server, t.Name),
			Description: "[MCP: " + item.Server + "] " + firstNonEmpty(t.Description, t.Title),
			Parameters:  tools.MCPNormalizeSchema(t.InputSchema),
		}})
	}
	if len(hub.servers) > 0 {
		for _, tool := range mcpMetaTools {
			out = append(out, Tool{Type: "function", Function: tools.ToolFunction{
				Name: tool.Name, Description: tool.Description, Parameters: tool.InputSchema,
			}})
		}
	}
	if len(hub.servers) == 0 && slices.ContainsFunc(hub.status, func(s tools.MCPGatewayStatus) bool { return s.Enabled }) {
		out = append(out, Tool{Type: "function", Function: tools.ToolFunction{Name: mcpExecuteTool, Description: executeDefinition.Description, Parameters: executeDefinition.InputSchema}})
	}
	return out
}

func (hub mcpHub) discussionNote() string {
	direct := []string{}
	for _, st := range hub.status {
		if st.Tools > 0 && !slices.ContainsFunc(hub.servers, func(s onDemandServer) bool { return s.Name == st.Name }) {
			direct = append(direct, fmt.Sprintf("%s (%d)", st.Name, st.Tools))
		}
	}
	var note string
	if len(direct) > 0 {
		note = "MCP servers connected (their tools are prefixed mcp__<server>__<tool>): " + strings.Join(direct, ", ") + "."
	}
	return strings.TrimSpace(note + "\n" + hub.note())
}

func (hub mcpHub) find(server, name string) (*mcp.Tool, error) {
	for _, item := range hub.lazy {
		if item.Server == server && item.Tool.Name == name {
			return item.Tool, nil
		}
	}
	return nil, fmt.Errorf("MCP tool unavailable")
}

type mcpSearchArgs struct {
	Query  string `json:"query"`
	Server string `json:"server"`
	Limit  int    `json:"limit"`
}
type mcpTarget struct {
	Server string `json:"server"`
	Tool   string `json:"tool"`
}
type mcpCallArgs struct {
	mcpTarget
	Arguments json.RawMessage `json:"arguments"`
	Fields    []string        `json:"fields,omitempty"`
	MaxChars  int             `json:"max_chars,omitempty"`
}
type mcpSearchMatch struct {
	mcpTarget
	Description string   `json:"description"`
	Required    []string `json:"required_args"`
	Args        []string `json:"args"`
}
type mcpSearchResult struct {
	Matches        []mcpSearchMatch `json:"matches"`
	Servers        []onDemandServer `json:"servers"`
	TotalAvailable int              `json:"total_available"`
}
type mcpDescription struct {
	Description string               `json:"description"`
	InputSchema any                  `json:"inputSchema"`
	Annotations *mcp.ToolAnnotations `json:"annotations"`
}

func mcpSearchWords(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
}

func mcpSearchScore(terms []string, server, tool, desc, args string) int {
	fields := []struct {
		text   string
		words  []string
		weight int
	}{{tool, nil, 8}, {server, nil, 5}, {args, nil, 4}, {desc, nil, 2}}
	for i := range fields {
		fields[i].text = strings.ToLower(fields[i].text)
		fields[i].words = mcpSearchWords(fields[i].text)
	}
	score := 0
	for _, term := range terms {
		best := 0
		for _, field := range fields {
			if slices.Contains(field.words, term) {
				best = max(best, field.weight*2)
			} else if strings.Contains(field.text, term) {
				best = max(best, field.weight)
			}
		}
		score += best
	}
	return score
}

func (hub mcpHub) search(args mcpSearchArgs) mcpSearchResult {
	query, server := strings.TrimSpace(args.Query), strings.TrimSpace(args.Server)
	terms := mcpSearchWords(query)
	limit := args.Limit
	if limit == 0 {
		limit = 8
	}
	limit = min(max(1, limit), 25)
	out := mcpSearchResult{Matches: []mcpSearchMatch{}, Servers: []onDemandServer{}}
	for _, s := range hub.servers {
		if server == "" || strings.EqualFold(server, s.Name) {
			out.Servers = append(out.Servers, s)
			out.TotalAvailable += s.ToolsCount
		}
	}
	type ranked struct {
		match mcpSearchMatch
		score int
	}
	var matches []ranked
	for _, item := range hub.lazy {
		if server != "" && !strings.EqualFold(server, item.Server) {
			continue
		}
		t := item.Tool
		schema := tools.MCPNormalizeSchema(t.InputSchema)
		keys, required := []string{}, []string{}
		if props, ok := schema["properties"].(map[string]any); ok {
			for key := range props {
				keys = append(keys, key)
			}
		}
		switch req := schema["required"].(type) {
		case []string:
			required = append(required, req...)
		case []any:
			for _, value := range req {
				if key, ok := value.(string); ok {
					required = append(required, key)
				}
			}
		}
		sort.Strings(keys)
		sort.Strings(required)
		score := 1
		if query != "*" {
			score = mcpSearchScore(terms, item.Server, t.Name, t.Description, strings.Join(keys, " "))
			if strings.EqualFold(query, t.Name) {
				score += 20
			}
		}
		if score > 0 {
			matches = append(matches, ranked{mcpSearchMatch{
				mcpTarget{item.Server, t.Name}, truncateRunes(strings.Join(strings.Fields(t.Description), " "), 159), required, keys,
			}, score})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool { return matches[i].score > matches[j].score })
	for _, hit := range matches[:min(limit, len(matches))] {
		out.Matches = append(out.Matches, hit.match)
	}
	return out
}

func mcpObjectSchema(required []string, fields map[string]string) map[string]any {
	properties := make(map[string]any, len(fields))
	for name, kind := range fields {
		if kind == "strings" {
			properties[name] = map[string]any{"type": "array", "items": map[string]string{"type": "string"}}
		} else {
			properties[name] = map[string]string{"type": kind}
		}
	}
	return map[string]any{"type": "object", "required": required, "properties": properties}
}

var mcpMetaTools = []*mcp.Tool{
	{Name: mcpSearchTool, Description: "Find on-demand MCP tools by keyword. Use * to list tools. Limit defaults to 8, max 25.",
		InputSchema: mcpObjectSchema([]string{"query"}, map[string]string{"query": "string", "server": "string", "limit": "integer"}),
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}},
	{Name: mcpDescribeTool, Description: "Get the exact schema of one on-demand MCP tool.",
		InputSchema: mcpObjectSchema([]string{"server", "tool"}, map[string]string{"server": "string", "tool": "string"}),
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}},
	{Name: mcpCallTool, Description: "Call an on-demand tool. Select JSON fields with dot paths. max_chars defaults to 12000 (maximum 65536).",
		InputSchema: mcpObjectSchema([]string{"server", "tool", "arguments"}, map[string]string{"server": "string", "tool": "string", "arguments": "object", "fields": "strings", "max_chars": "integer"})},
	executeDefinition,
}

func isMCPMetaTool(name string) bool {
	return name == mcpSearchTool || name == mcpDescribeTool || name == mcpCallTool || name == mcpExecuteTool
}

func registerMCPHubTools(s *mcp.Server, hub mcpHub) {
	for _, definition := range mcpMetaTools {
		if len(hub.servers) == 0 && definition.Name != mcpExecuteTool {
			continue
		}
		s.AddTool(definition, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return executeMCPMetaTool(ctx, definition.Name, req.Params.Arguments)
		})
	}
	registerMCPManagement(s)
}

func mcpJSONResult(value any) (*mcp.CallToolResult, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}, StructuredContent: value}, nil
}

func executeMCPMetaTool(ctx context.Context, name string, raw json.RawMessage) (*mcp.CallToolResult, error) {
	switch name {
	case mcpSearchTool:
		var args mcpSearchArgs
		if json.Unmarshal(raw, &args) != nil || strings.TrimSpace(args.Query) == "" {
			return nil, fmt.Errorf("query is required")
		}
		return mcpJSONResult(externalMCPHub().search(args))
	case mcpDescribeTool:
		var args mcpTarget
		if json.Unmarshal(raw, &args) != nil || args.Server == "" || args.Tool == "" {
			return nil, fmt.Errorf("server and tool are required")
		}
		t, err := externalMCPHub().find(args.Server, args.Tool)
		if err != nil {
			return nil, err
		}
		return mcpJSONResult(mcpDescription{t.Description, t.InputSchema, t.Annotations})
	case mcpCallTool:
		var args mcpCallArgs
		if json.Unmarshal(raw, &args) != nil || args.Server == "" || args.Tool == "" || len(args.Arguments) == 0 || args.Arguments[0] != '{' {
			return nil, fmt.Errorf("server, tool and an arguments object are required")
		}
		if !slices.ContainsFunc(externalMCPHub().servers, func(s onDemandServer) bool { return s.Name == args.Server }) {
			return nil, fmt.Errorf("MCP tool unavailable")
		}
		if err := validFields(args.Fields); err != nil {
			return nil, err
		}
		if args.MaxChars < 0 || args.MaxChars > 65536 {
			return nil, fmt.Errorf("max_chars must be between 1 and 65536")
		}
		result, err := authorizedMCPCall(ctx, args.mcpTarget, args.Arguments)
		if err != nil {
			return nil, err
		}
		return compactCallResult(result, args.Fields, args.MaxChars)
	case mcpExecuteTool:
		return executePipeline(ctx, raw, authorizedMCPCall)
	}
	return nil, fmt.Errorf("unknown MCP discovery tool")
}

func authorizedMCPCall(ctx context.Context, target mcpTarget, raw json.RawMessage) (*mcp.CallToolResult, error) {
	subject := "mcp.tool:" + target.Server + "/" + target.Tool
	return mcpMgr.GatewayCallChecked(ctx, target.Server, target.Tool, raw, func(ctx context.Context) error { return authorizeGatewayTool(ctx, "", subject, raw) })
}

type mcpTokenBudget struct {
	CodeMode       bool `json:"code_mode"`
	DirectTokens   int  `json:"direct_tokens"`
	InjectedTokens int  `json:"injected_tokens"`
	SavedTokens    int  `json:"saved_tokens"`
	SavedPercent   int  `json:"saved_percent"`
	OnDemandTools  int  `json:"on_demand_tools"`
	DirectTools    int  `json:"direct_tools"`
}

func estimateMCPBudget(servers []MCPServerStatus, exposed []tools.MCPGatewayTool, mode string) mcpTokenBudget {
	var budget mcpTokenBudget
	var directBytes, totalBytes int
	lazy := false
	exposures := make(map[string]string, len(servers))
	for _, st := range servers {
		exposure := mcpExposure(st.Exposure, mode)
		exposures[st.Name] = exposure
		budget.CodeMode = budget.CodeMode || st.Enabled
		lazy = lazy || st.Enabled && exposure == resources.MCPOnDemand
	}
	for _, item := range exposed {
		totalBytes += item.SchemaBytes
		if exposures[item.Server] == resources.MCPOnDemand {
			budget.OnDemandTools++
		} else {
			budget.DirectTools++
			directBytes += item.SchemaBytes
		}
	}
	budget.DirectTokens = totalBytes / 4
	budget.InjectedTokens = directBytes / 4
	if lazy {
		budget.InjectedTokens += mcpDiscoveryTokens
	} else if budget.CodeMode {
		budget.InjectedTokens += mcpExecuteTokens
	}
	budget.SavedTokens = max(0, budget.DirectTokens-budget.InjectedTokens)
	if budget.DirectTokens > 0 {
		budget.SavedPercent = min(99, budget.SavedTokens*100/budget.DirectTokens)
	}
	return budget
}

func toolTokens(v any) int { raw, _ := json.Marshal(v); return len(raw) / 4 }

var mcpDiscoveryTokens = toolTokens(mcpMetaTools)
var mcpExecuteTokens = toolTokens(executeDefinition)
