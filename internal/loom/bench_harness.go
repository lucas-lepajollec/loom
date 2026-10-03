package loom

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const benchSystemPrompt = "Answer the user request directly."

func benchPrompt(t benchTest) (string, int) {
	if t.Kind == "perf" || t.ID == benchTestPerf {
		return benchCorpusPrompt(2000), 300
	}
	n := t.MaxTokens
	if n <= 0 {
		n = 256
	}
	return t.Prompt, n
}

// This is a native account invocation, not ACP: no discussion, project,
// permissions, skills, MCP or credentials are exported by Loom. Do not replace
// safe mode with bare mode: recent Claude builds disable OAuth in bare mode.
func claudeBenchArgs(model string) []string {
	return []string{"--safe-mode", "--restricted", "--setting-sources", "", "--settings", `{"disableAllHooks":true,"forceLoginMethod":"claudeai"}`,
		"--tools", "", "--disallowedTools", "*", "--strict-mcp-config", "--mcp-config", `{"mcpServers":{}}`,
		"--disable-slash-commands", "--no-chrome", "--no-session-persistence", "--system-prompt", benchSystemPrompt,
		"--model", model, "--max-turns", "1", "--print", "--output-format", "stream-json", "--verbose", "--include-partial-messages"}
}

// Remove provider overrides for this invocation only. Native OAuth remains
// owned by Claude; Loom never reads its credential files or copies an OAuth key.
func claudeBenchEnv(env []string, maxTokens int) []string {
	out := []string{}
	for _, item := range env {
		key, _, _ := strings.Cut(item, "=")
		if strings.HasPrefix(key, "ANTHROPIC_") || strings.HasPrefix(key, "CLAUDE_CODE_") || key == "CLAUDE_CONFIG_DIR" {
			// Preserve the operator-selected native account directory, not API overrides.
			if key == "CLAUDE_CONFIG_DIR" {
				out = append(out, item)
			}
			continue
		}
		out = append(out, item)
	}
	return append(out, "CLAUDE_CODE_MAX_OUTPUT_TOKENS="+strconv.Itoa(maxTokens), "CLAUDE_CODE_MAX_RETRIES=0")
}

func benchNativeCommand(ctx context.Context, a acpAgent, args []string, maxTokens int) (*exec.Cmd, func(), error) {
	dir, err := os.MkdirTemp("", "loom-bench-native-")
	if err != nil {
		return nil, nil, errors.New("benchmark temporary directory unavailable")
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	argv := append([]string{"claude"}, args...)
	if a.Remote {
		var machine *RemoteMachine
		for _, m := range loadRemoteMachines() {
			if m.ID == a.Machine {
				copy := m
				machine = &copy
				break
			}
		}
		if machine == nil || lifecycleOS(machine) == "windows" {
			cleanup()
			return nil, nil, errors.New("native benchmark machine unavailable")
		}
		key, _, e := loomSSHKey()
		if e != nil {
			cleanup()
			return nil, nil, errors.New("SSH key unavailable")
		}
		parts := make([]string, len(argv))
		for i, v := range argv {
			parts[i] = shellQuote(v)
		}
		// No PTY. stdin remains prompt data, not a remote shell program. Avoid
		// sourcing a user's shell profile or reusing an existing project folder.
		script := remotePathPreamble
		for _, tool := range machine.Tools {
			if tool.Path != "" {
				script += "PATH=" + shellQuote(filepath.ToSlash(filepath.Dir(tool.Path))) + ":\"$PATH\"\n"
			}
		}
		script += "export PATH\n" + `loom_bench_dir=$(mktemp -d) || exit 1
trap 'rm -rf "$loom_bench_dir"' EXIT
trap 'exit 130' HUP INT TERM
cd "$loom_bench_dir" || exit 1
for loom_bench_var in $(env | cut -d= -f1); do
  case "$loom_bench_var" in ANTHROPIC_*|CLAUDE_CODE_*) unset "$loom_bench_var";; esac
done
` + "export CLAUDE_CODE_MAX_OUTPUT_TOKENS=" + strconv.Itoa(maxTokens) + " CLAUDE_CODE_MAX_RETRIES=0\n" + strings.Join(parts, " ")
		argv = append([]string{"ssh"}, sshArgs(*machine, key, "sh", "-c", shellQuote(script))...)
	} else {
		argv, err = harnessNativeArgv(argv)
		if err != nil {
			cleanup()
			return nil, nil, errors.New("Claude CLI unavailable")
		}
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir, cmd.Env, cmd.Stderr, cmd.WaitDelay = dir, claudeBenchEnv(os.Environ(), maxTokens), io.Discard, time.Second
	acpProcessGroup(cmd)
	cmd.Cancel = func() error { acpKillProcessGroup(cmd); return nil }
	return cmd, cleanup, nil
}

func benchNativeRead(ctx context.Context, a acpAgent, args []string) ([]byte, error) {
	cmd, cleanup, err := benchNativeCommand(ctx, a, args, 256)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	var out usageOutput
	cmd.Stdout = &out
	if cmd.Run() != nil {
		return nil, errors.New("native benchmark preflight failed: update Claude or sign in to its account")
	}
	return out.Bytes(), nil
}

func runAccountBenchTest(ctx context.Context, t benchTest, choiceID string) (*benchResult, string, error) {
	if err := runtimeVaultError(); err != nil {
		return nil, "", err
	}
	c, a, ok := benchHarnessChoice(choiceID)
	if !ok || benchHarnessReason(c, a) != "" {
		return nil, "", errors.New("native model-only benchmark unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Minute)
	defer cancel()
	preflight, stop := context.WithTimeout(ctx, 30*time.Second)
	defer stop()
	help, err := benchNativeRead(preflight, a, []string{"--help"})
	if err != nil {
		return nil, "", err
	}
	for _, flag := range []string{"--safe-mode", "--restricted", "--setting-sources", "--tools", "--disallowedTools", "--strict-mcp-config", "--mcp-config", "--disable-slash-commands", "--no-chrome", "--no-session-persistence", "--system-prompt", "--include-partial-messages"} {
		if !strings.Contains(string(help), flag) {
			return nil, "", errors.New("this Claude version lacks model-only controls: update the CLI")
		}
	}
	status, err := benchNativeRead(preflight, a, []string{"auth", "status", "--json"})
	if err != nil {
		return nil, "", err
	}
	var account struct {
		LoggedIn   bool   `json:"loggedIn"`
		AuthMethod string `json:"authMethod"`
	}
	if json.Unmarshal(status, &account) != nil || !account.LoggedIn || account.AuthMethod != "claude.ai" {
		return nil, "", errors.New("sign in with a native Claude account for this benchmark; API providers belong in Cloud")
	}
	stop()
	prompt, maxTokens := benchPrompt(t)
	cmd, cleanup, err := benchNativeCommand(ctx, a, claudeBenchArgs(c.Model), maxTokens)
	if err != nil {
		return nil, "", err
	}
	defer cleanup()
	cmd.Stdin = strings.NewReader(prompt)
	stdout, err := cmd.StdoutPipe()
	started := time.Now()
	if err != nil || cmd.Start() != nil {
		return nil, "", errors.New("native model-only benchmark could not start")
	}
	defer func() {
		if cmd.ProcessState == nil {
			acpKillProcessGroup(cmd)
			_ = cmd.Wait()
		}
	}()
	res, output, err := consumeClaudeBench(stdout, started)
	if err != nil {
		acpKillProcessGroup(cmd)
	}
	waitErr := cmd.Wait()
	if ctx.Err() != nil {
		return nil, "", ctx.Err()
	}
	if err != nil {
		return nil, "", err
	}
	if waitErr != nil {
		return nil, "", errors.New("native model-only benchmark failed: check account access and quota")
	}
	res.Elapsed = time.Since(started).Seconds()
	res.RateBasis = "end_to_end"
	if res.CompletionTokens != nil && res.Elapsed > 0 {
		rate := float64(*res.CompletionTokens) / res.Elapsed
		res.PredictedPerSec = &rate
	}
	return res, output, nil
}

// Require the CLI's empty tool/MCP catalog before accepting output. Unexpected
// tool events are errors, never approvals or fabricated tool results. Raw
// diagnostics/account metadata are discarded; only text and reported usage persist.
func consumeClaudeBench(reader io.Reader, started time.Time) (*benchResult, string, error) {
	res := &benchResult{BenchCloudMetrics: &BenchCloudMetrics{}}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	initialized, finished := false, false
	var output strings.Builder
	total := 0
	for scanner.Scan() {
		total += len(scanner.Bytes())
		if total > 2<<20 {
			return nil, "", errors.New("native benchmark output too large")
		}
		var e struct {
			Type, Subtype string
			Model         string            `json:"model"`
			Tools         *[]string         `json:"tools"`
			MCP           []json.RawMessage `json:"mcp_servers"`
			IsError       bool              `json:"is_error"`
			Result        string            `json:"result"`
			Event         struct {
				Type         string
				ContentBlock struct {
					Type string `json:"type"`
				} `json:"content_block"`
				Delta struct{ Type, Text string } `json:"delta"`
			} `json:"event"`
			Message struct{ Content []struct{ Type, Text string } } `json:"message"`
			Usage   struct {
				Input       *int64 `json:"input_tokens"`
				Output      *int64 `json:"output_tokens"`
				CacheRead   *int64 `json:"cache_read_input_tokens"`
				CacheCreate *int64 `json:"cache_creation_input_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal(scanner.Bytes(), &e) != nil {
			return nil, "", errors.New("unrecognized native benchmark stream")
		}
		if finished {
			return nil, "", errors.New("unexpected native benchmark continuation")
		}
		switch e.Type {
		case "system":
			if e.Subtype == "init" {
				if e.Tools == nil || len(*e.Tools) != 0 || len(e.MCP) != 0 {
					return nil, "", errors.New("native CLI did not disable all tools and MCP; benchmark refused")
				}
				if initialized {
					return nil, "", errors.New("native benchmark reinitialized")
				}
				initialized = true
				res.ActualModel = e.Model
			}
		case "stream_event":
			if !initialized {
				return nil, "", errors.New("native benchmark omitted tool catalog")
			}
			if block := e.Event.ContentBlock.Type; block != "" && block != "text" && block != "thinking" && block != "redacted_thinking" {
				return nil, "", errors.New("unexpected native benchmark tool call")
			}
			if e.Event.Delta.Type == "text_delta" && e.Event.Delta.Text != "" {
				if res.TTFT == nil {
					value := time.Since(started).Seconds()
					res.TTFT = &value
				}
				output.WriteString(e.Event.Delta.Text)
			}
		case "assistant":
			if !initialized {
				return nil, "", errors.New("native benchmark omitted tool catalog")
			}
			for _, block := range e.Message.Content {
				if block.Type != "text" && block.Type != "thinking" && block.Type != "redacted_thinking" {
					return nil, "", errors.New("unexpected native benchmark tool call")
				}
			}
		case "result":
			if !initialized || e.IsError || e.Subtype != "success" {
				return nil, "", errors.New("native model-only benchmark did not succeed")
			}
			if output.Len() == 0 {
				output.WriteString(e.Result)
			} // non-streaming build: TTFT stays unknown.
			res.PromptTokens = e.Usage.Input
			res.CompletionTokens = e.Usage.Output
			if res.PromptTokens != nil {
				n := *res.PromptTokens
				if e.Usage.CacheRead != nil {
					n += *e.Usage.CacheRead
				}
				if e.Usage.CacheCreate != nil {
					n += *e.Usage.CacheCreate
				}
				res.PromptTokens = &n
			}
			finished = true
		case "user", "tool", "tool_result", "tool_use":
			return nil, "", errors.New("unexpected native benchmark tool event")
		}
		if output.Len() > 64<<10 {
			return nil, "", errors.New("benchmark response too large")
		}
	}
	if scanner.Err() != nil || !finished {
		return nil, "", errors.New("incomplete native benchmark stream")
	}
	return res, output.String(), nil
}
