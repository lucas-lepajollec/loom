package runtime_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
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

func TestAgentLifecycleProcess(t *testing.T) {
	mode := os.Getenv("LOOM_AGENT_LIFECYCLE")
	if mode == "" {
		return
	}
	pi := strings.HasPrefix(mode, "pi")
	out := json.NewEncoder(os.Stdout)
	in := bufio.NewScanner(os.Stdin)
	for in.Scan() {
		var req map[string]any
		_ = json.Unmarshal(in.Bytes(), &req)
		method, _ := req["method"].(string)
		if pi {
			method, _ = req["type"].(string)
		}
		if method == "initialized" || method == "extension_ui_response" || method == "" {
			continue
		}
		if strings.HasSuffix(mode, "malformed") {
			_, _ = os.Stdout.WriteString("{invalid}\n")
			continue
		}
		if strings.HasSuffix(mode, "exit") {
			os.Exit(1)
		}
		if strings.HasSuffix(mode, "failure") {
			if pi {
				_ = out.Encode(map[string]any{"id": req["id"], "type": "response", "command": method, "success": false, "error": "verbatim command failure"})
			} else {
				_ = out.Encode(map[string]any{"id": req["id"], "error": map[string]any{"code": -32000, "message": "verbatim command failure"}})
			}
			continue
		}
		if method == "prompt" || method == "turn/start" {
			// Deliver a request while withholding the command acknowledgement.
			if pi {
				_ = out.Encode(map[string]any{"type": "agent_start"})
				_ = out.Encode(map[string]any{"type": "extension_ui_request", "id": "pending", "method": "input", "title": "Question"})
			} else {
				_ = out.Encode(map[string]any{"method": "turn/started", "params": map[string]any{"threadId": "native-thread", "turn": map[string]any{"id": "native-turn", "status": "inProgress"}}})
				_ = out.Encode(map[string]any{"id": 17, "method": "item/tool/requestUserInput", "params": map[string]any{"threadId": "native-thread", "turnId": "native-turn", "itemId": "question", "questions": []any{map[string]any{"id": "answer", "header": "Question", "question": "Question", "isOther": true, "isSecret": false, "options": []any{}}}}})
			}
			continue
		}
		data := map[string]any{}
		switch method {
		case "thread/start":
			data["thread"] = map[string]any{"id": "native-thread"}
		case "get_state":
			data["sessionId"] = "native-session"
			data["sessionFile"] = "/fixture/session.jsonl"
		case "abort", "turn/interrupt":
			if method == "turn/interrupt" {
				params, _ := req["params"].(map[string]any)
				if params["threadId"] != "native-thread" || params["turnId"] != "native-turn" {
					os.Exit(2)
				}
			}
			_ = os.WriteFile(os.Getenv("LOOM_AGENT_STOP_MARKER"), []byte(method), 0600)
		}
		if pi {
			_ = out.Encode(map[string]any{"id": req["id"], "type": "response", "command": method, "success": true, "data": data})
		} else {
			_ = out.Encode(map[string]any{"id": req["id"], "result": data})
		}
	}
	os.Exit(0)
}

func lifecycleClient(t *testing.T, mode, marker string) *agentstdio.Client {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestAgentLifecycleProcess$")
	cmd.Env = append(os.Environ(), "LOOM_AGENT_LIFECYCLE="+mode, "LOOM_AGENT_STOP_MARKER="+marker)
	c, err := agentstdio.New(cmd)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}

func TestAgentProtocolFailures(t *testing.T) {
	for _, name := range []string{"codex", "pi"} {
		for _, mode := range []string{"failure", "malformed", "exit"} {
			t.Run(name+"/"+mode, func(t *testing.T) {
				c := lifecycleClient(t, name+"-"+mode, "")
				if err := c.Start(); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				err := c.Call(ctx, "initialize", nil, name == "pi", nil)
				if err == nil || errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("failure not surfaced promptly: %v", err)
				}
				if mode == "failure" && err.Error() != "verbatim command failure" {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestAgentStopDuringAcknowledgement(t *testing.T) {
	for _, name := range []string{"codex", "pi"} {
		t.Run(name, func(t *testing.T) {
			marker := t.TempDir() + "/stop"
			c := lifecycleClient(t, name+"-cancel", marker)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			var mu sync.Mutex
			events := []string{}
			emit := func(e runtime.AgentEvent) bool {
				if strings.HasPrefix(e.Type, "request.") {
					mu.Lock()
					events = append(events, e.Type)
					mu.Unlock()
				}
				if e.Type == "request.opened" {
					cancel()
				}
				return true
			}
			b := runtime.NewRequestBroker(name, emit)
			defer b.Cancel()
			var err error
			if name == "pi" {
				s := pirpc.New(c, b, emit)
				err = c.Start()
				if err == nil {
					err = s.Turn(ctx, "", "", "", "", "fixture", func(pirpc.State) {})
				}
			} else {
				s := codexapp.New(c, b, emit)
				err = c.Start()
				if err == nil {
					err = s.Initialize(ctx)
				}
				if err == nil {
					err = s.Turn(ctx, codexapp.TurnConfig{}, "fixture", func(string) {})
				}
			}
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("stop: %v", err)
			}
			stopped, err := os.ReadFile(marker)
			want := "turn/interrupt"
			if name == "pi" {
				want = "abort"
			}
			if err != nil || string(stopped) != want {
				t.Fatalf("stop command: %q %v", stopped, err)
			}
			mu.Lock()
			defer mu.Unlock()
			if !reflect.DeepEqual(events, []string{"request.opened", "request.resolved"}) {
				t.Fatalf("request lifecycle: %v", events)
			}
		})
	}
}
