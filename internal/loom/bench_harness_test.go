package loom

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

const claudeBenchFixture = `{"type":"system","subtype":"init","tools":[],"mcp_servers":[],"model":"fixture-model"}
{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"Fixture answer"}}}
{"type":"assistant","message":{"content":[{"type":"text","text":"Fixture answer"}]}}
{"type":"result","subtype":"success","result":"Fixture answer","usage":{"input_tokens":5,"output_tokens":8,"cache_read_input_tokens":2,"cache_creation_input_tokens":1}}
`

func TestBenchNativeHelper(t *testing.T) {
	mode := os.Getenv("LOOM_TEST_BENCH_NATIVE")
	if mode == "" {
		return
	}
	args := os.Args[slices.Index(os.Args, "--")+1:]
	if len(args) == 1 && args[0] == "--help" {
		fmt.Println(strings.Join(claudeBenchArgs("fixture-model"), " "))
		os.Exit(0)
	}
	if slices.Equal(args, []string{"auth", "status", "--json"}) {
		if mode == "api" {
			fmt.Println(`{"loggedIn":true,"authMethod":"api_key","email":"PRIVATE-FIXTURE"}`)
		} else {
			fmt.Println(`{"loggedIn":true,"authMethod":"claude.ai","email":"PRIVATE-FIXTURE"}`)
		}
		os.Exit(0)
	}
	if !slices.Equal(args, claudeBenchArgs("fixture-model")) || os.Getenv("ANTHROPIC_API_KEY") != "" || os.Getenv("CLAUDE_CODE_MAX_OUTPUT_TOKENS") != "41" {
		os.Exit(31)
	}
	entries, _ := os.ReadDir(".")
	if len(entries) != 0 {
		os.Exit(32)
	}
	prompt, _ := io.ReadAll(os.Stdin)
	if string(prompt) != "Only this prompt" {
		os.Exit(33)
	}
	if mode == "wait" {
		fmt.Print(strings.Split(claudeBenchFixture, "\n")[0] + "\n")
		time.Sleep(time.Minute)
		os.Exit(34)
	}
	fmt.Print(claudeBenchFixture)
	os.Exit(0)
}

func benchNativeFixture(t *testing.T, mode string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX helper wrapper; native Windows acceptance is separate")
	}
	benchTestWorkspace(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, "native-config"))
	dir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "claude")
	script := "#!/bin/sh\nexec " + shellQuote(os.Args[0]) + " -test.run='^TestBenchNativeHelper$' -- \"$@\"\n"
	if os.WriteFile(path, []byte(script), 0700) != nil {
		t.Fatal("fixture write")
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("LOOM_TEST_BENCH_NATIVE", mode)
	t.Setenv("GORACE", "atexit_sleep_ms=0")
	t.Setenv("ANTHROPIC_API_KEY", "PRIVATE-FIXTURE")
	a := &acpAdapter{agent: acpAgent{ID: "claude-code", Name: "Claude Code", Command: path, Detect: []string{"claude"}}}
	isolateRuntimeRegistry(t, a)
	t.Cleanup(func() {
		if benchBusy.Load() {
			cancelBenchQueue()
			awaitBenchQueue(t)
		}
	})
	_ = putStoreJSON(bkHarnessConnections, a.agent.ID, harnessConnection{Connected: true, At: 1})
	probe := acpProbe{At: 1, Config: []map[string]any{{"id": "model", "category": "model", "options": []any{map[string]any{"value": "fixture-model", "name": "Fixture model"}}}}}
	_ = putStoreJSON(bkState, acpProbeKey+a.agent.ID, probe)
	return "claude-code:fixture-model"
}

func TestBenchNativeModelOnlyQueueAndAuth(t *testing.T) {
	for _, mode := range []string{"success", "api"} {
		t.Run(mode, func(t *testing.T) {
			id := benchNativeFixture(t, mode)
			test, err := saveCustomBenchTest("Native", "Only this prompt", 41)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = startBenchQueue(test.ID, []benchPick{{ChoiceID: id}}, false); err == nil {
				t.Fatal("native prompt ran without consent")
			}
			if _, err = startBenchQueue(test.ID, []benchPick{{ChoiceID: id}}, true); err != nil {
				t.Fatal(err)
			}
			j := awaitBenchQueue(t)
			row := j.Rows[0]
			if row.Kind != "account" || row.RuntimeID != "claude-code" {
				t.Fatalf("wrong native provenance: %+v", row)
			}
			if mode == "api" {
				if row.Status != "err" || !strings.Contains(row.Error, "native Claude account") {
					t.Fatal("API account treated as subscription")
				}
				return
			}
			if row.Status != "ok" || row.Output != "Fixture answer" || row.Result == nil || *row.Result.PromptTokens != 8 || *row.Result.CompletionTokens != 8 || row.Result.RateBasis != "end_to_end" || row.Result.ActualModel != "fixture-model" {
				t.Fatalf("native result: %+v", row)
			}
			if row.Result.PromptPerSecond != nil || row.Result.TTFT == nil {
				t.Fatal("invented native phases or missing first text")
			}
			raw, _ := json.Marshal(j)
			if strings.Contains(string(raw), "PRIVATE-FIXTURE") {
				t.Fatal("account metadata leaked")
			}
			if len(workspaceSessions.list()) != 0 {
				t.Fatal("bench created a discussion")
			}
		})
	}
}

func TestBenchNativeStreamRejectsToolsAndUnknownUsage(t *testing.T) {
	for _, tc := range []struct {
		name, stream string
		valid        bool
	}{
		{"valid", claudeBenchFixture, true},
		{"catalog tools", strings.Replace(claudeBenchFixture, `"tools":[]`, `"tools":["Read"]`, 1), false},
		{"MCP tools", strings.Replace(claudeBenchFixture, `"mcp_servers":[]`, `"mcp_servers":[{}]`, 1), false},
		{"missing catalog", strings.Replace(claudeBenchFixture, `"tools":[],`, "", 1), false},
		{"assistant tool", strings.Replace(claudeBenchFixture, `"type":"text","text":"Fixture answer"`, `"type":"tool_use","text":"PRIVATE-FIXTURE"`, 1), false},
		{"early tool", strings.Replace(claudeBenchFixture, `"type":"content_block_delta"`, `"type":"content_block_start","content_block":{"type":"tool_use"}`, 1), false},
		{"incomplete", strings.Split(claudeBenchFixture, `{"type":"result"`)[0], false},
		{"private failure", strings.Replace(claudeBenchFixture, `"subtype":"success"`, `"subtype":"failure","is_error":true,"error":"PRIVATE-FIXTURE"`, 1), false},
		{"unknown usage", strings.Replace(claudeBenchFixture, `"input_tokens":5,"output_tokens":8,"cache_read_input_tokens":2,"cache_creation_input_tokens":1`, "", 1), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, _, err := consumeClaudeBench(strings.NewReader(tc.stream), time.Now())
			if (err == nil) != tc.valid {
				t.Fatalf("stream validation: %v", err)
			}
			if err != nil && strings.Contains(err.Error(), "PRIVATE-FIXTURE") {
				t.Fatal("raw error leaked")
			}
			if tc.name == "unknown usage" && (r.CompletionTokens != nil || r.PromptTokens != nil || r.PredictedPerSec != nil) {
				t.Fatal("unknown usage converted to zero")
			}
		})
	}
}

func TestBenchNativeCancellation(t *testing.T) {
	id := benchNativeFixture(t, "wait")
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	_, _, err := runAccountBenchTest(ctx, benchTest{Prompt: "Only this prompt", MaxTokens: 41}, id)
	if err == nil || ctx.Err() == nil {
		t.Fatal("native process was not cancelled")
	}
}

func TestBenchCatalogIgnoresChatVisibilityAndUnsupportedAdapters(t *testing.T) {
	id := benchNativeFixture(t, "success")
	_ = putBytes(bkModelChoices, id, []byte("hidden"))
	agy := &acpAdapter{agent: acpAgent{ID: "antigravity", Name: "Antigravity", Command: os.Args[0]}}
	_ = registeredRuntimes.register(agy)
	_ = putStoreJSON(bkHarnessConnections, "antigravity", harnessConnection{Connected: true, At: 1})
	_ = putStoreJSON(bkState, acpProbeKey+"antigravity", acpProbe{At: 1, Config: []map[string]any{{"category": "model", "options": []any{map[string]any{"value": "native-agy"}}}}})
	w := httptest.NewRecorder()
	handleBenchCatalog(w, httptest.NewRequest(http.MethodGet, "/api/bench/catalog", nil))
	var data struct {
		Models []benchChoice `json:"models"`
	}
	if json.Unmarshal(w.Body.Bytes(), &data) != nil {
		t.Fatal(w.Body.String())
	}
	seen := false
	for _, c := range data.Models {
		if c.ID == id {
			seen = true
			if !c.Supported || c.Enabled {
				t.Fatal("Bench incorrectly follows chat visibility")
			}
		}
		if c.RuntimeID == "antigravity" && (c.Supported || c.Reason != "no_model_only_mode") {
			t.Fatal("AGY tool suppression invented")
		}
	}
	if !seen {
		t.Fatal("native model missing")
	}
	if _, err := benchRowsFromPicks([]benchPick{{ChoiceID: "antigravity:native-agy"}}); err == nil {
		t.Fatal("unsupported harness ran")
	}
}
