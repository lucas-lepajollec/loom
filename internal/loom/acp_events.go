package loom

import (
	"path/filepath"
	"strings"
	"time"
)

func (p *acpBinding) handleNotification(f acpFrame) {
	if f.Method != "session/update" {
		return
	}
	params, err := acpParseUpdate(f.Params)
	if err != nil {
		return
	}
	p.mu.Lock()
	if p.loading || (p.state.NativeSessionID != "" && params.SessionID != p.state.NativeSessionID) {
		p.mu.Unlock()
		return
	}
	u := params.Update
	if !p.active && (u["sessionUpdate"] == "agent_message_chunk" || u["sessionUpdate"] == "agent_thought_chunk" || u["sessionUpdate"] == "tool_call" || u["sessionUpdate"] == "tool_call_update") {
		p.mu.Unlock()
		return
	}
	// A Loom model may lack Codex metadata; this warning is not an answer.
	if p.loomModel && (u["sessionUpdate"] == "agent_message_chunk" || u["sessionUpdate"] == "agent_thought_chunk") {
		c, _ := u["content"].(map[string]any)
		text, _ := c["text"].(string)
		if strings.HasPrefix(strings.TrimSpace(text), "Warning: Model metadata for") {
			p.mu.Unlock()
			return
		}
	}
	e := acpUpdateEvent(u, p.tools)
	if e == nil {
		p.mu.Unlock()
		return
	}
	var filesEvent DiscussionEvent
	switch u["sessionUpdate"] {
	case "agent_message_chunk":
		text, _ := e["text"].(string)
		p.answer += text
	case "tool_call", "tool_call_update":
		tool, _ := e["tool"].(map[string]any)
		if tool["status"] == "completed" {
			filesEvent = p.noteToolDiffsLocked(tool)
		}
	case "usage_update":
		p.state.ACPUsage = acpCloneMap(e)
	case "current_mode_update":
		p.state.Mode, _ = e["current"].(string)
	case "config_option_update":
		options, _ := e["options"].([]map[string]any)
		p.applyConfig(options)
	case "available_commands_update":
		p.state.Commands, _ = e["commands"].([]map[string]any)
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
