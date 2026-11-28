package loom

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/lucas-lepajollec/loom/internal/loom/policy"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// gatewayToolFallback returns the default policy decision when no explicit rule
// matches an MCP gateway tool call. High-impact shell, session-creation and
// write tools default to Confirm; read-only discovery tools remain Allow.
func gatewayToolFallback(subject string) policy.Decision {
	switch subject {
	case "mcp.tool:loom/" + mcpListTool, "mcp.tool:loom/" + mcpSearchTool, "mcp.tool:loom/" + mcpDescribeTool,
		"mcp.tool:loom/loom_overview",
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
			if method != "tools/call" {
				return next(ctx, method, req)
			}
			var params struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}
			raw, _ := json.Marshal(req.GetParams())
			_ = json.Unmarshal(raw, &params)
			// These handlers authorize the real upstream subject or mcp.manage
			// after validating the proposal. The wrapper name is not a capability.
			if params.Name == mcpExecuteTool || params.Name == mcpCallTool || params.Name == mcpRegisterTool {
				return next(ctx, method, req)
			}
			subject := subjects[params.Name]
			if subject == "" {
				subject = "mcp.tool:loom/" + params.Name
			}
			if err := authorizeGatewayTool(ctx, params.Name, subject, params.Arguments); err != nil {
				return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}, nil
			}
			return next(ctx, method, req)
		}
	}
}

func authorizeGatewayTool(ctx context.Context, name, subject string, raw json.RawMessage) error {
	var args struct {
		Path, File, Dir, Command, Machine string
	}
	_ = json.Unmarshal(raw, &args)
	mgr, session := policyTurn(ctx)
	in := sessionPolicyInput(session, subject, gatewayToolFallback(subject))
	if name == "loom_machine_run" {
		resolved, err := resolveAgentMachine(args.Machine)
		if err != nil {
			return err
		}
		in.MachineID = resolved
	} else if strings.TrimSpace(args.Machine) != "" {
		if resolved, err := resolveAgentMachine(args.Machine); err == nil {
			in.MachineID = resolved
		} else {
			in.MachineID = strings.TrimSpace(args.Machine)
		}
	}
	in.Path, in.Command = firstNonEmpty(args.Path, args.File), args.Command
	if dir := strings.TrimSpace(args.Dir); dir != "" {
		in.Workdir = dir
	}
	in.Detail = gatewayCallDetail(in.MachineID, args.Command, raw)
	return authorizeMCPPolicy(ctx, mgr, in, false)
}

func authorizeMCPPolicy(ctx context.Context, mgr *runtimeSessions, in policy.Input, requireApproval bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r := evaluatePolicy(in, false)
	switch {
	case r.Decision == policy.Deny:
		if r.Reason == "policy_unavailable" {
			return errors.New("policy unavailable (vault locked or storage unreadable)")
		}
		return errors.New("policy denied capability: " + in.Subject)
	case r.Decision == policy.Allow && !requireApproval:
		return nil
	case mgr == nil:
		return errors.New("policy confirmation required for " + in.Subject)
	default:
		if gate, ok := ctx.Value(pipelineApprovalKey{}).(chan struct{}); ok {
			select {
			case gate <- struct{}{}:
				defer func() { <-gate }()
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return mgr.confirmPolicy(ctx, in)
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
