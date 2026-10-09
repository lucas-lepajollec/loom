package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/runtime/acp"
	"github.com/lucas-lepajollec/loom/internal/loom/runtime/agentstdio"
	"github.com/lucas-lepajollec/loom/internal/loom/runtime/opencodehttp"
)

// Optional discovery can be absent in an older agent. Preserve that fact in the
// snapshot without hiding transport failures or other protocol errors.
func discoveryResult(err error) (any, error) {
	if authRequired(err) {
		return map[string]any{"availability": "account required"}, nil
	}
	lower := strings.ToLower(err.Error())
	for _, text := range []string{"method not found", "unknown command", "unknown rpc command", "not supported", "http 404"} {
		if strings.Contains(lower, text) {
			return map[string]any{"availability": "unsupported"}, nil
		}
	}
	return nil, err
}
func probe(c candidate, env []string, dir string) (probeResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	result := probeResult{Capabilities: map[string]any{}}
	if c.ID == "opencode" {
		server := &opencodehttp.Server{}
		defer server.Close()
		client, err := server.Client(ctx, []string{c.Binary}, env)
		if err != nil {
			return result, err
		}
		if err := discoverOpenCode(ctx, client, dir, result.Capabilities); err != nil {
			return result, err
		}
		result.Message = "health + providers/agents/commands discovery"
		return result, nil
	}
	args := []string{}
	switch c.ID {
	case "codex":
		args = []string{"app-server"}
	case "pi":
		args = []string{"--mode", "rpc", "--no-session"}
	}
	cmd := exec.Command(c.Binary, args...)
	cmd.Env = env
	cmd.Dir = dir
	if c.ID != "codex" && c.ID != "pi" {
		return probeACP(ctx, cmd, dir)
	}
	client, err := agentstdio.New(cmd)
	if err != nil {
		return result, err
	}
	defer client.Close()
	if err := client.Start(); err != nil {
		return result, err
	}
	call := func(method string, params any, optional bool) error {
		var raw any
		err := client.Call(ctx, method, params, c.ID == "pi", &raw)
		if err != nil && optional {
			raw, err = discoveryResult(err)
		}
		if err == nil {
			result.Capabilities[method] = raw
		}
		return err
	}
	if c.ID == "codex" {
		if err := call("initialize", map[string]any{"clientInfo": map[string]any{"name": "loom-agents-watch", "version": "1"}, "capabilities": map[string]any{"experimentalApi": true}}, false); err != nil {
			return result, err
		}
		if err := client.Write(map[string]any{"method": "initialized"}); err != nil {
			return result, err
		}
		models := []any{}
		cursor := ""
		seen := map[string]bool{}
		for page := 0; ; page++ {
			var raw map[string]any
			params := map[string]any{}
			if cursor != "" {
				params["cursor"] = cursor
			}
			if err := client.Call(ctx, "model/list", params, false, &raw); err != nil {
				skipped, err := discoveryResult(err)
				if err != nil {
					return result, err
				}
				result.Capabilities["model/list"] = skipped
				break
			}
			data, _ := raw["data"].([]any)
			models = append(models, data...)
			next, _ := raw["nextCursor"].(string)
			// Preserve future fields on the listing envelope too.
			delete(raw, "data")
			delete(raw, "nextCursor")
			raw["models"] = models
			result.Capabilities["model/list"] = raw
			if next == "" {
				break
			}
			if seen[next] || page >= 40 || len(models) > 4096 {
				return result, errors.New("Codex model catalog pagination did not finish")
			}
			seen[next] = true
			cursor = next
		}
		result.Message = "initialize + model listing (account-only discovery may be skipped)"
	} else {
		for _, method := range []string{"get_state", "get_commands", "get_available_models"} {
			if err := call(method, nil, method != "get_state"); err != nil {
				return result, err
			}
		}
		result.Message = "state + available commands/models discovery"
	}
	return result, nil
}

func probeACP(ctx context.Context, cmd *exec.Cmd, dir string) (probeResult, error) {
	result := probeResult{Capabilities: map[string]any{}}
	client, err := acp.NewClient(cmd)
	if err != nil {
		return result, err
	}
	defer client.Close()
	client.Handler = func(*acp.Frame) (any, error) {
		return nil, &acp.RPCError{Code: -32601, Message: "watch probes do not accept interactive requests"}
	}
	var mu sync.Mutex
	announcements := map[string]any{}
	client.Notify = func(frame acp.Frame) {
		if frame.Method != "session/update" {
			return
		}
		var params struct {
			Update map[string]any `json:"update"`
		}
		if json.Unmarshal(frame.Params, &params) != nil {
			return
		}
		kind, _ := params.Update["sessionUpdate"].(string)
		switch kind {
		case "available_commands_update", "config_option_update", "current_mode_update":
			mu.Lock()
			announcements[kind] = params.Update
			mu.Unlock()
		}
	}
	if err := client.Start(); err != nil {
		return result, err
	}
	var init map[string]any
	if err := client.Call(ctx, "initialize", map[string]any{"protocolVersion": 1, "clientCapabilities": acp.ClientCapabilities(false, false), "clientInfo": map[string]string{"name": "loom-agents-watch", "version": "1"}}, &init); err != nil {
		return result, err
	}
	if init["protocolVersion"] != float64(1) {
		return result, fmt.Errorf("unexpected ACP protocol %v", init["protocolVersion"])
	}
	result.Capabilities["initialize"] = init
	var session any
	if err := client.Call(ctx, "session/new", map[string]any{"cwd": dir, "mcpServers": []any{}}, &session); err != nil {
		session, err = discoveryResult(err)
		if err != nil {
			return result, err
		}
	} else {
		// Commands can arrive immediately after session/new in session/update.
		timer := time.NewTimer(200 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return result, ctx.Err()
		}
	}
	result.Capabilities["session/new"] = session
	mu.Lock()
	defer mu.Unlock()
	if len(announcements) > 0 {
		result.Capabilities["session/update"] = announcements
	}
	result.Message = "initialize + session discovery (account-only discovery may be skipped)"
	return result, nil
}

func discoverOpenCode(ctx context.Context, client *opencodehttp.Client, dir string, announcements map[string]any) error {
	for _, endpoint := range []string{"/config/providers", "/agent", "/command"} {
		var raw any
		err := client.Call(ctx, "GET", endpoint, dir, nil, &raw)
		if err != nil {
			raw, err = discoveryResult(err)
		}
		if err != nil {
			return fmt.Errorf("%s: %w", endpoint, err)
		}
		announcements[endpoint] = raw
	}
	return nil
}
