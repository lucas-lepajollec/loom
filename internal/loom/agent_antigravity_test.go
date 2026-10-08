//go:build !windows

package loom

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	agent "github.com/lucas-lepajollec/loom/internal/loom/runtime"
	"github.com/lucas-lepajollec/loom/internal/loom/runtime/antigravity"
)

func fakeNativeAntigravity(t *testing.T, help string) (acpAgent, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "agy")
	script := `#!/bin/sh
case "$1" in
 --help) printf '%s\n' "$LOOM_AGY_TEST_HELP" >&2; exit 0;;
 --version) echo '1.3.1'; exit 0;;
 models) printf 'fixture-model\tFixture model\n'; exit 0;;
esac
printf '%s\n' "$@" > "$LOOM_AGY_TEST_ARGS"
cat > "$LOOM_AGY_TEST_INPUT"
cat "$LOOM_AGY_TEST_FRAMES"
`
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LOOM_AGY_TEST_HELP", help)
	return acpAgent{ID: "antigravity", Name: "Antigravity", Command: "loom", Args: []string{"agy-acp"}}, home
}

func TestAntigravityNativeSelectionAndFallback(t *testing.T) {
	for _, native := range []bool{false, true} {
		t.Run(map[bool]string{false: "older-cli", true: "structured"}[native], func(t *testing.T) {
			testHome(t)
			help := "--output-format stream-json"
			if native {
				help += " --input-format stream-json --conversation --effort low|medium|high|xhigh|max --sandbox"
			}
			a, _ := fakeNativeAntigravity(t, help)
			protocol := nativeAgentProtocol(a)
			if (protocol == antigravity.Protocol) != native {
				t.Fatal(protocol)
			}
			a.Custom = true
			if nativeAgentProtocol(a) != "" {
				t.Fatal("custom launcher overridden")
			}
			a.Custom = false
			a.Remote = true
			if nativeAgentProtocol(a) != "" {
				t.Fatal("remote launcher overridden")
			}
		})
	}
}

func TestAntigravityNativeProbeResumeAndCanonicalProjection(t *testing.T) {
	testHome(t)
	a, home := fakeNativeAntigravity(t, "--input-format stream-json --output-format stream-json --conversation --effort low|medium|high|xhigh|max --sandbox")
	probe := probeACPAgent(context.Background(), a)
	if probe.Error != "" || probe.Compatibility.Protocol != antigravity.Protocol || probe.Compatibility.Version != "1.3.1" || probe.Compatibility.Warning != "" {
		t.Fatalf("probe %+v", probe)
	}
	if err := putStoreJSON(bkState, acpProbeKey+a.ID, probe); err != nil {
		t.Fatal(err)
	}
	if len(probe.Config) != 3 || probe.Config[1]["id"] != "reasoning_effort" {
		t.Fatal(probe.Config)
	}
	if record := antigravityCompatibility(a, "1.4.0"); record.Warning == "" {
		t.Fatal("version drift silent")
	}
	for _, capability := range antigravityCaps() {
		if capability == "approvals" || capability == "user-input" || capability == "mcp" {
			t.Fatal("unsupported capability", capability)
		}
	}
	frames, err := filepath.Abs("testdata/agents/antigravity/normal.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := os.ReadFile(frames)
	if err != nil {
		t.Fatal(err)
	}
	native := []string{}
	for _, line := range strings.Split(strings.TrimSpace(string(rows)), "\n") {
		var row struct {
			Frame json.RawMessage `json:"frame"`
		}
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatal(err)
		}
		native = append(native, string(row.Frame))
	}
	framePath := filepath.Join(home, "frames")
	if err := os.WriteFile(framePath, []byte(strings.Join(native, "\n")+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	argsPath, inputPath := filepath.Join(home, "args"), filepath.Join(home, "input")
	t.Setenv("LOOM_AGY_TEST_FRAMES", framePath)
	t.Setenv("LOOM_AGY_TEST_ARGS", argsPath)
	t.Setenv("LOOM_AGY_TEST_INPUT", inputPath)
	m := newRuntimeSessions()
	turn := RuntimeTurn{Messages: []Message{{Role: "user", Content: "First"}}}
	s := RuntimeSession{ID: "fixture", LastRequestID: "turn-1", Model: "fixture-model", ACPState: ACPState{Workdir: home, Permission: "ask", ConfigOptions: map[string]any{"sandbox": true, "reasoning_effort": "max"}}}
	var state ACPState
	var usage *RuntimeUsage
	emit := func(event StreamEvent) bool {
		if event.ACPState != nil {
			state = cloneACPState(*event.ACPState)
		}
		if event.Usage != nil {
			usage = event.Usage
		}
		if event.AgentEvent != nil && event.AgentEvent.Type == "usage.spent" && event.ACPEvent["context"] != nil {
			t.Error("turn spend presented as context occupancy")
		}
		return true
	}
	result, err := m.runAntigravity(context.Background(), a, s, turn, emit)
	if err != nil || len(result) != 1 || result[0].Content != "Hello" || state.NativeSessionID != "fixture-conversation" || state.NativeRuntimeID != a.ID || usage == nil || usage.Total != 18 {
		t.Fatalf("result %v state %+v usage %+v err %v", result, state, usage, err)
	}
	args, _ := os.ReadFile(argsPath)
	if strings.Contains(string(args), "--conversation") || strings.Contains(string(args), "--dangerously-skip-permissions") || !strings.Contains(string(args), "--effort\nmax") || !strings.Contains(string(args), "--sandbox") {
		t.Fatal(string(args))
	}
	s.ACPState = state
	turn.Messages = append(turn.Messages, result...)
	turn.Messages = append(turn.Messages, Message{Role: "user", Content: "Next"})
	s.LastRequestID = "turn-2"
	_, err = m.runAntigravity(context.Background(), a, s, turn, emit)
	if err != nil {
		t.Fatal(err)
	}
	args, _ = os.ReadFile(argsPath)
	input, _ := os.ReadFile(inputPath)
	if !strings.Contains(string(args), "--conversation\nfixture-conversation") || strings.Contains(string(input), "portable discussion") || !strings.Contains(string(input), `"content":"Next"`) {
		t.Fatalf("resume args %s input %s", args, input)
	}
	// Route/context edits use an explicit fresh text handoff.
	s.NativeContext = "changed"
	_, err = m.runAntigravity(context.Background(), a, s, turn, emit)
	if err != nil {
		t.Fatal(err)
	}
	args, _ = os.ReadFile(argsPath)
	if strings.Contains(string(args), "--conversation") {
		t.Fatal("incompatible context resumed", string(args))
	}
	if _, err = listNativeAgentSessions(context.Background(), a); err == nil {
		t.Fatal("unsupported list simulated")
	}
	if row := newAgentProjection().event(agent.AgentEvent{Type: "item.completed", ItemID: "denied", ItemType: "tool_call", Error: "Native denial", Payload: agent.JSON(map[string]any{"toolName": "ask_permission", "aggregatedOutput": ""})}); row["tool"].(map[string]any)["output"] != "Native denial" {
		t.Fatal(row)
	}
	s.ConfigOptions["sandbox"] = "true"
	if _, err := m.runAntigravity(context.Background(), a, s, turn, emit); err == nil || !strings.Contains(err.Error(), "requires a boolean") {
		t.Fatal("sandbox selection silently ignored", err)
	}
}
