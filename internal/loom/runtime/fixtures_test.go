package runtime_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/runtime"
	"github.com/lucas-lepajollec/loom/internal/loom/runtime/agentstdio"
	"github.com/lucas-lepajollec/loom/internal/loom/runtime/codexapp"
	"github.com/lucas-lepajollec/loom/internal/loom/runtime/pirpc"
)

type exchange struct {
	Command  string                 `json:"command,omitempty"`
	Frame    json.RawMessage        `json:"frame"`
	Events   []string               `json:"events"`
	Answer   *runtime.RequestAnswer `json:"answer"`
	Response map[string]any         `json:"response"`
	Error    string                 `json:"error,omitempty"`
}

func readFixture(path string) ([]exchange, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	rows := []exchange{}
	s := bufio.NewScanner(file)
	s.Buffer(make([]byte, 4096), agentstdio.MaxFrame)
	for s.Scan() {
		var r exchange
		if err := json.Unmarshal(s.Bytes(), &r); err != nil {
			return nil, err
		}
		rows = append(rows, r)
	}
	return rows, s.Err()
}
func TestAgentFixtureProcess(t *testing.T) {
	path := os.Getenv("LOOM_AGENT_FIXTURE")
	if path == "" {
		return
	}
	rows, err := readFixture(path)
	if err != nil {
		panic(err)
	}
	in := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	pi := strings.Contains(path, string(filepath.Separator)+"pi"+string(filepath.Separator))
	reply := func(req map[string]any, data any) {
		if pi {
			_ = encoder.Encode(map[string]any{"type": "response", "command": req["type"], "id": req["id"], "success": true, "data": data})
		} else {
			_ = encoder.Encode(map[string]any{"id": req["id"], "result": data})
		}
	}
	for in.Scan() {
		var req map[string]any
		if json.Unmarshal(in.Bytes(), &req) != nil {
			os.Exit(2)
		}
		method, _ := req["method"].(string)
		if pi {
			method, _ = req["type"].(string)
		}
		switch method {
		case "initialized":
			continue
		case "initialize":
			reply(req, map[string]any{"userAgent": "fixture"})
		case "thread/start", "thread/resume":
			reply(req, map[string]any{"thread": map[string]any{"id": "fixture-thread"}})
		case "get_state":
			reply(req, map[string]any{"sessionId": "fixture-session", "sessionFile": "/fixture/session.jsonl", "isStreaming": false})
		case "switch_session", "set_model", "set_thinking_level", "abort", "turn/interrupt":
			reply(req, map[string]any{})
		case "get_session_stats":
			data := map[string]any{}
			for _, r := range rows {
				if r.Command == method {
					var frame agentstdio.Frame
					_ = json.Unmarshal(r.Frame, &frame)
					_ = json.Unmarshal(frame.Data, &data)
				}
			}
			reply(req, data)
		case "turn/start", "prompt":
			if method == "turn/start" {
				params, _ := req["params"].(map[string]any)
				for _, field := range []string{"parentTurnId", "rootTurnId"} {
					if _, exists := params[field]; exists {
						fmt.Fprintf(os.Stderr, "user turn sent %s", field)
						os.Exit(7)
					}
				}
			}
			data := map[string]any{"turn": map[string]any{"id": "turn-1"}}
			if pi {
				data = map[string]any{}
			}
			for _, r := range rows {
				if r.Command == method {
					var frame agentstdio.Frame
					_ = json.Unmarshal(r.Frame, &frame)
					_ = json.Unmarshal(frame.Data, &data)
				}
			}
			reply(req, data)
			for _, r := range rows {
				if r.Command != "" {
					continue
				}
				var frame map[string]any
				_ = json.Unmarshal(r.Frame, &frame)
				_ = encoder.Encode(frame)
				if r.Response != nil {
					if !in.Scan() {
						os.Exit(3)
					}
					var response map[string]any
					if json.Unmarshal(in.Bytes(), &response) != nil {
						os.Exit(4)
					}
					for k, want := range r.Response {
						if !reflect.DeepEqual(response[k], want) {
							fmt.Fprintf(os.Stderr, "reply %s mismatch: %v != %v", k, response[k], want)
							os.Exit(5)
						}
					}
					if !reflect.DeepEqual(response["id"], frame["id"]) {
						os.Exit(6)
					}
				}
			}
			// Stay alive until Loom closes stdin; the final event must be consumed.
		default:
			reply(req, map[string]any{})
		}
	}
	os.Exit(0)
}
func TestAgentFixtureCorpus(t *testing.T) {
	// This stdio driver speaks Codex app-server and Pi RPC. ACP/HTTP corpora
	// have their own transport replay tests; never run them as Codex frames.
	paths := []string{}
	for _, name := range []string{"codex", "pi"} {
		rows, err := filepath.Glob("../testdata/agents/" + name + "/*.jsonl")
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, rows...)
	}
	if len(paths) < 16 {
		t.Fatalf("corpus: %d", len(paths))
	}
	for _, path := range paths {
		t.Run(strings.TrimPrefix(path, "../testdata/agents/"), func(t *testing.T) {
			rows, err := readFixture(path)
			if err != nil {
				t.Fatal(err)
			}
			pi := strings.Contains(path, "/pi/")
			name := "codex"
			if pi {
				name = "pi"
			}
			expected := []string{}
			answers := map[string]runtime.RequestAnswer{}
			for _, r := range rows {
				expected = append(expected, r.Events...)
				if r.Answer != nil {
					var f agentstdio.Frame
					_ = json.Unmarshal(r.Frame, &f)
					id := name + ":" + string(f.ID)
					if pi {
						var s string
						_ = json.Unmarshal(f.ID, &s)
						id = name + ":" + s
					}
					answers[id] = *r.Answer
				}
			}
			cmd := exec.Command(os.Args[0], "-test.run=^TestAgentFixtureProcess$")
			cmd.Env = append(os.Environ(), "LOOM_AGENT_FIXTURE="+path)
			c, err := agentstdio.New(cmd)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var mu sync.Mutex
			actual := []string{}
			opened := make(chan runtime.AgentRequest, 32)
			emit := func(e runtime.AgentEvent) bool {
				mu.Lock()
				actual = append(actual, e.Type)
				mu.Unlock()
				if !json.Valid(e.Raw) {
					t.Errorf("invalid raw payload: %s", e.Type)
				}
				if e.Request != nil {
					opened <- *e.Request
				}
				return true
			}
			b := runtime.NewRequestBroker(name, emit)
			defer b.Cancel()
			go func() {
				for {
					select {
					case r := <-opened:
						if err := b.Resolve(r.ID, answers[r.ID]); err != nil {
							t.Errorf("resolve: %v", err)
						}
					case <-ctx.Done():
						return
					}
				}
			}()
			if pi {
				s := pirpc.New(c, b, emit)
				err = c.Start()
				if err == nil {
					err = s.Turn(ctx, "/fixture/session.jsonl", "fixture", "fixture-model", "medium", "fixture prompt", func(st pirpc.State) {
						if st.SessionID != "fixture-session" || st.SessionFile != "/fixture/session.jsonl" {
							t.Error(st)
						}
					})
				}
			} else {
				s := codexapp.New(c, b, emit)
				err = c.Start()
				if err == nil {
					err = s.Initialize(ctx)
				}
				if err == nil {
					err = s.Turn(ctx, codexapp.TurnConfig{ThreadID: "fixture-thread", Model: "fixture-model", Effort: "medium", Approval: "on-request", Sandbox: "workspace-write"}, "fixture prompt", func(id string) {
						if id != "fixture-thread" {
							t.Error(id)
						}
					})
				}
			}
			interrupted := strings.Contains(path, "/interrupt.jsonl") || strings.Contains(path, "/settled-aborted.jsonl")
			failure := ""
			if strings.Contains(path, "/error.jsonl") {
				failure = "fixture provider failure"
			}
			for _, r := range rows {
				if r.Error != "" {
					failure = r.Error
				}
			}
			if interrupted {
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("interrupt: %v", err)
				}
			} else if failure != "" {
				if err == nil || err.Error() != failure {
					t.Fatalf("failure: %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			defer mu.Unlock()
			if !reflect.DeepEqual(actual, expected) {
				t.Fatalf("sequence\ngot  %v\nwant %v", actual, expected)
			}
		})
	}
}
