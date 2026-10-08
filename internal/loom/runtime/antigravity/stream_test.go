package antigravity

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
	"testing"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/runtime"
)

type streamExchange struct {
	Frame    json.RawMessage `json:"frame"`
	Events   []string        `json:"events"`
	ExitCode int             `json:"exit_code"`
}

func streamFixture(path string) ([]streamExchange, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	rows := []streamExchange{}
	sc := bufio.NewScanner(file)
	for sc.Scan() {
		var row streamExchange
		if err := json.Unmarshal(sc.Bytes(), &row); err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, sc.Err()
}

// Fake native process: verify the documented user frame and stdin EOF, then
// replay synthetic 1.3.1 events. It never starts agy or accesses an account.
func TestAntigravityFixtureProcess(t *testing.T) {
	path := os.Getenv("LOOM_AGY_STREAM_FIXTURE")
	if path == "" {
		return
	}
	in := bufio.NewScanner(os.Stdin)
	if !in.Scan() {
		os.Exit(21)
	}
	var prompt struct {
		Event   string `json:"event"`
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(in.Bytes(), &prompt) != nil || prompt.Event != "user" || prompt.Message.Content != "fixture prompt" {
		os.Exit(22)
	}
	if in.Scan() || in.Err() != nil {
		os.Exit(23)
	}
	if path == "wait" {
		fmt.Println(`{"event":"init","conversation_id":"fixture-conversation"}`)
		select {}
	}
	if path == "malformed" {
		fmt.Println(`not json`)
		os.Exit(0)
	}
	if path == "stderr" {
		fmt.Fprintln(os.Stderr, "Conversation busy: token=secret-value")
		os.Exit(3)
	}
	rows, err := streamFixture(path)
	if err != nil {
		os.Exit(24)
	}
	code := 0
	for _, row := range rows {
		fmt.Println(string(row.Frame))
		code = row.ExitCode
	}
	os.Exit(code)
}

func fixtureCmd(path string) *exec.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run=^TestAntigravityFixtureProcess$")
	cmd.Env = append(os.Environ(), "LOOM_AGY_STREAM_FIXTURE="+path)
	return cmd
}
func TestAntigravityFixtureCorpus(t *testing.T) {
	paths, err := filepath.Glob("../../testdata/agents/antigravity/*.jsonl")
	if err != nil || len(paths) < 6 {
		t.Fatal(paths, err)
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			rows, err := streamFixture(path)
			if err != nil {
				t.Fatal(err)
			}
			want := []string{}
			for _, row := range rows {
				want = append(want, row.Events...)
			}
			events := []runtime.AgentEvent{}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			id := ""
			err = RunStream(ctx, fixtureCmd(path), "fixture prompt", "", "turn-1", func(e runtime.AgentEvent) bool { events = append(events, e); return true }, func(value string) { id = value })
			got := []string{}
			for _, e := range events {
				got = append(got, e.Type)
				if e.ThreadID != "fixture-conversation" || e.TurnID != "turn-1" || !json.Valid(e.Raw) {
					t.Fatalf("invalid envelope: %+v", e)
				}
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("events %v != %v; err %v", got, want, err)
			}
			if id != "fixture-conversation" {
				t.Fatal("lost native conversation ID", id)
			}
			last := events[len(events)-1]
			if (err == nil) != (last.Status == "completed") {
				t.Fatalf("outcome %s: %v", last.Status, err)
			}
			switch filepath.Base(path) {
			case "normal.jsonl":
				u := events[len(events)-2].Usage
				if u == nil || *u.Total != 18 || *u.Cached != 4 || *u.Reasoning != 2 {
					t.Fatalf("step spend duplicated or cumulative result counted: %+v", u)
				}
			case "error.jsonl", "busy.jsonl":
				var result struct {
					Result struct {
						Error string `json:"error"`
					} `json:"result"`
				}
				_ = json.Unmarshal(rows[len(rows)-1].Frame, &result)
				if err.Error() != result.Result.Error {
					t.Fatalf("failure changed: %v", err)
				}
			case "tool-file.jsonl":
				for _, e := range events {
					if e.ItemType == "file_change" {
						var payload map[string]any
						_ = json.Unmarshal(e.Payload, &payload)
						if payload["path"] != "note.txt" || payload["diff"] != nil || payload["changes"] != nil {
							t.Fatal("invented file diff", payload)
						}
					}
				}
			case "permission-question.jsonl":
				for _, e := range events {
					if e.Request != nil {
						t.Fatal("invented interactive request")
					}
				}
			case "unknown.jsonl":
				found := false
				for _, e := range events {
					if e.Method == "step_update/future_reasoning" {
						found = strings.Contains(string(e.Payload), `"new_field":"preserve"`)
					}
				}
				if !found {
					t.Fatal("unknown fields lost from raw display row")
				}
			}
		})
	}
}

func TestAntigravityCancellationAndProtocolFailures(t *testing.T) {
	for _, scenario := range []string{"wait", "malformed", "stderr"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			events := []runtime.AgentEvent{}
			err := RunStream(ctx, fixtureCmd(scenario), "fixture prompt", "", "turn-1", func(e runtime.AgentEvent) bool {
				events = append(events, e)
				if scenario == "wait" && e.Type == "turn.started" {
					cancel()
				}
				return true
			}, func(string) {})
			if err == nil || len(events) == 0 || events[len(events)-1].Type != "turn.completed" {
				t.Fatal(events, err)
			}
			if scenario == "wait" && (!errors.Is(err, context.Canceled) || events[len(events)-1].Status != "cancelled") {
				t.Fatal(events, err)
			}
			if scenario == "stderr" && (!strings.Contains(err.Error(), "Conversation busy") || strings.Contains(err.Error(), "secret-value")) {
				t.Fatal("missing/redaction failure", err)
			}
		})
	}
}

func TestAntigravityResumeIdentityAndUnknownCounters(t *testing.T) {
	m := Mapper{ThreadID: "selected", TurnID: "turn-1"}
	if _, err := m.Feed([]byte(`{"event":"init","conversation_id":"different"}`)); err == nil {
		t.Fatal("resume silently created a new conversation")
	}
	m = Mapper{}
	_, err := m.Feed([]byte(`{"event":"step_update","step_update":{"step_index":1,"step_type":"checkpoint","usage":{"input_tokens":2,"output_tokens":-1}}}`))
	if err != nil {
		t.Fatal(err)
	}
	u := m.turnUsage()
	if u.Input == nil || *u.Input != 2 || u.Output != nil || u.Total != nil {
		t.Fatal("unknown counters became zero", u)
	}
}

func TestAntigravityCancelledResultKeepsNativeError(t *testing.T) {
	m := Mapper{}
	if _, err := m.Feed([]byte(`{"event":"result","result":{"conversation_id":"fixture-conversation","status":"CANCELED","error":"Native cancellation detail"}}`)); err != nil {
		t.Fatal(err)
	}
	events, err := m.Finish(nil)
	last := events[len(events)-1]
	if !errors.Is(err, context.Canceled) || last.Status != "cancelled" || last.Error != "Native cancellation detail" {
		t.Fatal(events, err)
	}
	m = Mapper{}
	_, _ = m.Feed([]byte(`{"event":"result","result":{"status":"SUCCESS","response":"Hello"}}`))
	if _, err := m.Finish(nil); err == nil {
		t.Fatal("missing conversation ID silently accepted")
	}
}

func TestAntigravityLaunchPolicy(t *testing.T) {
	for _, c := range []TurnConfig{{}, {Mode: "default", Permission: "ask"}, {Mode: "accept-edits"}, {Permission: "edits"}, {Mode: "plan", Permission: "full"}, {Mode: "full"}, {Permission: "full"}, {ConversationID: "native-id", Model: "native-model", Effort: "xhigh", Sandbox: true, AdditionalDirs: []string{"/fixture/extra"}}} {
		args, err := c.Args()
		if err != nil {
			t.Fatal(err)
		}
		joined := strings.Join(args, " ")
		bypass := c.Mode == "full" || c.Permission == "full" && c.Mode != "plan"
		if strings.Contains(joined, "--dangerously-skip-permissions") != bypass {
			t.Fatal("permission changed", c, args)
		}
		if strings.Contains(joined, "--continue") || strings.Contains(joined, "--print ") {
			t.Fatal("implicit resume or dropped prompt", args)
		}
		if c.ConversationID != "" && (!strings.Contains(joined, "--conversation native-id") || !strings.Contains(joined, "--effort xhigh") || !strings.Contains(joined, "--sandbox") || !strings.Contains(joined, "--add-dir /fixture/extra")) {
			t.Fatal(args)
		}
	}
	for _, c := range []TurnConfig{{Effort: "invented"}, {Mode: "yolo"}, {Permission: "never"}, {Model: "bad model"}, {ConversationID: "bad/id"}} {
		if _, err := c.Args(); err == nil {
			t.Fatal("accepted invalid configuration", c)
		}
	}
}
