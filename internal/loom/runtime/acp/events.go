package acp

import (
	"encoding/json"
	"strings"
)

func Clip(text string, n int) string {
	if len(text) <= n {
		return text
	}
	return strings.ToValidUTF8(text[:max(0, n-3)], "") + "…"
}
func Compact(value any, n int) string { b, _ := json.Marshal(value); return Clip(string(b), n) }
func CloneMap(value map[string]any) map[string]any {
	b, _ := json.Marshal(value)
	var copy map[string]any
	_ = json.Unmarshal(b, &copy)
	return copy
}
func Tool(tools map[string]map[string]any, update map[string]any) map[string]any {
	id, _ := update["toolCallId"].(string)
	old := tools[id]
	if old == nil {
		old = map[string]any{"id": id, "title": "", "kind": "other", "status": "pending", "locations": []any{}, "diffs": []any{}, "output": "", "input": ""}
	}
	t := CloneMap(old)
	for _, key := range []string{"title", "kind", "status"} {
		if v, ok := update[key]; ok && v != nil {
			t[key] = v
		}
	}
	if locations, ok := update["locations"].([]any); ok {
		normalized := []map[string]any{}
		for _, raw := range locations {
			if location, ok := raw.(map[string]any); ok {
				item := map[string]any{"path": location["path"]}
				if line, ok := location["line"]; ok {
					item["line"] = line
				}
				normalized = append(normalized, item)
			}
		}
		t["locations"] = normalized
	}
	if input, ok := update["rawInput"]; ok && input != nil {
		t["input"] = Compact(input, 4<<10)
	}
	// Some adapters (codex-acp) stream command output in _meta instead of
	// content: terminal_output_delta chunks, or terminal_output once.
	streamed := ""
	if meta, ok := update["_meta"].(map[string]any); ok {
		if exit, ok := meta["terminal_exit"].(map[string]any); ok {
			if code, ok := exit["exit_code"]; ok {
				t["exit_code"] = code
			}
			if signal, ok := exit["signal"]; ok {
				t["signal"] = signal
			}
		}
		for _, key := range []string{"terminal_output_delta", "terminal_output"} {
			if chunk, ok := meta[key].(map[string]any); ok {
				if data, ok := chunk["data"].(string); ok {
					streamed += data
				}
			}
		}
	}
	if streamed != "" {
		prev, _ := t["output"].(string)
		t["output"] = Clip(prev+streamed, 64<<10)
	}
	if contents, ok := update["content"].([]any); ok {
		diffs := []any{}
		output := ""
		for _, raw := range contents {
			item, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			switch item["type"] {
			case "diff":
				before, _ := item["oldText"].(string)
				after, _ := item["newText"].(string)
				diffs = append(diffs, map[string]any{"path": item["path"], "old": before, "new": after})
			case "content":
				content, _ := item["content"].(map[string]any)
				if content["type"] == "text" {
					text, _ := content["text"].(string)
					output += text
				}
			}
		}
		t["diffs"] = diffs
		if output != "" || streamed == "" && t["output"] == "" {
			t["output"] = Clip(output, 64<<10)
		}
	} else if v, ok := update["rawOutput"]; ok && v != nil && t["output"] == "" {
		if text, ok := v.(string); ok {
			t["output"] = Clip(text, 64<<10)
		} else {
			t["output"] = Compact(v, 64<<10)
		}
	}
	tools[id] = t
	return CloneMap(t)
}

// SessionUpdate is the session/update params envelope.
type SessionUpdate struct {
	SessionID string         `json:"sessionId"`
	Update    map[string]any `json:"update"`
}

func ParseUpdate(raw json.RawMessage) (SessionUpdate, error) {
	var p SessionUpdate
	err := json.Unmarshal(raw, &p)
	return p, err
}

// UpdateEvent maps a live protocol update; the caller owns tools and synchronization.
func UpdateEvent(u map[string]any, tools map[string]map[string]any) map[string]any {
	var e map[string]any
	switch u["sessionUpdate"] {
	case "agent_message_chunk", "agent_thought_chunk":
		c, _ := u["content"].(map[string]any)
		if c["type"] != "text" {
			return nil
		}
		text, _ := c["text"].(string)
		kind := "text_delta"
		if u["sessionUpdate"] == "agent_thought_chunk" {
			kind = "reasoning_delta"
		}
		e = map[string]any{"type": kind, "text": text}
	case "tool_call", "tool_call_update":
		if u["sessionUpdate"] == "tool_call" {
			id, _ := u["toolCallId"].(string)
			delete(tools, id)
		}
		tool := Tool(tools, u)
		kind := "tool_start"
		if u["sessionUpdate"] == "tool_call_update" {
			kind = "tool_delta"
		}
		if tool["status"] == "completed" || tool["status"] == "failed" {
			kind = "tool_end"
		}
		e = map[string]any{"type": kind, "tool": tool}
	case "plan", "plan_update":
		entries := []map[string]any{}
		if raw, ok := u["entries"].([]any); ok {
			for _, entry := range raw {
				if v, ok := entry.(map[string]any); ok {
					entries = append(entries, map[string]any{"content": v["content"], "status": v["status"], "priority": v["priority"]})
				}
			}
		}
		e = map[string]any{"type": "plan", "entries": entries}
	case "usage_update":
		e = map[string]any{"type": "usage", "context": map[string]any{"used": u["used"], "size": u["size"]}, "cost": u["cost"]}
	case "current_mode_update":
		mode, _ := u["currentModeId"].(string)
		e = map[string]any{"type": "mode", "current": mode}
	case "config_option_update":
		var options []map[string]any
		b, _ := json.Marshal(u["configOptions"])
		_ = json.Unmarshal(b, &options)
		e = map[string]any{"type": "config", "options": options}
	case "available_commands_update":
		var commands []map[string]any
		b, _ := json.Marshal(u["availableCommands"])
		_ = json.Unmarshal(b, &commands)
		for i, c := range commands {
			commands[i] = map[string]any{"name": c["name"], "description": c["description"]}
		}
		e = map[string]any{"type": "commands", "commands": commands}
	default:
		return nil
	}
	return e
}
