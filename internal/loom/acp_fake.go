package loom

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
)

// runFakeACP is a deterministic stdio agent, used by the test subprocess and
// the explicitly enabled development command. It never contacts a provider.
func runFakeACP(in io.Reader, out io.Writer) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var writeMu, mu sync.Mutex
	var next atomic.Uint64
	pending := map[string]chan acpFrame{}
	sessions := map[string]string{}
	turns := map[string]context.CancelFunc{}
	send := func(v any) {
		b, _ := json.Marshal(v)
		writeMu.Lock()
		_, _ = out.Write(append(b, '\n'))
		writeMu.Unlock()
	}
	reply := func(id json.RawMessage, result any) {
		send(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	}
	update := func(sid string, u map[string]any) {
		send(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": sid, "update": u}})
	}
	request := func(turnCtx context.Context, method string, params any) (acpFrame, error) {
		id, _ := json.Marshal(fmt.Sprintf("fake-%d", next.Add(1)))
		ch := make(chan acpFrame, 1)
		mu.Lock()
		pending[string(id)] = ch
		mu.Unlock()
		defer func() { mu.Lock(); delete(pending, string(id)); mu.Unlock() }()
		send(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(id), "method": method, "params": params})
		select {
		case f := <-ch:
			return f, nil
		case <-turnCtx.Done():
			return acpFrame{}, turnCtx.Err()
		case <-ctx.Done():
			return acpFrame{}, ctx.Err()
		}
	}
	config := func(value any) []any {
		return []any{map[string]any{"id": "verbosity", "name": "Verbosity", "type": "select", "currentValue": value, "options": []any{map[string]any{"value": "short", "name": "Short"}, map[string]any{"value": "long", "name": "Long"}}}}
	}
	modes := map[string]any{"currentModeId": "default", "availableModes": []any{map[string]any{"id": "default", "name": "Default"}, map[string]any{"id": "plan", "name": "Plan"}}}
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 64<<10), acpMaxFrame)
	for sc.Scan() {
		var f acpFrame
		if json.Unmarshal(sc.Bytes(), &f) != nil {
			return
		}
		if f.Method == "" {
			mu.Lock()
			ch := pending[string(f.ID)]
			mu.Unlock()
			if ch != nil {
				ch <- f
			}
			continue
		}
		var params map[string]any
		_ = json.Unmarshal(f.Params, &params)
		sid, _ := params["sessionId"].(string)
		switch f.Method {
		case "initialize":
			reply(f.ID, map[string]any{"protocolVersion": 1, "agentInfo": map[string]any{"name": "loom-fake-acp", "version": "1"}, "agentCapabilities": map[string]any{"loadSession": true, "mcpCapabilities": map[string]any{"http": true}}})
		case "session/new", "session/load":
			cwd, _ := params["cwd"].(string)
			if sid == "" {
				sid = fmt.Sprintf("fake-session-%d", next.Add(1))
			}
			mu.Lock()
			sessions[sid] = cwd
			mu.Unlock()
			if f.Method == "session/load" {
				update(sid, map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "OLD REPLAY"}})
			}
			reply(f.ID, map[string]any{"sessionId": sid, "modes": modes, "configOptions": config("short")})
		case "session/set_mode":
			update(sid, map[string]any{"sessionUpdate": "current_mode_update", "currentModeId": params["modeId"]})
			reply(f.ID, map[string]any{})
		case "session/set_config_option":
			reply(f.ID, map[string]any{"configOptions": config(params["value"])})
		case "session/cancel":
			mu.Lock()
			stop := turns[sid]
			mu.Unlock()
			if stop != nil {
				stop()
			}
		case "session/prompt":
			mu.Lock()
			cwd := sessions[sid]
			turnCtx, stop := context.WithCancel(ctx)
			turns[sid] = stop
			mu.Unlock()
			go func(f acpFrame, sid, cwd string, params map[string]any) {
				defer stop()
				prompt, _ := json.Marshal(params["prompt"])
				if strings.Contains(string(prompt), `\"content\":\"wait\"`) || strings.Contains(string(prompt), `"text":"wait"`) {
					<-turnCtx.Done()
					reply(f.ID, map[string]any{"stopReason": "cancelled"})
					return
				}
				path := filepath.Join(cwd, "loom-fake-acp.txt")
				update(sid, map[string]any{"sessionUpdate": "available_commands_update", "availableCommands": []any{map[string]any{"name": "review", "description": "Review changes"}}})
				update(sid, map[string]any{"sessionUpdate": "current_mode_update", "currentModeId": "default"})
				update(sid, map[string]any{"sessionUpdate": "config_option_update", "configOptions": config("short")})
				update(sid, map[string]any{"sessionUpdate": "agent_thought_chunk", "content": map[string]any{"type": "text", "text": "Reported thought"}})
				update(sid, map[string]any{"sessionUpdate": "plan", "entries": []any{map[string]any{"content": "Write a fixture", "priority": "medium", "status": "in_progress"}}})
				tool := map[string]any{"toolCallId": "fake-tool", "title": "Write fixture", "kind": "edit", "status": "pending", "locations": []any{map[string]any{"path": path, "line": 1}}, "rawInput": map[string]any{"path": path}}
				initial := acpCloneMap(tool)
				initial["sessionUpdate"] = "tool_call"
				update(sid, initial)
				update(sid, map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "fake-tool", "status": "in_progress"})
				options := []any{map[string]any{"optionId": "once", "name": "Allow once", "kind": "allow_once"}, map[string]any{"optionId": "always", "name": "Allow always", "kind": "allow_always"}, map[string]any{"optionId": "reject", "name": "Reject", "kind": "reject_once"}}
				permission, err := request(turnCtx, "session/request_permission", map[string]any{"sessionId": sid, "toolCall": tool, "options": options})
				var decision struct {
					Outcome struct {
						Outcome  string `json:"outcome"`
						OptionID string `json:"optionId"`
					} `json:"outcome"`
				}
				_ = json.Unmarshal(permission.Result, &decision)
				allowed := err == nil && decision.Outcome.Outcome == "selected" && decision.Outcome.OptionID != "reject"
				if allowed {
					_, _ = request(turnCtx, "fs/write_text_file", map[string]any{"sessionId": sid, "path": path, "content": "fixture\n"})
					_, _ = request(turnCtx, "fs/read_text_file", map[string]any{"sessionId": sid, "path": path, "line": 1, "limit": 1})
					outside, _ := request(turnCtx, "fs/write_text_file", map[string]any{"sessionId": sid, "path": filepath.Join(cwd, "..", "loom-fake-acp-outside.txt"), "content": "forbidden"})
					if outside.Error == nil {
						update(sid, map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "OUTSIDE ACCEPTED"}})
					}
				}
				status := "completed"
				if !allowed {
					status = "failed"
				}
				update(sid, map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "fake-tool", "status": status, "content": []any{map[string]any{"type": "diff", "path": path, "oldText": "", "newText": "fixture\n"}, map[string]any{"type": "content", "content": map[string]any{"type": "text", "text": "Fixture result"}}}})
				update(sid, map[string]any{"sessionUpdate": "plan", "entries": []any{map[string]any{"content": "Write a fixture", "priority": "medium", "status": "completed"}}})
				update(sid, map[string]any{"sessionUpdate": "usage_update", "used": 24, "size": 4096, "cost": map[string]any{"amount": 0.01, "currency": "USD"}})
				update(sid, map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "OK"}})
				reply(f.ID, map[string]any{"stopReason": "end_turn"})
			}(f, sid, cwd, params)
		default:
			send(map[string]any{"jsonrpc": "2.0", "id": f.ID, "error": map[string]any{"code": -32601, "message": "Unsupported"}})
		}
	}
}
