package loom

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"

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
						Machine string `json:"machine"`
					} `json:"arguments"`
				}
				var call struct {
					Arguments json.RawMessage `json:"arguments"`
				}
				raw, _ := json.Marshal(req.GetParams())
				_ = json.Unmarshal(raw, &params)
				_ = json.Unmarshal(raw, &call)
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
				in.Detail = gatewayCallDetail(params.Arguments.Machine, params.Arguments.Command, call.Arguments)
				if err := m.authorizePolicy(ctx, in, false); err != nil {
					return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}, nil
				}
			}
			return next(ctx, method, req)
		}
	}
}

// gatewayCallDetail is what the person reads before approving an agent's call:
// the machine and command for a command, otherwise the arguments.
func gatewayCallDetail(machine, command string, args json.RawMessage) string {
	if strings.TrimSpace(command) != "" {
		if strings.TrimSpace(machine) == "" {
			machine = "local"
		}
		return truncateRunes(strings.TrimSpace(machine)+" $ "+strings.TrimSpace(command), 600)
	}
	var compact bytes.Buffer
	if json.Compact(&compact, args) != nil || compact.String() == "{}" || compact.String() == "null" {
		return ""
	}
	return truncateRunes(compact.String(), 600)
}

func truncateRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
