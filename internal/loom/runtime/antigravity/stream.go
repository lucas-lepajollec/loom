package antigravity

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/runtime"
	"github.com/lucas-lepajollec/loom/internal/loom/runtime/acp"
	"github.com/lucas-lepajollec/loom/internal/loom/runtime/agentstdio"
)

const TestedVersion = "1.3.1"
const Protocol = "agy-stream-json"

// Efforts is the installed 1.3.1 help vocabulary, not fabricated model IDs.
var Efforts = []string{"low", "medium", "high", "xhigh", "max"}

type TurnConfig struct {
	ConversationID string
	Model          string
	Effort         string
	Mode           string
	Permission     string
	Sandbox        bool
	AdditionalDirs []string
}

func (c TurnConfig) Args() ([]string, error) {
	if c.Model != "" && c.Model != "default" && !agyModelID.MatchString(c.Model) {
		return nil, errors.New("invalid Antigravity model")
	}
	if c.ConversationID != "" && !agyModelID.MatchString(c.ConversationID) {
		return nil, errors.New("invalid Antigravity conversation ID")
	}
	if c.Effort != "" {
		known := false
		for _, effort := range Efforts {
			known = known || effort == c.Effort
		}
		if !known {
			return nil, errors.New("unsupported Antigravity effort")
		}
	}
	if c.Mode != "" && c.Mode != "default" && c.Mode != "accept-edits" && c.Mode != "plan" && c.Mode != "full" {
		return nil, errors.New("unsupported Antigravity mode")
	}
	if c.Permission != "" && c.Permission != "ask" && c.Permission != "edits" && c.Permission != "full" {
		return nil, errors.New("unsupported Antigravity permission level")
	}
	args := []string{"--input-format", "stream-json", "--output-format", "stream-json", "--disable-slash-commands", "--print-timeout", "0"}
	if c.Model != "" && c.Model != "default" {
		args = append(args, "--model", c.Model)
	}
	if c.ConversationID != "" {
		args = append(args, "--conversation", c.ConversationID)
	}
	if c.Effort != "" {
		args = append(args, "--effort", c.Effort)
	}
	mode := c.Mode
	if (mode == "" || mode == "default") && c.Permission == "edits" {
		mode = "accept-edits"
	}
	if mode == "plan" || mode == "accept-edits" {
		args = append(args, "--mode", mode)
	}
	// Explicit full selection is the only launch-scoped permission bypass.
	// A denial never upgrades the mode or causes another prompt.
	if mode == "full" || c.Permission == "full" && mode != "plan" {
		args = append(args, "--dangerously-skip-permissions")
	}
	if c.Sandbox {
		args = append(args, "--sandbox")
	}
	for _, dir := range c.AdditionalDirs {
		args = append(args, "--add-dir", dir)
	}
	return args, nil
}

// RunStream submits exactly one user message, closes stdin, drains stdout, and
// verifies process exit before publishing completion. Only Loom's child tree
// is stopped on cancellation. The CLI owns credentials and conversation state.
func RunStream(ctx context.Context, cmd *exec.Cmd, prompt, conversationID, turnID string, emit runtime.EventSink[runtime.AgentEvent], onSession func(string)) error {
	if len(prompt) > 1<<20 {
		return errors.New("Antigravity prompt too large")
	}
	input, _ := json.Marshal(map[string]any{"event": "user", "message": map[string]string{"content": prompt}})
	cmd.Stdin = strings.NewReader(string(input) + "\n")
	acp.ProcessGroup(cmd)
	var diagnostics agentstdio.Diagnostics
	cmd.Stderr = &diagnostics
	cmd.WaitDelay = time.Second
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	mapper := Mapper{ThreadID: conversationID, TurnID: turnID}
	if err = cmd.Start(); err == nil {
		stop := context.AfterFunc(ctx, func() {
			acp.KillProcessGroup(cmd)
			_ = stdout.Close()
		})
		err = ReadStream(stdout, &mapper, func(event runtime.AgentEvent) bool {
			if ctx.Err() != nil {
				return false
			}
			if mapper.ThreadID != "" {
				onSession(mapper.ThreadID)
			}
			return emit(event)
		})
		if err != nil {
			acp.KillProcessGroup(cmd)
		}
		waitErr := cmd.Wait()
		stop()
		// Kill only descendants in this child's group after drain as well.
		acp.KillProcessGroup(cmd)
		if err == nil {
			err = waitErr
		}
	}
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if err != nil || !mapper.done {
		if detail := diagnostics.String(cmd.Env); detail != "" && ctx.Err() == nil {
			if err == nil {
				err = errors.New(detail)
			} else {
				err = fmt.Errorf("%w: %s", err, detail)
			}
		}
	}
	events, err := mapper.Finish(err)
	for _, event := range events {
		if !emit(event) && err == nil {
			err = context.Canceled
		}
	}
	return err
}

type streamUsage struct {
	Input     *int64 `json:"input_tokens"`
	Output    *int64 `json:"output_tokens"`
	Total     *int64 `json:"total_tokens"`
	Reasoning *int64 `json:"thinking_tokens"`
	Cached    *int64 `json:"cache_read_tokens"`
}
type streamStep struct {
	ConversationID string          `json:"conversation_id"`
	Index          *int            `json:"step_index"`
	Type           string          `json:"step_type"`
	State          string          `json:"state"`
	Text           string          `json:"text_delta"`
	Tool           string          `json:"tool_name"`
	Usage          *streamUsage    `json:"usage"`
	Info           json.RawMessage `json:"tool_info"`
}

// Mapper keeps only display state for one turn. Unknown steps retain raw rows;
// undocumented thoughts, plan and context variants are never reconstructed.
type Mapper struct {
	ThreadID string
	TurnID   string
	started  bool
	done     bool
	status   string
	failure  string
	text     string
	result   json.RawMessage
	lastRaw  json.RawMessage
	items    map[int]bool
	usage    map[int]streamUsage
}

func (m *Mapper) event(raw []byte, method string) runtime.AgentEvent {
	return runtime.AgentEvent{Type: "raw", Runtime: "antigravity", ThreadID: m.ThreadID, TurnID: m.TurnID, Method: method, Payload: runtime.BoundedJSON(raw), Raw: runtime.BoundedJSON(raw)}
}
func (m *Mapper) Feed(raw []byte) ([]runtime.AgentEvent, error) {
	m.lastRaw = runtime.BoundedJSON(raw)
	var frame struct {
		Event          string          `json:"event"`
		ConversationID string          `json:"conversation_id"`
		Step           *streamStep     `json:"step_update"`
		Result         json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(raw, &frame); err != nil || frame.Event == "" {
		return nil, errors.New("invalid Antigravity stream event")
	}
	if m.done {
		return []runtime.AgentEvent{m.event(raw, frame.Event)}, nil
	}
	id := frame.ConversationID
	if frame.Step != nil && frame.Step.ConversationID != "" {
		id = frame.Step.ConversationID
	}
	var result struct {
		ConversationID string            `json:"conversation_id"`
		Status         string            `json:"status"`
		Response       *string           `json:"response"`
		Error          json.RawMessage   `json:"error"`
		Denied         []json.RawMessage `json:"denied_actions"`
	}
	if frame.Event == "result" {
		if json.Unmarshal(frame.Result, &result) != nil || result.Status == "" {
			return nil, errors.New("invalid Antigravity result")
		}
		if result.ConversationID != "" {
			id = result.ConversationID
		}
	}
	if id != "" {
		if !agyModelID.MatchString(id) {
			return nil, errors.New("invalid Antigravity conversation ID in stream")
		}
		if m.ThreadID != "" && m.ThreadID != id {
			return nil, errors.New("Antigravity returned a different conversation ID; resume refused")
		}
		m.ThreadID = id
	}
	e := m.event(raw, frame.Event)
	if m.items == nil {
		m.items = map[int]bool{}
		m.usage = map[int]streamUsage{}
	}
	events := []runtime.AgentEvent{}
	if !m.started && m.ThreadID != "" {
		m.started = true
		start := e
		start.Type = "turn.started"
		events = append(events, start)
	}
	switch frame.Event {
	case "init":
		// Preserve effective native permission mode, model, tools and cwd.
		events = append(events, e)
	case "step_update":
		s := frame.Step
		if s == nil || s.Index == nil || *s.Index < 0 || *s.Index > 1000000 || s.Type == "" {
			return nil, errors.New("invalid Antigravity step")
		}
		index := *s.Index
		if len(m.items)+len(m.usage) > 8192 {
			return nil, errors.New("too many Antigravity steps")
		}
		e.ItemID = "agy:" + m.TurnID + ":" + strconv.Itoa(index)
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(raw, &fields)
		e.Payload = runtime.BoundedJSON(fields["step_update"])
		if s.Usage != nil {
			m.usage[index] = *s.Usage // Per-step snapshots, not additive deltas.
		}
		switch s.Type {
		case "agent_response":
			if len(m.text)+len(s.Text) > 1<<20 {
				return nil, errors.New("Antigravity response too large")
			}
			if s.Text != "" {
				m.text += s.Text
				e.Type, e.Stream, e.Delta = "content.delta", "assistant_text", s.Text
				e.ItemID = "agy:" + m.TurnID + ":response"
				events = append(events, e)
			}
		case "tool":
			var info struct {
				Name       string                     `json:"name"`
				Parameters map[string]json.RawMessage `json:"parameters"`
				Output     string                     `json:"output"`
				Error      json.RawMessage            `json:"error"`
			}
			if len(s.Info) > 0 && json.Unmarshal(s.Info, &info) != nil {
				return nil, errors.New("invalid Antigravity tool info")
			}
			name := s.Tool
			if name == "" {
				name = info.Name
			}
			e.ItemType = "tool_call"
			payload := map[string]any{"toolName": name, "tool_info": json.RawMessage(s.Info), "aggregatedOutput": info.Output}
			if command := parameter(info.Parameters, "CommandLine", "Command"); command != "" {
				e.ItemType = "command_execution"
				payload["command"] = command
			} else if (name == "write_to_file" || name == "replace_file_content" || name == "multi_replace_file_content" || name == "edit_file") && parameter(info.Parameters, "TargetFile", "AbsolutePath", "File", "Path") != "" {
				e.ItemType = "file_change"
				payload["path"] = parameter(info.Parameters, "TargetFile", "AbsolutePath", "File", "Path")
				// Report the target and native output; never read disk or invent a diff.
			}
			e.Type, e.Status = "item.started", "in_progress"
			if m.items[index] {
				e.Type = "item.updated"
			}
			m.items[index] = true
			if s.State == "DONE" {
				e.Type, e.Status = "item.completed", "completed"
			} else if s.State != "ACTIVE" {
				e.Type, e.Status = "item.completed", "failed"
			}
			if errorText(info.Error) != "" {
				e.Type, e.Status, e.Error = "item.completed", "failed", errorText(info.Error)
				payload["error"] = json.RawMessage(info.Error)
			}
			e.Payload = runtime.JSON(payload)
			events = append(events, e)
		case "user_input":
			e.Type, e.ItemType, e.Status = "item.completed", "user_message", "completed"
			events = append(events, e)
		default:
			// Includes checkpoints/subagents, permission/question tools and any
			// future reasoning/plan/context fields with no accepted wire contract.
			e.Method = "step_update/" + s.Type
			events = append(events, e)
		}
	case "result":
		m.done, m.status, m.result = true, result.Status, runtime.BoundedJSON(raw)
		m.failure = errorText(result.Error)
		if result.Response != nil && *result.Response != m.text {
			if len(*result.Response) > 1<<20 {
				return nil, errors.New("Antigravity response too large")
			}
			m.text = *result.Response
			text := e
			text.Type, text.Stream, text.Delta, text.Replace = "content.delta", "assistant_text", m.text, true
			text.ItemID = "agy:" + m.TurnID + ":response"
			events = append(events, text)
		}
		// Result usage is cumulative session accounting, not turn spend or KV
		// occupancy. Keep it verbatim in the raw result row.
		events = append(events, e)
		if len(result.Denied) > 0 {
			denied := e
			denied.Type = "warning"
			denied.Message = "Antigravity denied actions under native policy; no automatic retry was submitted."
			denied.Payload = runtime.JSON(result.Denied)
			events = append(events, denied)
		}
	default:
		events = append(events, e)
	}
	return events, nil
}

func parameter(p map[string]json.RawMessage, keys ...string) string {
	for _, key := range keys {
		var value string
		if json.Unmarshal(p[key], &value) == nil && value != "" {
			return value
		}
	}
	return ""
}
func errorText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var err struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &err) == nil && err.Message != "" {
		return err.Message
	}
	return string(raw)
}

func (m *Mapper) Finish(processErr error) ([]runtime.AgentEvent, error) {
	raw := m.result
	if len(raw) == 0 {
		raw = m.lastRaw
	}
	e := m.event(raw, "result")
	e.Type, e.Status = "turn.completed", "failed"
	var err error
	switch m.status {
	case "SUCCESS":
		e.Status = "completed"
		if m.ThreadID == "" {
			e.Status = "failed"
			err = errors.New("Antigravity completed without a conversation ID")
		}
	case "CANCELED", "CANCELLED":
		e.Status = "cancelled"
		err = context.Canceled
	case "INTERRUPTED":
		e.Status = "interrupted"
		err = errors.New("Antigravity turn interrupted")
	default:
		err = fmt.Errorf("Antigravity turn ended with %s", m.status)
	}
	if !m.done {
		err = errors.New("Antigravity stream ended without a result")
	}
	if m.failure != "" {
		if e.Status != "cancelled" && e.Status != "interrupted" {
			e.Status = "failed"
		}
		if e.Status != "cancelled" {
			err = errors.New(m.failure)
		}
	}
	if processErr != nil {
		e.Status = "failed"
		if m.failure == "" {
			err = processErr
		}
		if errors.Is(processErr, context.Canceled) || errors.Is(processErr, context.DeadlineExceeded) {
			e.Status, err = "cancelled", processErr
		}
	}
	if err != nil {
		e.Error = err.Error()
		if m.failure != "" && (processErr == nil || !errors.Is(processErr, context.Canceled) && !errors.Is(processErr, context.DeadlineExceeded)) {
			e.Error = m.failure
		}
	}
	events := []runtime.AgentEvent{}
	if usage := m.turnUsage(); usage != nil {
		u := e
		u.Type, u.Status, u.Error, u.Usage = "usage.spent", "", "", usage
		events = append(events, u)
	}
	if err != nil {
		failure := e
		failure.Type = "error"
		events = append(events, failure)
	}
	events = append(events, e)
	return events, err
}

func (m *Mapper) turnUsage() *runtime.AgentUsage {
	if len(m.usage) == 0 {
		return nil
	}
	usage := &runtime.AgentUsage{Scope: "turn"}
	indices := []int{}
	for index := range m.usage {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	for field := 0; field < 5; field++ {
		var total int64
		known := true
		for _, index := range indices {
			u := m.usage[index]
			value := []*int64{u.Input, u.Output, u.Total, u.Reasoning, u.Cached}[field]
			if value == nil || *value < 0 || *value > 1<<40 {
				known = false
				break
			}
			total += *value
		}
		if known {
			*([]**int64{&usage.Input, &usage.Output, &usage.Total, &usage.Reasoning, &usage.Cached}[field]) = &total
		}
	}
	return usage
}

// ReadStream is useful for protocol fixture replay without any native CLI.
func ReadStream(reader io.Reader, mapper *Mapper, emit runtime.EventSink[runtime.AgentEvent]) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), agentstdio.MaxFrame)
	for scanner.Scan() {
		events, err := mapper.Feed(scanner.Bytes())
		if err != nil {
			return err
		}
		for _, event := range events {
			if !emit(event) {
				return context.Canceled
			}
		}
	}
	return scanner.Err()
}
