package loom

import (
	"context"

	"encoding/json"

	"github.com/lucas-lepajollec/loom/internal/loom/policy"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func gatewayPolicyMiddleware(subjects map[string]string) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if method == "tools/call" {
				var params struct {
					Name      string `json:"name"`
					Arguments struct {
						Path    string `json:"path"`
						File    string `json:"file"`
						Command string `json:"command"`
					} `json:"arguments"`
				}
				raw, _ := json.Marshal(req.GetParams())
				_ = json.Unmarshal(raw, &params)
				subject := subjects[params.Name]
				if subject == "" {
					subject = "mcp.tool:loom/" + params.Name
				}
				m, session := policyTurn(ctx)
				in := sessionPolicyInput(session, subject, policy.Allow)
				in.Path, in.Command = params.Arguments.Path, params.Arguments.Command
				if in.Path == "" {
					in.Path = params.Arguments.File
				}
				if err := m.authorizePolicy(ctx, in, false); err != nil {
					return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}, nil
				}
			}
			return next(ctx, method, req)
		}
	}
}
