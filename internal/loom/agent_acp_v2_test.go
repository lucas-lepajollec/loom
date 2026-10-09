package loom

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	agent "github.com/lucas-lepajollec/loom/internal/loom/runtime"
	"github.com/lucas-lepajollec/loom/internal/loom/runtime/acp"
)

func TestClaudeACPFixtures(t *testing.T) {
	testHome(t)
	paths, err := filepath.Glob("testdata/agents/claude/*.jsonl")
	if err != nil || len(paths) == 0 {
		t.Fatal(paths, err)
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			file, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			scan := bufio.NewScanner(file)
			for scan.Scan() {
				var row struct {
					Frame       acpFrame            `json:"frame"`
					Kind        string              `json:"kind"`
					Questions   int                 `json:"questions"`
					MultiSelect bool                `json:"multi_select"`
					Answer      agent.RequestAnswer `json:"answer"`
					Response    any                 `json:"response"`
					Error       string              `json:"error"`
				}
				if err := json.Unmarshal(scan.Bytes(), &row); err != nil {
					t.Fatal(err)
				}
				var fields map[string]json.RawMessage
				_ = json.Unmarshal(scan.Bytes(), &fields)
				row.Frame.Raw = fields["frame"]
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				p := &acpBinding{agentID: "claude-code", ctx: ctx, active: true, state: ACPState{NativeSessionID: "fixture-session", Permission: "ask"}, tools: map[string]map[string]any{}, approvals: map[string]*acpApproval{}, manager: newRuntimeSessions(), client: &acpClient{done: make(chan struct{})}}
				events := []AgentEvent{}
				p.emit = func(e StreamEvent) bool {
					if e.AgentEvent != nil {
						events = append(events, *e.AgentEvent)
					}
					return true
				}
				p.broker = agent.NewRequestBroker("claude-code", func(e AgentEvent) bool {
					events = append(events, e)
					if e.Request != nil {
						if e.Request.Kind != row.Kind {
							t.Errorf("kind=%s want=%s", e.Request.Kind, row.Kind)
						}
						if len(e.Request.Questions) != row.Questions {
							t.Errorf("questions=%v", e.Request.Questions)
						}
						for _, q := range e.Request.Questions {
							if q.MultiSelect != row.MultiSelect || !q.FreeText || !q.Optional {
								t.Errorf("question=%+v", q)
							}
						}
						go func() {
							if err := p.broker.Resolve(e.Request.ID, row.Answer); err != nil {
								t.Error(err)
							}
						}()
					}
					return true
				})
				defer p.broker.Cancel()
				if row.Error != "" {
					if row.Frame.Error != nil {
						row.Frame.Error.Raw = row.Frame.Raw
						p.publishClaudeRPCFailure(acpAgent{ID: "claude-code"}, row.Frame.Error)
						if len(events) != 1 || events[0].Error != row.Error {
							t.Fatal(events)
						}
						if err := claudeACPError(acpAgent{ID: "claude-code"}, row.Frame.Error, "fallback"); err.Error() != row.Error {
							t.Fatal(err)
						}
						continue
					}
					var meta json.RawMessage
					if row.Frame.Method == "session/update" {
						p.handleNotification(row.Frame)
						if p.failure != row.Error {
							t.Fatalf("failure=%q want=%q", p.failure, row.Error)
						}
						if len(events) != 1 || events[0].Type != "error" || events[0].Error != row.Error {
							t.Fatal(events)
						}
					} else {
						var result struct {
							Meta json.RawMessage `json:"_meta"`
						}
						_ = json.Unmarshal(row.Frame.Result, &result)
						meta = result.Meta
						if msg, _ := acp.FailureMeta(meta); msg != row.Error {
							t.Fatalf("failure=%q", msg)
						}
					}
					continue
				}
				var result any
				if row.Kind == "approval" {
					p.state.Permission = "full" // A plan transition still needs an explicit choice.
					p.emit = func(e StreamEvent) bool {
						if e.AgentEvent != nil {
							events = append(events, *e.AgentEvent)
							if e.AgentEvent.Request != nil {
								go func() {
									p.mu.Lock()
									pending := p.approvals[e.AgentEvent.Request.ID]
									p.mu.Unlock()
									pending.answer <- acpDecision{option: row.Answer.Decision}
								}()
							}
						}
						return true
					}
					var params struct {
						Tool    map[string]any   `json:"toolCall"`
						Options []map[string]any `json:"options"`
					}
					_ = json.Unmarshal(row.Frame.Params, &params)
					result, err = p.permission(ctx, params.Tool, params.Options, &row.Frame)
				} else {
					result, err = p.elicitation(ctx, &row.Frame)
				}
				if err != nil {
					t.Fatal(err)
				}
				got, _ := json.Marshal(result)
				want, _ := json.Marshal(row.Response)
				var gv, wv any
				_ = json.Unmarshal(got, &gv)
				_ = json.Unmarshal(want, &wv)
				if !reflect.DeepEqual(gv, wv) {
					t.Fatalf("response=%s want=%s", got, want)
				}
				if len(events) != 2 || events[0].Type != "request.opened" || events[1].Type != "request.resolved" {
					t.Fatal(events)
				}
			}
			if err = scan.Err(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestACPCompatibilityPinsAndWarnings(t *testing.T) {
	for _, id := range []string{"claude-code", "opencode", "hermes", "openclaw", "antigravity"} {
		t.Run(id, func(t *testing.T) {
			a := acpAgent{ID: id, Name: id, Command: id}
			for _, v := range builtinACPAgents() {
				if v.ID == id {
					a = v
				}
			}
			r := acpCompatibility(a, map[string]any{"version": "unexpected-fixture-version"}, nil)
			if r.AgentVersion != "unexpected-fixture-version" || r.AdapterPackage == "" || r.Warning == "" {
				t.Fatal(r)
			}
			if id == "claude-code" && r.AdapterVersion != "0.88.0" {
				t.Fatal(r)
			}
			r = acpCompatibility(a, nil, nil)
			if r.AgentVersion != "" || (id != "hermes" && id != "openclaw" && r.Warning != "") {
				t.Fatal(r)
			}
		})
	}
}

// A complete bidirectional ACP turn verifies negotiation and native completion,
// without invoking an installed agent or any paid provider.
func TestStep2ACPFixtureProcess(t *testing.T) {
	name := os.Getenv("LOOM_STEP2_ACP_FIXTURE")
	if name == "" {
		return
	}
	scan := bufio.NewScanner(os.Stdin)
	send := func(v any) { _ = json.NewEncoder(os.Stdout).Encode(v) }
	for scan.Scan() {
		var f acpFrame
		_ = json.Unmarshal(scan.Bytes(), &f)
		switch f.Method {
		case "initialize":
			var p struct {
				Capabilities map[string]any `json:"clientCapabilities"`
			}
			_ = json.Unmarshal(f.Params, &p)
			form := p.Capabilities["elicitation"].(map[string]any)["form"]
			if _, ok := form.(map[string]any); !ok {
				os.Exit(4)
			}
			meta := p.Capabilities["_meta"].(map[string]any)
			if meta["terminal_output"] != true || p.Capabilities["terminal"] != false {
				os.Exit(5)
			}
			send(map[string]any{"jsonrpc": "2.0", "id": f.ID, "result": map[string]any{"protocolVersion": 1, "agentInfo": map[string]any{"name": name, "version": "fixture"}, "agentCapabilities": map[string]any{}}})
		case "session/new", "session/resume":
			if name == "deepseek-harness" {
				var params map[string]any
				_ = json.Unmarshal(f.Params, &params)
				if _, present := params["additionalDirectories"]; present {
					os.Exit(10)
				}
				if os.Getenv("LOOM_STEP2_ACP_CASE") == "resume" && (f.Method != "session/resume" || params["sessionId"] != "fixture-session") {
					os.Exit(11)
				}
			}
			result := map[string]any{"sessionId": "fixture-session"}
			if name == "deepseek-harness" {
				result["configOptions"] = []any{map[string]any{"id": "model", "name": "Model", "category": "model", "type": "select", "currentValue": "deepseek-v4-pro", "options": []any{map[string]any{"value": "deepseek-v4-pro", "name": "DeepSeek V4 Pro"}, map[string]any{"value": "deepseek-v4-flash", "name": "DeepSeek V4 Flash"}}}}
			}
			send(map[string]any{"jsonrpc": "2.0", "id": f.ID, "result": result})
		case "session/set_config_option":
			var params map[string]any
			_ = json.Unmarshal(f.Params, &params)
			if name != "deepseek-harness" || params["configId"] != "model" || params["value"] != "deepseek-v4-flash" {
				os.Exit(9)
			}
			send(map[string]any{"jsonrpc": "2.0", "id": f.ID, "result": map[string]any{"configOptions": []any{map[string]any{"id": "model", "name": "Model", "category": "model", "type": "select", "currentValue": "deepseek-v4-flash", "options": []any{map[string]any{"value": "deepseek-v4-flash", "name": "DeepSeek V4 Flash"}}}}}})
		case "session/prompt":
			fixtureCase := os.Getenv("LOOM_STEP2_ACP_CASE")
			if fixtureCase == "" {
				fixtureCase = "normal"
			}
			path := filepath.Join(os.Getenv("LOOM_STEP2_ACP_FIXTURE_ROOT"), name, fixtureCase+".jsonl")
			data, err := os.ReadFile(path)
			if err != nil {
				os.Exit(6)
			}
			rows := bufio.NewScanner(bytes.NewReader(data))
			for rows.Scan() {
				var row struct {
					Frame    map[string]any `json:"frame"`
					Response map[string]any `json:"response"`
				}
				_ = json.Unmarshal(rows.Bytes(), &row)
				if row.Frame["method"] == nil {
					row.Frame["id"] = f.ID
				}
				send(row.Frame)
				if row.Response != nil {
					if !scan.Scan() {
						os.Exit(7)
					}
					var answer struct {
						Result map[string]any `json:"result"`
					}
					_ = json.Unmarshal(scan.Bytes(), &answer)
					if !reflect.DeepEqual(row.Response, answer.Result) {
						os.Exit(8)
					}
				}
			}
			if name == "claude" && !strings.HasPrefix(fixtureCase, "failure-") {
				send(map[string]any{"jsonrpc": "2.0", "id": f.ID, "result": map[string]any{"stopReason": "end_turn"}})
			}
		default:
			send(map[string]any{"jsonrpc": "2.0", "id": f.ID, "result": map[string]any{}})
		}
	}
	os.Exit(0)
}
func TestStep2ACPFullFixtureTurns(t *testing.T) {
	for _, id := range []string{"opencode-acp"} {
		t.Run(id, func(t *testing.T) {
			testHome(t)
			t.Setenv("LOOM_STEP2_ACP_FIXTURE", id)
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			// Fixture process reads the corpus by absolute testdata root, since the agent's
			// cwd is deliberately an isolated project directory.
			t.Setenv("LOOM_STEP2_ACP_FIXTURE_ROOT", filepath.Join(mustWorkingDir(t), "testdata/agents"))
			m := newRuntimeSessions()
			defer m.shutdownACP()
			a := acpAgent{ID: id, Name: id, Command: exe, Args: []string{"-test.run=^TestStep2ACPFixtureProcess$"}, Custom: true}
			s := RuntimeSession{ID: "fixture-discussion", RuntimeID: id, Model: "default", ACPState: ACPState{Workdir: dir}}
			events := []AgentEvent{}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			result, err := m.runACP(ctx, a, s, RuntimeTurn{Messages: []Message{{Role: "user", Content: "Fixture prompt"}}}, func(e StreamEvent) bool {
				if e.AgentEvent != nil {
					events = append(events, *e.AgentEvent)
				}
				return true
			})
			if err != nil || len(result) != 1 {
				t.Fatal(result, err)
			}
			want := "Hello from Gemini."
			if id == "opencode-acp" {
				want = "Hello from OpenCode ACP."
			}
			if result[0].Content != want {
				t.Fatal(result)
			}
			types := []string{}
			for _, e := range events {
				if e.Type != "warning" {
					types = append(types, e.Type)
				}
			}
			if !reflect.DeepEqual(types, []string{"turn.started", "content.delta", "turn.completed"}) {
				t.Fatal(types)
			}
		})
	}
}
func mustWorkingDir(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestACPCompatibilityRetainsActualLauncherPin(t *testing.T) {
	a := acpAgent{ID: "claude-code", Name: "Claude", Command: "npx", Args: []string{"-y", "@agentclientprotocol/claude-agent-acp@0.84.0"}}
	r := acpCompatibility(a, map[string]any{"version": "0.84.0"}, nil)
	if r.AdapterVersion != "0.84.0" || r.Warning == "" {
		t.Fatal(r)
	}
}

func TestClaudeACPFullRequestAndFailureTurns(t *testing.T) {
	for _, name := range []string{"ask-multiple", "ask-multiselect", "plan-approval", "failure-overloaded", "failure-rate-limit", "failure-auth-rpc"} {
		t.Run(name, func(t *testing.T) {
			testHome(t)
			t.Setenv("LOOM_STEP2_ACP_FIXTURE", "claude")
			t.Setenv("LOOM_STEP2_ACP_CASE", name)
			t.Setenv("LOOM_STEP2_ACP_FIXTURE_ROOT", filepath.Join(mustWorkingDir(t), "testdata/agents"))
			exe, _ := os.Executable()
			dir := t.TempDir()
			m := newRuntimeSessions()
			defer m.shutdownACP()
			a := acpAgent{ID: "claude-code", Name: "Claude fixture", Command: exe, Args: []string{"-test.run=^TestStep2ACPFixtureProcess$"}, Custom: true}
			s := RuntimeSession{ID: "fixture", RuntimeID: a.ID, Model: "default", ACPState: ACPState{Workdir: dir, Permission: "ask"}}
			if err := putStoreJSON(bkRuntimeSessions, s.ID, s); err != nil {
				t.Fatal(err)
			}
			data, _ := os.ReadFile(filepath.Join("testdata/agents/claude", name+".jsonl"))
			first := bytes.Split(data, []byte("\n"))[0]
			var row struct {
				Answer agent.RequestAnswer `json:"answer"`
				Error  string              `json:"error"`
			}
			_ = json.Unmarshal(first, &row)
			var mu sync.Mutex
			events := []AgentEvent{}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_, err := m.runACP(ctx, a, s, RuntimeTurn{Messages: []Message{{Role: "user", Content: "Fixture prompt"}}}, func(e StreamEvent) bool {
				if e.AgentEvent != nil {
					mu.Lock()
					events = append(events, *e.AgentEvent)
					mu.Unlock()
					if r := e.AgentEvent.Request; r != nil {
						if r.Kind == "approval" {
							go func() {
								if err := m.answerACP(s.ID, r.ID, row.Answer.Decision, false); err != nil {
									t.Error(err)
								}
							}()
						} else {
							go func() {
								m.acpMu.Lock()
								b := m.requests[s.ID]
								m.acpMu.Unlock()
								if err := b.Resolve(r.ID, row.Answer); err != nil {
									t.Error(err)
								}
							}()
						}
					}
				}
				return true
			})
			if row.Error != "" {
				if err == nil || err.Error() != row.Error {
					t.Fatal(err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			defer mu.Unlock()
			if len(events) == 0 || events[len(events)-1].Type != "turn.completed" {
				t.Fatal(events)
			}
			end := events[len(events)-1]
			if row.Error != "" && end.Status != "failed" {
				t.Fatal(end)
			}
			if row.Error == "" {
				opened, resolved := 0, 0
				for _, e := range events {
					if e.Type == "request.opened" {
						opened++
					}
					if e.Type == "request.resolved" {
						resolved++
					}
				}
				if opened != 1 || resolved != 1 {
					t.Fatal(events)
				}
			}
		})
	}
}
