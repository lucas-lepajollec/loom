package loom

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"time"
)

func acpClip(text string, n int) string {
	if len(text) <= n {
		return text
	}
	return strings.ToValidUTF8(text[:max(0, n-3)], "") + "…"
}
func acpCompact(value any, n int) string { b, _ := json.Marshal(value); return acpClip(string(b), n) }
func acpCloneMap(value map[string]any) map[string]any {
	b, _ := json.Marshal(value)
	var copy map[string]any
	_ = json.Unmarshal(b, &copy)
	return copy
}
func (p *acpBinding) tool(update map[string]any) map[string]any {
	id, _ := update["toolCallId"].(string)
	old := p.tools[id]
	if old == nil {
		old = map[string]any{"id": id, "title": "", "kind": "other", "status": "pending", "locations": []any{}, "diffs": []any{}, "output": "", "input": ""}
	}
	t := acpCloneMap(old)
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
		t["input"] = acpCompact(input, 4<<10)
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
		t["output"] = acpClip(output, 64<<10)
	} else if v, ok := update["rawOutput"]; ok && v != nil {
		if text, ok := v.(string); ok {
			t["output"] = acpClip(text, 64<<10)
		} else {
			t["output"] = acpCompact(v, 64<<10)
		}
	}
	p.tools[id] = t
	return acpCloneMap(t)
}
func (p *acpBinding) handleNotification(f acpFrame) {
	if f.Method != "session/update" {
		return
	}
	var params struct {
		SessionID string         `json:"sessionId"`
		Update    map[string]any `json:"update"`
	}
	if json.Unmarshal(f.Params, &params) != nil {
		return
	}
	p.mu.Lock()
	if p.loading || (p.state.NativeSessionID != "" && params.SessionID != p.state.NativeSessionID) {
		p.mu.Unlock()
		return
	}
	u := params.Update
	e := DiscussionEvent{}
	var filesEvent DiscussionEvent
	if !p.active && (u["sessionUpdate"] == "agent_message_chunk" || u["sessionUpdate"] == "agent_thought_chunk" || u["sessionUpdate"] == "tool_call" || u["sessionUpdate"] == "tool_call_update") {
		p.mu.Unlock()
		return
	}
	switch u["sessionUpdate"] {
	case "agent_message_chunk", "agent_thought_chunk":
		c, _ := u["content"].(map[string]any)
		if c["type"] != "text" {
			p.mu.Unlock()
			return
		}
		text, _ := c["text"].(string)
		kind := "text_delta"
		if u["sessionUpdate"] == "agent_thought_chunk" {
			kind = "reasoning_delta"
		}
		if kind == "text_delta" {
			p.answer += text
		}
		e = DiscussionEvent{"type": kind, "text": text}
	case "tool_call", "tool_call_update":
		if u["sessionUpdate"] == "tool_call" {
			id, _ := u["toolCallId"].(string)
			delete(p.tools, id)
		}
		tool := p.tool(u)
		kind := "tool_start"
		if u["sessionUpdate"] == "tool_call_update" {
			kind = "tool_delta"
		}
		if tool["status"] == "completed" || tool["status"] == "failed" {
			kind = "tool_end"
		}
		if tool["status"] == "completed" {
			filesEvent = p.noteToolDiffsLocked(tool)
		}
		e = DiscussionEvent{"type": kind, "tool": tool}
	case "plan", "plan_update":
		entries := []map[string]any{}
		if raw, ok := u["entries"].([]any); ok {
			for _, entry := range raw {
				if v, ok := entry.(map[string]any); ok {
					entries = append(entries, map[string]any{"content": v["content"], "status": v["status"], "priority": v["priority"]})
				}
			}
		}
		e = DiscussionEvent{"type": "plan", "entries": entries}
	case "usage_update":
		e = DiscussionEvent{"type": "usage", "context": map[string]any{"used": u["used"], "size": u["size"]}, "cost": u["cost"]}
		p.state.ACPUsage = acpCloneMap(e)
	case "current_mode_update":
		p.state.Mode, _ = u["currentModeId"].(string)
		e = DiscussionEvent{"type": "mode", "current": p.state.Mode}
	case "config_option_update":
		var options []map[string]any
		b, _ := json.Marshal(u["configOptions"])
		_ = json.Unmarshal(b, &options)
		p.applyConfig(options)
		e = DiscussionEvent{"type": "config", "options": options}
	case "available_commands_update":
		var commands []map[string]any
		b, _ := json.Marshal(u["availableCommands"])
		_ = json.Unmarshal(b, &commands)
		for i, c := range commands {
			commands[i] = map[string]any{"name": c["name"], "description": c["description"]}
		}
		p.state.Commands = commands
		e = DiscussionEvent{"type": "commands", "commands": commands}
	default:
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()
	p.publish(e)
	if filesEvent != nil {
		p.publish(filesEvent)
	}
}

// Agents that edit with their own tools (Claude Code, Codex) report the change
// as a diff instead of calling fs/write_text_file. Record those diffs in the
// changed files too, only for paths inside the discussion's folders.
func (p *acpBinding) noteToolDiffsLocked(tool map[string]any) DiscussionEvent {
	diffs, _ := tool["diffs"].([]any)
	if len(diffs) == 0 {
		return nil
	}
	changed := false
	for _, raw := range diffs {
		d, _ := raw.(map[string]any)
		path, _ := d["path"].(string)
		before, _ := d["old"].(string)
		after, _ := d["new"].(string)
		if path == "" || !filepath.IsAbs(path) || !p.insideRootsLocked(path) {
			continue
		}
		if p.state.FileBaselines == nil {
			p.state.FileBaselines = map[string]string{}
		}
		if _, seen := p.state.FileBaselines[path]; !seen && len(before) <= acpMaxFile {
			p.state.FileBaselines[path] = before
		}
		add, del := acpLineCounts(before, after)
		change := ACPChangedFile{Path: path, Op: "edit", Add: add, Del: del, At: time.Now().UnixMilli()}
		if before == "" {
			change.Op = "create"
		}
		found := false
		for i := range p.state.Files {
			if p.state.Files[i].Path == path {
				p.state.Files[i], found = change, true
			}
		}
		if !found {
			p.state.Files = append(p.state.Files, change)
		}
		changed = true
	}
	if !changed {
		return nil
	}
	return DiscussionEvent{"type": "files", "files": append([]ACPChangedFile{}, p.state.Files...)}
}

func (p *acpBinding) insideRootsLocked(path string) bool {
	clean := filepath.Clean(path)
	if p.remoteRoot != "" {
		return strings.HasPrefix(clean, p.remoteRoot+"/")
	}
	if real, err := filepath.EvalSymlinks(clean); err == nil {
		clean = real
	}
	for _, root := range p.roots {
		if acpInside(root.Name(), clean) {
			return true
		}
	}
	return false
}
