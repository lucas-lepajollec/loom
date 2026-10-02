package loom

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestAntigravityStream(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
		fail             bool
	}{
		{"stream", "{\"event\":\"step_update\",\"step_update\":{\"step_type\":\"agent_response\",\"text_delta\":\"Hello\"}}\n{\"event\":\"step_update\",\"step_update\":{\"step_type\":\"tool\",\"tool_name\":\"view_file\",\"step_index\":3,\"state\":\"DONE\"}}\n{\"event\":\"result\",\"result\":{\"status\":\"SUCCESS\",\"conversation_id\":\"native-id\",\"response\":\"Hello\",\"usage\":{\"input_tokens\":7,\"output_tokens\":2,\"total_tokens\":9}}}\n", "Hello", false},
		{"final-only", "{\"event\":\"result\",\"result\":{\"status\":\"SUCCESS\",\"response\":\"Hello\"}}\n", "Hello", false},
		{"missing-final", "{\"event\":\"step_update\",\"step_update\":{\"step_type\":\"agent_response\",\"text_delta\":\"Partial\"}}\n", "Partial", true},
		{"private-error", "{\"event\":\"result\",\"result\":{\"status\":\"ERROR\",\"error\":\"secret-key\"}}\n", "", true},
		{"malformed", "bad\n", "", true},
		{"inconsistent", "{\"event\":\"step_update\",\"step_update\":{\"step_type\":\"agent_response\",\"text_delta\":\"Partial\"}}\n{\"event\":\"result\",\"result\":{\"status\":\"SUCCESS\",\"response\":\"Different\"}}\n", "Partial", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var text, native string
			var usage *RuntimeUsage
			events := 0
			answer, err := consumeAgyStream(context.Background(), strings.NewReader(tc.body), func(e StreamEvent) bool {
				text += e.Content
				if e.Usage != nil {
					usage = e.Usage
				}
				if e.HarnessEvent != nil {
					events++
				}
				if e.NativeSessionID != "" {
					native = e.NativeSessionID
				}
				return true
			})
			if (err != nil) != tc.fail || text != tc.want {
				t.Fatalf("answer %q / text %q / error %v", answer, text, err)
			}
			if err != nil && strings.Contains(err.Error(), "secret-key") {
				t.Fatal("private diagnostics leaked")
			}
			if tc.name == "stream" && (usage == nil || usage.Total != 9 || native != "native-id" || events != 1) {
				t.Fatal("metadata missing")
			}
		})
	}
}
func TestAntigravitySubprocessStdinAndCancellation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fixture")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "agy-fixture")
	script := "#!/bin/sh\nfor arg in \"$@\"; do case \"$arg\" in --dangerously-skip-permissions|--continue|--conversation) exit 20;; esac; done\nread -r input\ncase \"$input\" in *'portable messages'*'context-marker'*) ;; *) exit 21;; esac\nprintf '%s\\n' '{\"event\":\"result\",\"result\":{\"status\":\"SUCCESS\",\"response\":\"fixture\"}}'\n"
	if os.WriteFile(path, []byte(script), 0700) != nil {
		t.Fatal("fixture write failed")
	}
	a := antigravityAdapter{model: "fixture-model", executable: path}
	result, err := a.Run(context.Background(), RuntimeTurn{Messages: []Message{{Role: "user", Content: "context-marker"}}}, func(StreamEvent) bool { return true })
	if err != nil || result[0].Content != "fixture" {
		t.Fatalf("%v %v", result, err)
	}
	if os.WriteFile(path, []byte("#!/bin/sh\nexec sleep 20\n"), 0700) != nil {
		t.Fatal("fixture write failed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = a.Run(ctx, RuntimeTurn{}, func(StreamEvent) bool { return true })
	if err == nil || time.Since(start) > 3*time.Second {
		t.Fatalf("cancellation failed %v", err)
	}
}
func TestAntigravityModelSelectionRequiresConsent(t *testing.T) {
	testHome(t)
	m := newRuntimeSessions()
	s, err := m.create("", "", false)
	if err != nil {
		t.Fatal(err)
	}
	// Models come from the ACP bridge's probe, like every harness.
	probe := acpProbe{Config: []map[string]any{{"id": "model", "category": "model", "type": "select", "currentValue": "native-test",
		"options": []any{map[string]any{"value": "native-test", "name": "Native"}}}}}
	if putStoreJSON(bkState, acpProbeKey+"antigravity", probe) != nil {
		t.Fatal("probe write failed")
	}
	if _, err = m.selectModel(s.ID, "antigravity:native-test", false); err == nil {
		t.Fatal("external handoff without consent")
	}
	if agyInstalled() {
		selected, err := m.selectModel(s.ID, "antigravity:native-test", true)
		if err != nil || selected.ID != s.ID || selected.RuntimeID != "antigravity" {
			t.Fatalf("selection %v %v", selected, err)
		}
	}
}

func TestAntigravityReportedMetricsAndSafeToolDetails(t *testing.T) {
	body := `{"event":"step_update","step_update":{"step_index":1,"step_type":"agent_response","state":"DONE","text_delta":"OK","usage":{"input_tokens":10,"output_tokens":40,"total_tokens":50,"thinking_tokens":30}}}
{"event":"step_update","step_update":{"step_index":2,"step_type":"tool","state":"DONE","tool_info":{"name":"write_to_file","parameters":{"TargetFile":"/tmp/fixture.txt","CodeContent":"private-code"},"output":"private-output","error":{"message":"private-error"}}}}
{"event":"result","result":{"status":"SUCCESS","response":"OK","duration_seconds":2.5,"usage":{"input_tokens":12,"output_tokens":42,"total_tokens":54,"thinking_tokens":30}}}
`
	var usage *RuntimeUsage
	var tool *HarnessEvent
	var duration float64
	_, err := consumeAgyStream(context.Background(), strings.NewReader(body), func(e StreamEvent) bool {
		if e.Usage != nil {
			usage = e.Usage
		}
		if e.HarnessEvent != nil {
			tool = e.HarnessEvent
		}
		if e.DurationSeconds > 0 {
			duration = e.DurationSeconds
		}
		if e.Reasoning != "" {
			t.Fatal("hidden reasoning invented")
		}
		return true
	})
	if err != nil || usage == nil || usage.Output != 42 || usage.Thinking != 30 || duration != 2.5 || tool == nil || !tool.Failed || tool.FileTarget != "/tmp/fixture.txt" {
		t.Fatalf("missing native metadata: %v %+v %+v %v", err, usage, tool, duration)
	}
	encoded, _ := json.Marshal(tool)
	if strings.Contains(string(encoded), "private-") {
		t.Fatal("private tool details leaked")
	}
	if validAgyUsage(&agyUsage{Thinking: -1}) || validAgyUsage(&agyUsage{Input: 1e12}) {
		t.Fatal("invalid usage accepted")
	}
}
