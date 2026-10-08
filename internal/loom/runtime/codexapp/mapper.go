package codexapp

import (
	"encoding/json"
	"fmt"
	"github.com/lucas-lepajollec/loom/internal/loom/harness"
	"strings"

	"github.com/lucas-lepajollec/loom/internal/loom/runtime"
	"github.com/lucas-lepajollec/loom/internal/loom/runtime/agentstdio"
)

var TestedVersion = harness.LatestTestedVersion("codex")

func Notification(f agentstdio.Frame) []runtime.AgentEvent {
	e := runtime.AgentEvent{Runtime: "codex", Type: "raw", Method: f.Method, Raw: runtime.BoundedJSON(f.Raw), Payload: runtime.BoundedJSON(f.Params)}
	var envelope struct {
		ThreadID string `json:"threadId"`
		TurnID   string `json:"turnId"`
		ItemID   string `json:"itemId"`
	}
	if json.Unmarshal(f.Params, &envelope) != nil {
		e.Type = "error"
		e.Error = "invalid Codex notification: " + f.Method
		return []runtime.AgentEvent{e}
	}
	e.ThreadID, e.TurnID, e.ItemID = envelope.ThreadID, envelope.TurnID, envelope.ItemID
	switch f.Method {
	case "turn/started", "turn/completed":
		var p TurnCompletedNotification
		if json.Unmarshal(f.Params, &p) != nil {
			break
		}
		var t struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(p.Turn, &t) != nil {
			break
		}
		e.TurnID = t.ID
		e.Status = t.Status
		e.Type = "turn.started"
		if f.Method == "turn/completed" {
			e.Type = "turn.completed"
		}
		if t.Error != nil {
			e.Error = t.Error.Message
		}
	case "item/started", "item/completed":
		var p ItemStartedNotification
		if json.Unmarshal(f.Params, &p) != nil {
			break
		}
		var item struct {
			ID     string `json:"id"`
			Type   string `json:"type"`
			Status string `json:"status"`
		}
		if json.Unmarshal(p.Item, &item) != nil {
			break
		}
		e.ItemID = item.ID
		e.ItemType = ItemType(item.Type)
		e.Status = item.Status
		e.Payload = runtime.BoundedJSON(p.Item)
		e.Type = "item.started"
		if f.Method == "item/completed" {
			e.Type = "item.completed"
		}
	case "item/agentMessage/delta", "item/reasoning/summaryTextDelta", "item/reasoning/textDelta", "item/commandExecution/outputDelta", "item/fileChange/outputDelta":
		var p AgentMessageDeltaNotification
		if json.Unmarshal(f.Params, &p) != nil {
			break
		}
		e.Type = "content.delta"
		e.Delta = p.Delta
		e.Stream = "assistant_text"
		switch f.Method {
		case "item/reasoning/summaryTextDelta", "item/reasoning/textDelta":
			e.Stream = "reasoning_text"
		case "item/commandExecution/outputDelta":
			e.Stream = "command_output"
		case "item/fileChange/outputDelta":
			e.Stream = "file_change_output"
		}
	case "turn/plan/updated":
		e.Type = "item.updated"
		e.ItemType = "plan"
		e.ItemID = "plan:" + e.TurnID
	case "turn/diff/updated":
		e.Type = "item.updated"
		e.ItemType = "file_change"
		e.ItemID = "diff:" + e.TurnID
	case "thread/tokenUsage/updated":
		var p ThreadTokenUsageUpdatedNotification
		if json.Unmarshal(f.Params, &p) != nil {
			break
		}
		e.Type = "context.updated"
		e.Payload = runtime.BoundedJSON(p.TokenUsage)
		var u struct {
			Last struct {
				Input     *int64 `json:"inputTokens"`
				Output    *int64 `json:"outputTokens"`
				Cached    *int64 `json:"cachedInputTokens"`
				Reasoning *int64 `json:"reasoningOutputTokens"`
				Total     *int64 `json:"totalTokens"`
			} `json:"last"`
			Window *int64 `json:"modelContextWindow"`
		}
		if json.Unmarshal(p.TokenUsage, &u) == nil {
			e.Usage = &runtime.AgentUsage{Scope: "context", Input: u.Last.Input, Output: u.Last.Output, Cached: u.Last.Cached, Reasoning: u.Last.Reasoning, Total: u.Last.Total, ContextWindow: u.Window}
		}
	case "error":
		var p ErrorNotification
		if json.Unmarshal(f.Params, &p) != nil {
			break
		}
		var detail struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(p.Error, &detail)
		e.Type = "error"
		e.Error = detail.Message
		if p.WillRetry {
			e.Type = "warning"
			e.Message = e.Error
			e.Error = ""
		}
	}
	return []runtime.AgentEvent{e}
}
func ItemType(t string) string {
	switch t {
	case "agentMessage":
		return "assistant_message"
	case "userMessage":
		return "user_message"
	case "reasoning":
		return "reasoning"
	case "commandExecution":
		return "command_execution"
	case "fileChange":
		return "file_change"
	case "mcpToolCall":
		return "mcp_tool_call"
	case "dynamicToolCall", "collabAgentToolCall":
		return "tool_call"
	case "webSearch":
		return "web_search"
	case "plan":
		return "plan"
	default:
		return t
	}
}
func Request(f agentstdio.Frame) (runtime.AgentEvent, error) {
	e := runtime.AgentEvent{Type: "request.opened", Runtime: "codex", Method: f.Method, Raw: runtime.BoundedJSON(f.Raw)}
	r := &runtime.AgentRequest{ID: "codex:" + string(f.ID), Method: f.Method, Payload: runtime.BoundedJSON(f.Params)}
	e.Request = r
	if len(f.Params) > 64<<10 {
		return e, fmt.Errorf("Codex request too large: %s", f.Method)
	}
	switch f.Method {
	case "item/commandExecution/requestApproval":
		var p CommandExecutionRequestApprovalParams
		if err := json.Unmarshal(f.Params, &p); err != nil {
			return e, err
		}
		e.ThreadID, e.TurnID = p.ThreadId, p.TurnId
		r.ItemID = p.ItemId
		r.Kind = "approval"
		r.ApprovalKind = "command"
		r.Message = p.Reason
		allowed := []string{"accept", "acceptForSession", "decline"}
		r.Options = approvalOptions(allowed)
	case "item/permissions/requestApproval":
		var p PermissionsRequestApprovalParams
		if err := json.Unmarshal(f.Params, &p); err != nil {
			return e, err
		}
		e.ThreadID, e.TurnID = p.ThreadId, p.TurnId
		r.ItemID = p.ItemId
		r.Kind = "approval"
		r.ApprovalKind = "tool"
		r.Message = p.Reason
		r.Options = approvalOptions([]string{"accept", "acceptForSession", "decline"})

	case "item/fileChange/requestApproval":
		var p FileChangeRequestApprovalParams
		if err := json.Unmarshal(f.Params, &p); err != nil {
			return e, err
		}
		e.ThreadID, e.TurnID = p.ThreadId, p.TurnId
		r.ItemID = p.ItemId
		r.Kind = "approval"
		r.ApprovalKind = "file"
		r.Message = p.Reason
		r.Options = approvalOptions([]string{"accept", "acceptForSession", "decline"})
	case "item/tool/requestUserInput":
		var p ToolRequestUserInputParams
		if err := json.Unmarshal(f.Params, &p); err != nil {
			return e, err
		}
		e.ThreadID, e.TurnID = p.ThreadId, p.TurnId
		r.ItemID = p.ItemId
		r.Kind = "user_input"
		var questions []struct {
			ID       string `json:"id"`
			Header   string `json:"header"`
			Question string `json:"question"`
			IsOther  bool   `json:"isOther"`
			IsSecret bool   `json:"isSecret"`
			Options  []struct {
				Label       string `json:"label"`
				Description string `json:"description"`
			} `json:"options"`
		}
		if err := json.Unmarshal(p.Questions, &questions); err != nil {
			return e, err
		}
		for _, q := range questions {
			v := runtime.InputQuestion{ID: q.ID, Header: q.Header, Question: q.Question, FreeText: q.IsOther || len(q.Options) == 0, Secret: q.IsSecret}
			for _, o := range q.Options {
				v.Options = append(v.Options, runtime.RequestOption{ID: o.Label, Label: o.Label, Description: o.Description})
			}
			r.Questions = append(r.Questions, v)
		}
	case "mcpServer/elicitation/request":
		var p McpServerElicitationRequestParams
		if err := json.Unmarshal(f.Params, &p); err != nil {
			return e, err
		}
		if p.Mode != "form" && p.Mode != "openai/form" && p.Mode != "openaiForm" && p.Mode != "url" {
			return e, fmt.Errorf("unsupported Codex elicitation mode: %s", p.Mode)
		}
		e.ThreadID, e.TurnID = p.ThreadId, p.TurnId
		r.Kind = "elicitation"
		r.Message = p.Message
		r.URL = p.Url
		r.Schema = p.RequestedSchema
	default:
		e.Type = "raw"
		e.Request = nil
		e.Payload = runtime.BoundedJSON(f.Params)
		return e, fmt.Errorf("unsupported Codex server request: %s", f.Method)
	}
	return e, nil
}
func approvalOptions(decisions []string) []runtime.RequestOption {
	var out []runtime.RequestOption
	for _, d := range decisions {
		switch d {
		case "accept":
			out = append(out, runtime.RequestOption{ID: "allow_once", Label: "Allow once"})
		case "acceptForSession":
			out = append(out, runtime.RequestOption{ID: "allow_always", Label: "Allow for this session"})
		case "decline":
			out = append(out, runtime.RequestOption{ID: "deny", Label: "Deny"})
		}
	}
	return out
}
func Response(r runtime.AgentRequest, a runtime.RequestAnswer) any {
	switch r.Kind {
	case "approval":
		if r.Method == "item/permissions/requestApproval" {
			var p PermissionsRequestApprovalParams
			_ = json.Unmarshal(r.Payload, &p)
			permissions := json.RawMessage(`{}`)
			scope := runtime.JSON("turn")
			if a.Decision == "allow_once" || a.Decision == "allow_always" {
				permissions = p.Permissions
			}
			if a.Decision == "allow_always" {
				scope = runtime.JSON("session")
			}
			return PermissionsRequestApprovalResponse{Permissions: permissions, Scope: scope}
		}
		d := "cancel"
		switch a.Decision {
		case "allow_once":
			d = "accept"
		case "allow_always":
			d = "acceptForSession"
		case "deny":
			d = "decline"
		}
		return CommandExecutionRequestApprovalResponse{Decision: runtime.JSON(d)}
	case "user_input":
		answers := map[string]any{}
		for k, v := range a.Answers {
			answers[k] = map[string]any{"answers": v}
		}
		return ToolRequestUserInputResponse{Answers: runtime.JSON(answers)}
	case "elicitation":
		return McpServerElicitationRequestResponse{Action: a.Decision, Content: a.Content}
	}
	return nil
}
func SessionError(err error) error {
	if err == nil {
		return nil
	}
	s := strings.ToLower(err.Error())
	if strings.Contains(s, "locked") || strings.Contains(s, "busy") || strings.Contains(s, "already running") {
		return fmt.Errorf("session open in another Codex window: %w", err)
	}
	return err
}
