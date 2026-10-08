package loom

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	agent "github.com/lucas-lepajollec/loom/internal/loom/runtime"
)

type agentProjection struct {
	approvals map[string]bool
	tools     map[string]map[string]any
	raw       int
	ended     bool
}

func newAgentProjection() *agentProjection {
	return &agentProjection{approvals: map[string]bool{}, tools: map[string]map[string]any{}}
}
func (p *agentProjection) event(e agent.AgentEvent) DiscussionEvent {
	d := DiscussionEvent{"type": e.Type, "agent_event": e}
	switch e.Type {
	case "content.delta":
		switch e.Stream {
		case "assistant_text":
			d["type"] = "text_delta"
			d["text"] = e.Delta
			if e.Replace {
				d["replace"] = true
			}
		case "reasoning_text":
			d["type"] = "reasoning_delta"
			d["text"] = e.Delta
			d["summary"] = e.Runtime == "codex"
		case "command_output", "file_change_output":
			tool := p.tools[e.ItemID]
			if tool == nil {
				tool = map[string]any{"id": e.ItemID, "kind": "execute", "title": e.ItemID, "status": "in_progress"}
				p.tools[e.ItemID] = tool
			}
			previous, _ := tool["output"].(string)
			tool["output"] = boundedBytes(previous+e.Delta, 256<<10)
			d["type"] = "tool_delta"
			d["tool"] = acpCloneMap(tool)
		}
	case "item.started", "item.updated", "item.completed":
		if e.ItemType == "assistant_message" || e.ItemType == "user_message" || e.ItemType == "reasoning" {
			break
		}
		var payload map[string]any
		_ = json.Unmarshal(e.Payload, &payload)
		if e.ItemType == "plan" {
			entries := []map[string]any{}
			if plan, ok := payload["plan"].([]any); ok {
				for _, raw := range plan {
					v, _ := raw.(map[string]any)
					entries = append(entries, map[string]any{"content": v["step"], "status": v["status"]})
				}
			}
			d["type"] = "plan"
			d["entries"] = entries
			break
		}
		tool := p.tools[e.ItemID]
		if tool == nil {
			tool = map[string]any{"id": e.ItemID, "kind": "other", "title": e.ItemType, "status": "in_progress"}
			p.tools[e.ItemID] = tool
		}
		kind := "other"
		switch e.ItemType {
		case "command_execution":
			kind = "execute"
		case "file_change":
			kind = "edit"
		case "web_search":
			kind = "search"
		}
		tool["kind"] = kind
		for _, k := range []string{"command", "toolName", "tool", "query", "name"} {
			if v, ok := payload[k].(string); ok && v != "" {
				tool["title"] = v
				break
			}
		}
		if out, ok := payload["aggregatedOutput"].(string); ok {
			tool["output"] = out
		}
		if out, ok := payload["diff"].(string); ok {
			tool["output"] = out
		}
		// Pi partialResult is cumulative. Replace it rather than appending.
		for _, k := range []string{"partialResult", "result"} {
			if out, ok := payload[k].(map[string]any); ok {
				if content, ok := out["content"].([]any); ok {
					var text strings.Builder
					for _, raw := range content {
						v, _ := raw.(map[string]any)
						if t, ok := v["text"].(string); ok {
							text.WriteString(t)
						}
					}
					tool["output"] = text.String()
				}
			}
		}
		if e.ItemType == "file_change" {
			if diff, ok := payload["diff"].([]any); ok {
				tool["output"] = string(agent.JSON(diff))
			}
			if changes, ok := payload["changes"].([]any); ok {
				tool["output"] = string(agent.JSON(changes))
			}
		}
		if e.Status != "" {
			tool["status"] = e.Status
		}
		d["type"] = "tool_start"
		if e.Type == "item.updated" {
			d["type"] = "tool_delta"
		}
		if e.Type == "item.completed" {
			d["type"] = "tool_end"
			if e.Status == "" {
				tool["status"] = "completed"
			}
		}
		d["tool"] = acpCloneMap(tool)
	case "request.opened":
		d["request"] = e.Request
		if e.Request.Kind == "approval" {
			p.approvals[e.Request.ID] = true
			opts := []map[string]any{}
			for _, o := range e.Request.Options {
				kind := o.ID
				if kind == "deny" {
					kind = "reject_once"
				}
				opts = append(opts, map[string]any{"id": o.ID, "name": o.Label, "kind": kind})
			}
			tool := p.tools[e.Request.ItemID]
			if tool == nil {
				tool = map[string]any{"id": e.Request.ItemID, "kind": "other", "title": e.Request.Message}
				var payload map[string]any
				_ = json.Unmarshal(e.Request.Payload, &payload)
				if command, ok := payload["command"].(string); ok {
					tool["title"] = command
					tool["kind"] = "execute"
				}
			}
			d["type"] = "approval_request"
			d["approval"] = map[string]any{"id": e.Request.ID, "tool": tool, "options": opts}
		}
	case "request.resolved":
		d["request_id"] = e.RequestID
		d["outcome"] = e.Outcome
		if p.approvals[e.RequestID] {
			d["type"] = "approval_resolved"
			d["id"] = e.RequestID
			d["option_id"] = e.Decision
			delete(p.approvals, e.RequestID)
		}
	// Canonical outcome also reaches new request renderers; legacy approvals
	// consume the alias only when the matching request was an approval.
	case "raw", "warning":
		p.raw++
		d["type"] = "tool_end"
		d["tool"] = map[string]any{"id": fmt.Sprintf("raw:%d", p.raw), "kind": "other", "title": firstNonEmpty(e.Message, e.Method, "Agent event"), "status": "completed", "output": string(e.Payload)}
	case "error":
		d["type"] = "error"
		d["error"] = e.Error
	case "turn.completed":
		p.ended = true
		for id, tool := range p.tools {
			status, _ := tool["status"].(string)
			if status != "completed" && status != "failed" {
				tool["status"] = "interrupted"
				p.tools[id] = tool
			}
		}
	case "usage.spent", "context.updated":
		d["type"] = "usage"
		d["usage"] = e.Usage
		if e.Usage != nil && e.Usage.Total != nil {
			d["context"] = map[string]any{"used": *e.Usage.Total, "size": e.Usage.ContextWindow}
		}
	}
	return d
}

// An upstream may fail/interrupt without completing every tool item. Close
// those display rows explicitly, preserving the provider's terminal frame.
func (p *agentProjection) closeItems(end AgentEvent) []AgentEvent {
	ids := []string{}
	for id, tool := range p.tools {
		status, _ := tool["status"].(string)
		if status != "completed" && status != "failed" && status != "interrupted" {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	events := []AgentEvent{}
	for _, id := range ids {
		tool := p.tools[id]
		kind := "tool_call"
		switch tool["kind"] {
		case "execute":
			kind = "command_execution"
		case "edit":
			kind = "file_change"
		case "search":
			kind = "web_search"
		}
		events = append(events, AgentEvent{Type: "item.completed", Runtime: end.Runtime, ThreadID: end.ThreadID, TurnID: end.TurnID, ItemID: id, ItemType: kind, Status: "interrupted", Error: "turn ended before the item completed", Payload: agent.JSON(tool), Raw: end.Raw})
	}
	return events
}
