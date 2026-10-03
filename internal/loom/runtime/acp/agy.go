package acp

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Antigravity speaks no ACP. `loom agy-acp` is a small ACP agent that drives
// the agy CLI underneath (headless, stream-json), so Antigravity gets the same
// treatment as every other harness: work folder, extra folders, modes, model,
// tools and changed files in the thread, and resuming its own conversation.
//
// agy cannot ask for permission in headless mode, so the access level is a
// mode: "default" (risky tools are refused by agy), "accept-edits", "plan" and
// "full" (every tool approved, --dangerously-skip-permissions).

type agySession struct {
	cwd   string
	extra []string
	conv  string // agy conversation id, for --conversation
	model string
	mode  string
	turn  context.CancelFunc
	seen  map[string]string // file contents read or written this session, for diffs
}

var agyModes = []map[string]any{
	{"id": "default", "name": "Cautious", "description": "Risky actions (commands…) are denied: Antigravity cannot request approval in headless mode."},
	{"id": "accept-edits", "name": "Auto edits", "description": "File reads and edits are allowed."},
	{"id": "plan", "name": "Plan", "description": "Antigravity prepares a plan without making changes."},
	{"id": "full", "name": "Allow everything", "description": "All actions are allowed without asking."},
}

func (b AgyBridge) Run(in io.Reader, out io.Writer) {
	var writeMu, mu sync.Mutex
	sessions := map[string]*agySession{}
	send := func(v any) {
		b, _ := json.Marshal(v)
		writeMu.Lock()
		_, _ = out.Write(append(b, '\n'))
		writeMu.Unlock()
	}
	reply := func(id json.RawMessage, result any) {
		send(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	}
	fail := func(id json.RawMessage, msg string) {
		send(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -32000, "message": msg}})
	}
	update := func(sid string, u map[string]any) {
		send(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": sid, "update": u}})
	}
	models, catalogErr := DiscoverAgyModelsNamed(context.Background(), b.Read)
	defaultModel := ""
	if len(models) > 0 {
		defaultModel = models[0][0]
	}
	config := func(s *agySession) []any {
		opts := []any{}
		for _, m := range models {
			opts = append(opts, map[string]any{"value": m[0], "name": m[1]})
		}
		return []any{map[string]any{"id": "model", "name": "Model", "category": "model", "type": "select", "currentValue": s.model, "options": opts}}
	}
	modes := func(s *agySession) map[string]any {
		return map[string]any{"currentModeId": s.mode, "availableModes": agyModes}
	}
	newSession := func(params map[string]any, id string) *agySession {
		s := &agySession{model: defaultModel, mode: "default", conv: id}
		s.cwd, _ = params["cwd"].(string)
		if dirs, ok := params["additionalDirectories"].([]any); ok {
			for _, d := range dirs {
				if p, ok := d.(string); ok && p != "" {
					s.extra = append(s.extra, p)
				}
			}
		}
		return s
	}
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 64<<10), MaxFrame)
	for sc.Scan() {
		var f Frame
		if json.Unmarshal(sc.Bytes(), &f) != nil || f.Method == "" {
			continue // responses to our (none) requests, or noise
		}
		var params map[string]any
		_ = json.Unmarshal(f.Params, &params)
		sid, _ := params["sessionId"].(string)
		switch f.Method {
		case "initialize":
			ver, _ := b.Read(context.Background(), "--version")
			reply(f.ID, map[string]any{"protocolVersion": 1,
				"agentInfo":         map[string]any{"name": "Antigravity", "version": strings.TrimSpace(string(ver))},
				"agentCapabilities": map[string]any{"loadSession": true, "promptCapabilities": map[string]any{}}})
		case "session/new":
			if catalogErr != nil || len(models) == 0 {
				fail(f.ID, "Antigravity native catalog unavailable: check login on this machine")
				continue
			}
			id := "loom-agy-" + randomHex(8)
			s := newSession(params, "")
			mu.Lock()
			sessions[id] = s
			mu.Unlock()
			reply(f.ID, map[string]any{"sessionId": id, "modes": modes(s), "configOptions": config(s)})
		case "session/load", "session/resume":
			if catalogErr != nil || len(models) == 0 {
				fail(f.ID, "Antigravity native catalog unavailable: check login on this machine")
				continue
			}
			// The id is agy's own conversation id: the next prompt resumes it.
			s := newSession(params, sid)
			mu.Lock()
			sessions[sid] = s
			mu.Unlock()
			reply(f.ID, map[string]any{"modes": modes(s), "configOptions": config(s)})
		case "session/set_mode":
			mode, _ := params["modeId"].(string)
			mu.Lock()
			s := sessions[sid]
			ok := s != nil && validAgyMode(mode)
			if ok {
				s.mode = mode
			}
			mu.Unlock()
			if !ok {
				fail(f.ID, "unknown mode")
				continue
			}
			reply(f.ID, map[string]any{})
		case "session/set_config_option":
			value, _ := params["value"].(string)
			mu.Lock()
			s := sessions[sid]
			ok := s != nil && params["configId"] == "model" && agyModelKnown(models, value)
			if ok {
				s.model = value
			}
			mu.Unlock()
			if !ok {
				fail(f.ID, "unknown option")
				continue
			}
			reply(f.ID, map[string]any{"configOptions": config(s)})
		case "session/cancel":
			mu.Lock()
			if s := sessions[sid]; s != nil && s.turn != nil {
				s.turn()
			}
			mu.Unlock()
		case "session/prompt":
			mu.Lock()
			s := sessions[sid]
			mu.Unlock()
			if s == nil {
				fail(f.ID, "unknown session")
				continue
			}
			text := promptText(params["prompt"])
			ctx, cancel := context.WithCancel(context.Background())
			mu.Lock()
			s.turn = cancel
			mu.Unlock()
			go func(id json.RawMessage, s *agySession) {
				defer cancel()
				stop, err := b.turn(ctx, s, text, func(u map[string]any) { update(sid, u) })
				if err != nil && ctx.Err() == nil {
					fail(id, err.Error())
					return
				}
				reply(id, map[string]any{"stopReason": stop})
			}(f.ID, s)
		default:
			fail(f.ID, "method not supported by Antigravity")
		}
	}
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func validAgyMode(m string) bool {
	for _, x := range agyModes {
		if x["id"] == m {
			return true
		}
	}
	return false
}

func agyModelKnown(models [][2]string, id string) bool {
	for _, m := range models {
		if m[0] == id {
			return true
		}
	}
	return false
}

func promptText(p any) string {
	parts := []string{}
	if list, ok := p.([]any); ok {
		for _, x := range list {
			if m, ok := x.(map[string]any); ok && m["type"] == "text" {
				if t, ok := m["text"].(string); ok {
					parts = append(parts, t)
				}
			}
		}
	}
	return strings.Join(parts, "\n")
}

// discoverAgyModelsNamed returns [id, display name] in agy's order.
func DiscoverAgyModelsNamed(ctx context.Context, read AgyRead) ([][2]string, error) {
	out, err := read(ctx, "models")
	if err != nil {
		return nil, err
	}
	list := [][2]string{}
	for _, line := range strings.Split(string(out), "\n") {
		id, name, ok := strings.Cut(line, "\t")
		id, name = strings.TrimSpace(id), strings.TrimSpace(name)
		if ok && id != "" && !strings.ContainsAny(id, " /") {
			if name == "" {
				name = id
			}
			list = append(list, [2]string{id, name})
		}
	}
	return list, nil
}

// agyArgs builds the headless agy invocation for one turn.
func agyArgs(s *agySession) []string {
	args := []string{"--input-format", "stream-json", "--output-format", "stream-json", "--disable-slash-commands", "--print-timeout", "0"}
	if s.model != "" {
		args = append(args, "--model", s.model)
	}
	if s.conv != "" {
		args = append(args, "--conversation", s.conv)
	}
	switch s.mode {
	case "accept-edits", "plan":
		args = append(args, "--mode", s.mode)
	case "full":
		args = append(args, "--dangerously-skip-permissions")
	}
	for _, d := range s.extra {
		args = append(args, "--add-dir", d)
	}
	return args
}

// agyToolKind maps agy tool names to ACP tool kinds.
func agyToolKind(name string) string {
	switch {
	case strings.Contains(name, "view") || strings.Contains(name, "read") || strings.HasPrefix(name, "list_"):
		return "read"
	case strings.Contains(name, "write") || strings.Contains(name, "replace") || strings.Contains(name, "edit"):
		return "edit"
	case strings.Contains(name, "command") || strings.Contains(name, "terminal"):
		return "execute"
	case strings.Contains(name, "search") || strings.Contains(name, "grep") || strings.Contains(name, "find"):
		return "search"
	case strings.Contains(name, "browser") || strings.Contains(name, "url") || strings.Contains(name, "web"):
		return "fetch"
	}
	return "other"
}

func firstString(params map[string]json.RawMessage, keys ...string) string {
	for _, k := range keys {
		var v string
		if json.Unmarshal(params[k], &v) == nil && v != "" {
			return v
		}
	}
	return ""
}

// agyToolCall describes one agy tool step as an ACP tool call.
func agyToolCall(name string, params map[string]json.RawMessage, cwd string) map[string]any {
	title := name
	path := firstString(params, "TargetFile", "AbsolutePath", "File", "Path", "DirectoryPath", "SearchPath")
	cmd := firstString(params, "CommandLine", "Command")
	switch {
	case cmd != "":
		title = cmd
	case path != "":
		title = name + " " + path
	}
	call := map[string]any{"title": title, "kind": agyToolKind(name), "rawInput": params}
	if path != "" {
		if !filepath.IsAbs(path) && cwd != "" {
			path = filepath.Join(cwd, path)
		}
		call["locations"] = []any{map[string]any{"path": path}}
		// A whole-file write carries its content: show it as a diff.
		if content := firstString(params, "CodeContent", "Content"); content != "" && agyToolKind(name) == "edit" {
			call["content"] = []any{map[string]any{"type": "diff", "path": path, "oldText": nil, "newText": content}}
		}
	}
	return call
}

// agyTurn runs one prompt and translates agy's stream into ACP updates.
func (b AgyBridge) turn(ctx context.Context, s *agySession, text string, update func(map[string]any)) (string, error) {
	path, err := exec.LookPath(b.Executable)
	if err != nil {
		return "", fmt.Errorf("agy CLI not found")
	}
	input, _ := json.Marshal(map[string]any{"event": "user", "message": map[string]string{"content": text}})
	cmd := exec.CommandContext(ctx, path, agyArgs(s)...)
	cmd.Dir = s.cwd
	if cmd.Dir == "" {
		cmd.Dir, _ = os.UserHomeDir()
	}
	cmd.Stdin = strings.NewReader(string(input) + "\n")
	cmd.Stderr = io.Discard
	cmd.WaitDelay = 2 * time.Second
	stdout, err := cmd.StdoutPipe()
	if err != nil || cmd.Start() != nil {
		return "", fmt.Errorf("Antigravity could not start")
	}
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	status := ""
	// agy reports only the target of a write, after writing it: the bridge keeps
	// the contents of files seen this session and the turn's start time, so a
	// write becomes an exact diff when the file was seen, a creation otherwise.
	before := map[int]*string{}
	turnStart := time.Now().Add(-time.Second)
	if s.seen == nil {
		s.seen = map[string]string{}
	}
	for sc.Scan() {
		var ev struct {
			Event string `json:"event"`
			Init  struct {
				Conv string `json:"conversation_id"`
			} `json:"-"`
			Conv   string `json:"conversation_id"`
			Result struct {
				Status string `json:"status"`
				Conv   string `json:"conversation_id"`
				Usage  *struct {
					Total int `json:"total_tokens"`
				} `json:"usage"`
			} `json:"result"`
			Step struct {
				Index int    `json:"step_index"`
				Type  string `json:"step_type"`
				State string `json:"state"`
				Text  string `json:"text_delta"`
				Tool  string `json:"tool_name"`
				Info  struct {
					Name       string                     `json:"name"`
					Parameters map[string]json.RawMessage `json:"parameters"`
					Error      json.RawMessage            `json:"error"`
				} `json:"tool_info"`
				Conv string `json:"conversation_id"`
			} `json:"step_update"`
		}
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue
		}
		if c := FirstNonEmpty(ev.Conv, ev.Step.Conv, ev.Result.Conv); c != "" {
			s.conv = c
		}
		switch ev.Event {
		case "step_update":
			switch ev.Step.Type {
			case "agent_response":
				if ev.Step.Text != "" {
					update(map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": ev.Step.Text}})
				}
			case "tool":
				name := FirstNonEmpty(ev.Step.Tool, ev.Step.Info.Name, "tool")
				id := fmt.Sprintf("agy-%d", ev.Step.Index)
				target := agyEditTarget(name, ev.Step.Info.Parameters, s.cwd)
				if read := agyReadTarget(name, ev.Step.Info.Parameters, s.cwd); read != "" && ev.Step.State == "DONE" {
					if c := readSmallText(read); c != nil {
						s.seen[read] = *c
					}
				}
				if ev.Step.State == "ACTIVE" {
					if target != "" {
						before[ev.Step.Index] = readSmallText(target)
					}
					call := agyToolCall(name, ev.Step.Info.Parameters, s.cwd)
					call["sessionUpdate"], call["toolCallId"], call["status"] = "tool_call", id, "in_progress"
					update(call)
				} else if ev.Step.State == "DONE" {
					st := "completed"
					if len(ev.Step.Info.Error) > 0 && string(ev.Step.Info.Error) != "null" {
						st = "failed"
					}
					u := agyToolCall(name, ev.Step.Info.Parameters, s.cwd)
					u["sessionUpdate"], u["toolCallId"], u["status"] = "tool_call_update", id, st
					if after := readSmallText(target); st == "completed" && target != "" && after != nil {
						var old *string
						if prev, ok := s.seen[target]; ok {
							old = &prev
						} else if b := before[ev.Step.Index]; b != nil && *b != *after {
							old = b
						}
						changed := old != nil && *old != *after
						if info, err := os.Stat(target); err == nil && info.ModTime().After(turnStart) {
							changed = true
						}
						if changed && (old == nil || *old != *after) {
							var oldText any
							if old != nil {
								oldText = *old
							}
							u["content"] = []any{map[string]any{"type": "diff", "path": target, "oldText": oldText, "newText": *after}}
						}
						s.seen[target] = *after
					}
					update(u)
				}
			}
		case "result":
			status = ev.Result.Status
		}
	}
	waitErr := cmd.Wait()
	if ctx.Err() != nil {
		return "cancelled", nil
	}
	if status == "" && waitErr != nil {
		return "", fmt.Errorf("Antigravity interrupted the turn")
	}
	if status != "" && status != "SUCCESS" {
		update(map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "\n\n(Antigravity : " + strings.ToLower(status) + ")"}})
	}
	return "end_turn", nil
}

// agyEditTarget is the absolute file an editing tool writes, if any.
func agyEditTarget(name string, params map[string]json.RawMessage, cwd string) string {
	if agyToolKind(name) != "edit" {
		return ""
	}
	path := firstString(params, "TargetFile", "AbsolutePath", "File", "Path")
	if path == "" {
		return ""
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}
	return filepath.Clean(path)
}

// agyReadTarget is the absolute file a reading tool opens, if any.
func agyReadTarget(name string, params map[string]json.RawMessage, cwd string) string {
	if agyToolKind(name) != "read" {
		return ""
	}
	path := firstString(params, "AbsolutePath", "File", "Path", "TargetFile")
	if path == "" {
		return ""
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}
	return filepath.Clean(path)
}

// readSmallText reads a text file up to 1 MB; nil when absent or too large.
func readSmallText(path string) *string {
	if path == "" {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	text := string(b)
	return &text
}

func FirstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

// AgyRead is an explicit native account/CLI read supplied by the application.
type AgyRead func(context.Context, ...string) ([]byte, error)

// AgyBridge owns only its in-process ACP agent state, independent of Loom sessions.
type AgyBridge struct {
	Read       AgyRead
	Executable string
}
