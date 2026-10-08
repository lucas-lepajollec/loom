package loom

import (
	"encoding/json"
	agent "github.com/lucas-lepajollec/loom/internal/loom/runtime"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

func (p *acpBinding) handleNotification(f acpFrame) {
	if f.Method != "session/update" {
		p.publishRawACP(f)
		return
	}
	params, err := acpParseUpdate(f.Params)
	if err != nil {
		p.publishRawACP(f)
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
	// pi-acp sends its startup notice and provider retries as answer text.
	if u["sessionUpdate"] == "agent_message_chunk" {
		c, _ := u["content"].(map[string]any)
		text, _ := c["text"].(string)
		if trimmed := strings.TrimSpace(text); trimmed != "" && (trimmed == p.prelude || piRetryNotice(trimmed)) {
			if piRetryNotice(trimmed) && strings.HasPrefix(trimmed, "Retrying") {
				p.retries++
			}
			p.mu.Unlock()
			return
		}
	}
	e := acpUpdateEvent(u, p.tools)
	if e == nil {
		p.mu.Unlock()
		p.publishRawACP(f)
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
	e["agent_event"] = canonicalACPEvent(p.agentID, f, e)
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

var piRetryPattern = regexp.MustCompile(`^(Retrying( \(attempt \d+/\d+, waiting \d+s\))?\.\.\.)+( ?Retry finished, resuming\.)?$|^Retry finished, resuming\.$`)

func piRetryNotice(text string) bool { return piRetryPattern.MatchString(text) }

// piTurnError reads Pi's own session journal for the error of a turn that
// produced no answer: pi-acp does not forward provider errors.
func piTurnError(since time.Time) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	files, _ := filepath.Glob(filepath.Join(home, ".pi", "agent", "sessions", "*", "*.jsonl"))
	newest, newestTime := "", since
	for _, f := range files {
		if info, err := os.Stat(f); err == nil && info.ModTime().After(newestTime) {
			newest, newestTime = f, info.ModTime()
		}
	}
	if newest == "" {
		return ""
	}
	data, err := os.ReadFile(newest)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	for i := len(lines) - 1; i >= 0 && i >= len(lines)-20; i-- {
		var entry struct {
			Message struct {
				Role         string `json:"role"`
				StopReason   string `json:"stopReason"`
				ErrorMessage string `json:"errorMessage"`
			} `json:"message"`
		}
		if json.Unmarshal([]byte(lines[i]), &entry) != nil || entry.Message.Role != "assistant" {
			continue
		}
		if entry.Message.StopReason == "error" {
			return entry.Message.ErrorMessage
		}
		return ""
	}
	return ""
}

func (p *acpBinding) publishRawACP(f acpFrame) {
	e := AgentEvent{Type: "raw", Runtime: p.agentID, Method: f.Method, Raw: agent.BoundedJSON(f.Raw), Payload: agent.BoundedJSON(f.Params)}
	p.publish(DiscussionEvent{"type": "tool_end", "tool": map[string]any{"id": "raw:" + newSessionID(), "kind": "other", "title": f.Method, "status": "completed", "output": string(e.Payload)}, "agent_event": e})
}
func canonicalACPEvent(name string, f acpFrame, d DiscussionEvent) AgentEvent {
	e := AgentEvent{Type: "raw", Runtime: name, Method: f.Method, Raw: agent.BoundedJSON(f.Raw), Payload: agent.JSON(d)}
	switch d["type"] {
	case "text_delta", "reasoning_delta":
		e.Type = "content.delta"
		e.Stream = "assistant_text"
		if d["type"] == "reasoning_delta" {
			e.Stream = "reasoning_text"
		}
		e.Delta, _ = d["text"].(string)
	case "tool_start", "tool_delta", "tool_end":
		e.Type = "item.started"
		if d["type"] == "tool_delta" {
			e.Type = "item.updated"
		}
		if d["type"] == "tool_end" {
			e.Type = "item.completed"
		}
		tool, _ := d["tool"].(map[string]any)
		e.ItemID, _ = tool["id"].(string)
		e.ItemType = "tool_call"
		e.Payload = agent.JSON(tool)
	case "plan":
		e.Type = "item.updated"
		e.ItemType = "plan"
	case "usage":
		e.Type = "context.updated"
	}
	return e
}
