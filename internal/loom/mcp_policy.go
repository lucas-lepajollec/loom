package loom

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"

	"github.com/lucas-lepajollec/loom/internal/loom/policy"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// gatewayToolFallback returns the default policy decision when no explicit rule
// matches an MCP gateway tool call. High-impact shell, session-creation and
// write tools default to Confirm; read-only discovery tools remain Allow.
func gatewayToolFallback(subject string) policy.Decision {
	switch subject {
	case "mcp.tool:loom/loom_overview",
		"mcp.tool:loom/loom_machines",
		"mcp.tool:loom/loom_gateway_status",
		"mcp.tool:loom/loom_subagent_catalog",
		"mcp.tool:loom/loom_subagent_spawn",
		"mcp.tool:loom/loom_subagent_list",
		"mcp.tool:loom/loom_subagent_send",
		"mcp.tool:loom/loom_subagent_wait",
		"mcp.tool:loom/loom_subagent_read",
		"mcp.tool:loom/loom_subagent_stop",
		"mcp.tool:loom/loom_board_post",
		"mcp.tool:loom/loom_board_read",
		"mcp.tool:loom/brain_search",
		"mcp.tool:loom/brain_pack",
		"mcp.tool:loom/brain_read",
		"mcp.tool:loom/brain_write",
		"mcp.tool:loom/brain_edit",
		"mcp.tool:loom/memory_index",
		"mcp.tool:loom/memory_read",
		"mcp.tool:loom/memory_write",
		"mcp.tool:loom/memory_delete",
		"mcp.tool:loom/list_skills",
		"mcp.tool:loom/read_skill",
		"mcp.tool:loom/search_discussions",
		"mcp.tool:loom/remember",
		"mcp.tool:loom/update_memory",
		"mcp.tool:loom/forget_memory",
		"mcp.tool:loom/list_memory":
		return policy.Allow
	case "mcp.tool:loom/loom_machine_run",
		"mcp.tool:agents/start_session":
		return policy.Confirm
	}
	if strings.HasPrefix(subject, "mcp.tool:loom/") {
		return policy.Confirm
	}
	if tool, ok := strings.CutPrefix(subject, "mcp.tool:agents/"); ok {
		low := strings.ToLower(tool)
		for _, kw := range []string{"start", "send", "stop", "delete", "kill", "write", "exec", "run", "create", "remove"} {
			if strings.Contains(low, kw) {
				return policy.Confirm
			}
		}
	}
	return policy.Allow
}

func gatewayPolicyMiddleware(subjects map[string]string) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if method == "tools/call" {
				var params struct {
					Name      string `json:"name"`
					Arguments struct {
						Path    string `json:"path"`
						File    string `json:"file"`
						Dir     string `json:"dir"`
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
				in := sessionPolicyInput(session, subject, gatewayToolFallback(subject))
				if params.Name == "loom_machine_run" {
					resolved, err := resolveAgentMachine(params.Arguments.Machine)
					if err != nil {
						return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}, nil
					}
					in.MachineID = resolved
				} else if strings.TrimSpace(params.Arguments.Machine) != "" {
					if resolved, err := resolveAgentMachine(params.Arguments.Machine); err == nil {
						in.MachineID = resolved
					} else {
						in.MachineID = strings.TrimSpace(params.Arguments.Machine)
					}
				}
				in.Path, in.Command = params.Arguments.Path, params.Arguments.Command
				if in.Path == "" {
					in.Path = params.Arguments.File
				}
				if strings.TrimSpace(params.Arguments.Dir) != "" {
					in.Workdir = strings.TrimSpace(params.Arguments.Dir)
				}
				in.Detail = gatewayCallDetail(in.MachineID, params.Arguments.Command, call.Arguments)
				r := evaluatePolicy(in, false)
				switch r.Decision {
				case policy.Allow:
				case policy.Deny:
					msg := "policy denied capability: " + in.Subject
					if r.Reason == "policy_unavailable" {
						msg = "policy unavailable (vault locked or storage unreadable)"
					}
					return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: msg}}}, nil
				default:
					if err := m.confirmPolicy(ctx, in); err != nil {
						return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}, nil
					}
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
