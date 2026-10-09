package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/lucas-lepajollec/loom/internal/loom/runtime/opencodehttp"
)

// An actual subprocess checks the framing and ensures probes send discovery
// only. Any turn, sign-in, or otherwise unexpected method makes it fail.
func TestWatchProbeHelper(t *testing.T) {
	kind := os.Getenv("LOOM_WATCH_FIXTURE")
	if kind == "" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var request map[string]any
		if json.Unmarshal(scanner.Bytes(), &request) != nil {
			os.Exit(2)
		}
		method, _ := request["method"].(string)
		if kind == "pi" {
			method, _ = request["type"].(string)
		}
		var result any
		var rpcError any
		switch method {
		case "initialized":
			continue
		case "initialize":
			result = map[string]any{"protocolVersion": 1, "agentCapabilities": map[string]any{"futureFeature": true}, "agentInfo": map[string]any{"name": "fixture", "version": "1"}, "_meta": map[string]any{"future.extension": "volatile"}}
		case "session/new":
			if kind == "acp-auth" {
				rpcError = map[string]any{"code": -32000, "message": "Authentication required"}
			} else {
				_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "volatile", "update": map[string]any{"sessionUpdate": "available_commands_update", "availableCommands": []any{map[string]any{"name": "compact"}}}}})
				result = map[string]any{"sessionId": "volatile", "modes": map[string]any{"availableModes": []any{map[string]any{"id": "code"}}}, "configOptions": []any{map[string]any{"id": "future-config", "type": "select"}}}
			}
		case "model/list":
			params, _ := request["params"].(map[string]any)
			cursor, _ := params["cursor"].(string)
			if value, present := params["cursor"]; present && value == "" {
				os.Exit(5)
			}
			next := ""
			effort := "high"
			if cursor == "" {
				next = "page2"
				effort = "low"
			}
			result = map[string]any{"futureEnvelope": true, "nextCursor": next, "data": []any{map[string]any{"id": "model", "futureFlag": true, "supportedReasoningEfforts": []any{map[string]any{"reasoningEffort": effort}}}}}
		case "get_state":
			result = map[string]any{"sessionId": "volatile", "futureState": true}
		case "get_commands":
			result = map[string]any{"commands": []any{map[string]any{"name": "compact", "futureCommandField": true}}}
		case "get_available_models":
			result = map[string]any{"models": []any{map[string]any{"id": "fixture", "reasoning": true}}}
		default:
			os.Exit(3)
		}
		response := map[string]any{"id": request["id"], "jsonrpc": "2.0", "result": result}
		if rpcError != nil {
			response["error"] = rpcError
			delete(response, "result")
		}
		if kind == "pi" {
			response = map[string]any{"id": request["id"], "type": "response", "command": method, "success": true, "data": result}
		}
		if encoder.Encode(response) != nil {
			os.Exit(4)
		}
	}
	os.Exit(0)
}
func TestProbesCaptureUnknownAnnouncementsWithoutTurns(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("weekly watch uses Unix npm shims")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	binary := filepath.Join(dir, "agent")
	script := "#!/bin/sh\nexec '" + strings.ReplaceAll(exe, "'", "'\\''") + "' -test.run=TestWatchProbeHelper -- \"$@\"\n"
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ id, fixture, field string }{{"codex", "codex", "futureEnvelope"}, {"pi", "pi", "futureCommandField"}, {"claude-acp", "acp", "future-config"}, {"claude-acp", "acp-auth", "account required"}} {
		t.Run(tc.fixture, func(t *testing.T) {
			result, err := probe(candidate{ID: tc.id, Binary: binary}, append(os.Environ(), "LOOM_WATCH_FIXTURE="+tc.fixture), dir)
			if err != nil {
				t.Fatal(err)
			}
			data, err := normalizeCapabilities(result.Capabilities)
			if err != nil || !strings.Contains(string(data), tc.field) || strings.Contains(string(data), "volatile") {
				t.Fatal(string(data), err)
			}
			if tc.id == "codex" && (!strings.Contains(string(data), "high") || !strings.Contains(string(data), "low")) {
				t.Fatal("lost model page", string(data))
			}
			if tc.fixture == "acp" && !strings.Contains(string(data), "compact") {
				t.Fatal("lost command notification", string(data))
			}
		})
	}
}
func TestOpenCodeNoAccountEndpoints(t *testing.T) {
	calls := []string{}
	client := opencodehttp.New("http://127.0.0.1:1234", "loom", "ephemeral-server-secret")
	client.HTTP = &http.Client{Transport: notesTransport(func(req *http.Request) (*http.Response, error) {
		calls = append(calls, req.URL.Path)
		if req.Method != "GET" {
			t.Fatal("non-read-only probe", req.Method)
		}
		switch req.URL.Path {
		case "/config/providers":
			return notesResponse(`{"providers":[{"id":"native","models":{"model-id":{"reasoning":true,"futureModelField":true}}}]}`, 200), nil
		case "/agent":
			return notesResponse(`[{"name":"build","futureAgentFlag":true}]`, 200), nil
		case "/command":
			return notesResponse(`[{"name":"review","futureCommandFlag":true}]`, 200), nil
		default:
			return nil, fmt.Errorf("unexpected endpoint")
		}
	})}
	raw := map[string]any{}
	if err := discoverOpenCode(context.Background(), client, "/empty-profile", raw); err != nil {
		t.Fatal(err)
	}
	data, err := normalizeCapabilities(raw)
	if err != nil || len(calls) != 3 || !strings.Contains(string(data), "futureModelField") || !strings.Contains(string(data), "futureAgentFlag") || !strings.Contains(string(data), "futureCommandFlag") {
		t.Fatal(string(data), calls, err)
	}
}
func TestOptionalDiscoveryDoesNotHideFailures(t *testing.T) {
	for _, text := range []string{"Authentication required", "Unknown command: get_commands", "OpenCode HTTP 404"} {
		value, err := discoveryResult(fmt.Errorf("%s", text))
		if err != nil || value == nil {
			t.Fatal(value, err)
		}
	}
	if _, err := discoveryResult(fmt.Errorf("process exited")); err == nil {
		t.Fatal("transport error skipped")
	}
}
