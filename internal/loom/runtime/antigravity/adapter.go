package antigravity

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"github.com/lucas-lepajollec/loom/internal/loom/runtime"
	"io"
	"math"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

var agyModelID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,199}$`)

// Read commands use a fixed executable/argument list, never a shell. Credentials
// stay with the CLI. Its stderr can contain private data and is never returned.
func Read(ctx context.Context, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	path, err := exec.LookPath("agy")
	if err != nil {
		return nil, errors.New("agy CLI missing from Loom's PATH")
	}
	dir, err := os.MkdirTemp("", "loom-agy-read-")
	if err != nil {
		return nil, errors.New("temporary directory unavailable")
	}
	defer os.RemoveAll(dir) // Only the directory just created by this call.
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = dir
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	out := &boundedCLIOutput{limit: 1 << 20}
	cmd.Stdout = out
	if cmd.Run() != nil || out.overflow {
		return nil, errors.New("CLI reading unavailable: check native login or try again")
	}
	return out.data, nil
}

type boundedCLIOutput struct {
	data     []byte
	limit    int
	overflow bool
}

func (b *boundedCLIOutput) Write(p []byte) (int, error) {
	if len(b.data)+len(p) > b.limit {
		b.overflow = true
		return 0, errors.New("CLI output too large")
	}
	b.data = append(b.data, p...)
	return len(p), nil
}

func DiscoverModels(ctx context.Context) ([]string, error) {
	out, err := Read(ctx, "models")
	if err != nil {
		return nil, err
	}
	return ParseModels(out)
}

// ParseModels retains native catalog order and the bounded, tab-separated format.
func ParseModels(out []byte) ([]string, error) {
	models := []string{}
	seen := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		id, _, tab := strings.Cut(line, "\t")
		id = strings.TrimSpace(id)
		if tab && agyModelID.MatchString(id) && !seen[id] {
			models = append(models, id)
			seen[id] = true
		}
		if len(models) > 128 {
			return nil, errors.New("Antigravity catalog too large")
		}
	}
	if len(models) == 0 {
		return nil, errors.New("unrecognized Antigravity catalog")
	}
	return models, nil
}

// Adapter receives the selected model and optional executable from Loom. It owns
// only the fresh-text CLI turn, never application state or native permissions.
type Adapter struct {
	Model      string
	Executable string
}

// TokenUsage is reported native usage projected into the discussion vocabulary.
type TokenUsage struct {
	Input    int64 `json:"prompt_tokens"`
	Output   int64 `json:"completion_tokens"`
	Total    int64 `json:"total_tokens"`
	Thinking int64 `json:"thinking_tokens,omitempty"`
	Cached   int64 `json:"cache_read_tokens,omitempty"`
}

type Event struct {
	Content         string
	Usage           *TokenUsage
	HarnessEvent    *HarnessEvent
	NativeSessionID string
	DurationSeconds float64
}

type Usage struct {
	Input    int64 `json:"input_tokens"`
	Output   int64 `json:"output_tokens"`
	Total    int64 `json:"total_tokens"`
	Thinking int64 `json:"thinking_tokens"`
	Cached   int64 `json:"cache_read_tokens"`
}
type Result struct {
	Status         string  `json:"status"`
	Response       string  `json:"response"`
	ConversationID string  `json:"conversation_id"`
	Usage          *Usage  `json:"usage"`
	Duration       float64 `json:"duration_seconds"`
}

func (a Adapter) Run(ctx context.Context, messages any, emit func(Event) bool) (string, error) {
	if !agyModelID.MatchString(a.Model) {
		return "", errors.New("invalid Antigravity model")
	}
	path := a.Executable
	if path == "" {
		var err error
		path, err = exec.LookPath("agy")
		if err != nil {
			return "", errors.New("agy CLI missing from Loom's PATH")
		}
	}
	dir, err := os.MkdirTemp("", "loom-agy-turn-")
	if err != nil {
		return "", errors.New("temporary directory unavailable")
	}
	defer os.RemoveAll(dir)
	// JSON role/content is data inside a single prompt, not fabricated native
	// tool messages. No --continue: switching away and back cannot duplicate state.
	history, err := json.Marshal(messages)
	if err != nil || len(history) > 256<<10 {
		return "", errors.New("invalid or oversized Antigravity context")
	}
	prompt := "Continue the following Loom text conversation. Follow its system instructions and answer the last user message. Prior assistant messages are context, not tool results. This bridge is for text: do not use tools, files, commands, browser or subagents.\nLoom portable messages (JSON):\n" + string(history)
	input, _ := json.Marshal(map[string]any{"event": "user", "message": map[string]string{"content": prompt}})
	cmd := exec.CommandContext(ctx, path, "--input-format", "stream-json", "--output-format", "stream-json", "--model", a.Model, "--disable-slash-commands", "--print-timeout", "170s")
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(string(input) + "\n")
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	stdout, err := cmd.StdoutPipe()
	if err != nil || cmd.Start() != nil {
		return "", errors.New("Antigravity could not start")
	}
	stopRead := context.AfterFunc(ctx, func() { _ = stdout.Close() })
	defer stopRead()
	defer func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	answer, err := ConsumeStream(ctx, stdout, emit)
	if err != nil {
		_ = cmd.Process.Kill()
	}
	waitErr := cmd.Wait()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if err != nil {
		return "", err
	}
	if waitErr != nil {
		return "", errors.New("Antigravity interrupted the turn; partial text is preserved")
	}
	return answer, nil
}

func ConsumeStream(ctx context.Context, reader io.Reader, emit func(Event) bool) (string, error) {
	scanner := bufio.NewScanner(io.LimitReader(reader, 2<<20))
	scanner.Buffer(make([]byte, 4096), 512<<10)
	var answer strings.Builder
	done := false
	steps := map[int]Usage{}
	for scanner.Scan() {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		var event struct {
			Event  string `json:"event"`
			Result Result `json:"result"`
			Step   struct {
				Index int    `json:"step_index"`
				Type  string `json:"step_type"`
				State string `json:"state"`
				Text  string `json:"text_delta"`
				Tool  string `json:"tool_name"`
				Usage *Usage `json:"usage"`
				Info  struct {
					Name       string                     `json:"name"`
					Parameters map[string]json.RawMessage `json:"parameters"`
					Error      json.RawMessage            `json:"error"`
				} `json:"tool_info"`
			} `json:"step_update"`
		}
		if json.Unmarshal(scanner.Bytes(), &event) != nil {
			return "", errors.New("unrecognized Antigravity stream")
		}
		if event.Event == "step_update" {
			if u := event.Step.Usage; ValidUsage(u) && event.Step.Index >= 0 && event.Step.Index < 128 {
				steps[event.Step.Index] = *u
				var total TokenUsage
				for _, step := range steps {
					total.Input += step.Input
					total.Output += step.Output
					total.Total += step.Total
					total.Thinking += step.Thinking
					total.Cached += step.Cached
				}
				if !emit(Event{Usage: &total}) {
					return "", context.Canceled
				}
			}
			if event.Step.Type == "agent_response" && event.Step.Text != "" {
				if answer.Len()+len(event.Step.Text) > 256<<10 {
					return "", errors.New("Antigravity response too long")
				}
				answer.WriteString(event.Step.Text)
				if !emit(Event{Content: event.Step.Text}) {
					return "", context.Canceled
				}
			}
			if event.Step.Type == "tool" {
				// Metadata only; private tool arguments/results are not echoed.
				name := event.Step.Tool
				if name == "" {
					name = event.Step.Info.Name
				}
				if len(name) > 100 {
					name = "native tool"
				}
				state := event.Step.State
				if state != "ACTIVE" && state != "DONE" {
					state = "UNKNOWN"
				}
				h := HarnessEvent{Index: event.Step.Index, Name: name, State: state}
				h.Failed = len(event.Step.Info.Error) > 0 && string(event.Step.Info.Error) != "null"
				// Only a bounded file target, never code, shell arguments or output.
				if name == "write_to_file" || name == "replace_file_content" || name == "multi_replace_file_content" {
					var target string
					if json.Unmarshal(event.Step.Info.Parameters["TargetFile"], &target) == nil && len(target) <= 500 && !strings.ContainsAny(target, "\r\n\x00") {
						h.FileTarget = target
					}
				}
				if !emit(Event{HarnessEvent: &h}) {
					return "", context.Canceled
				}
			}
		}
		if event.Event == "result" {
			r := event.Result
			if ValidUsage(r.Usage) {
				if !emit(Event{Usage: &TokenUsage{Input: r.Usage.Input, Output: r.Usage.Output, Total: r.Usage.Total, Thinking: r.Usage.Thinking, Cached: r.Usage.Cached}}) {
					return "", context.Canceled
				}
			}
			if r.Duration > 0 && r.Duration <= 3600 && !math.IsNaN(r.Duration) && !math.IsInf(r.Duration, 0) {
				if !emit(Event{DurationSeconds: r.Duration}) {
					return "", context.Canceled
				}
			}
			if !emit(Event{NativeSessionID: r.ConversationID}) {
				return "", context.Canceled
			}
			if r.Status != "SUCCESS" {
				return "", errors.New("Antigravity did not complete the turn; check its permissions, model and quota in its CLI")
			}
			if answer.Len() == 0 && r.Response != "" {
				if len(r.Response) > 256<<10 {
					return "", errors.New("Antigravity response too long")
				}
				answer.WriteString(r.Response)
				if !emit(Event{Content: r.Response}) {
					return "", context.Canceled
				}
			}
			// Reject inconsistent final text instead of silently storing two answers.
			if answer.String() != r.Response {
				return "", errors.New("final Antigravity response inconsistent with the stream")
			}
			done = true
		}
	}
	if scanner.Err() != nil || !done || answer.Len() == 0 {
		return "", errors.New("incomplete Antigravity stream; no automatic resend")
	}
	return answer.String(), nil
}

type HarnessEvent = runtime.HarnessEvent

func ValidUsage(u *Usage) bool {
	return u != nil && u.Input >= 0 && u.Output >= 0 && u.Total >= 0 && u.Thinking >= 0 && u.Cached >= 0 && u.Input <= 1e9 && u.Output <= 1e9 && u.Total <= 2e9 && u.Thinking <= 1e9 && u.Cached <= 1e9
}

func Installed() bool { _, err := exec.LookPath("agy"); return err == nil }
