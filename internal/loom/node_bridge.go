package loom

import (
	"context"

	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"

	"time"
)

type nodeBridgeAccess struct {
	Machine string `json:"machine"`
	URL     string `json:"url"`
	Token   string `json:"token"`
}

func nodeModules() []string {
	modules := []string{"engine"}
	if nodeVoiceEnabled() {
		modules = append(modules, "voice")
	}
	if nodeTerminalEnabled() {
		modules = append(modules, "terminal")
	}
	if nodeObserveEnabled() {
		modules = append(modules, "observe")
	}
	return modules
}

func nodeMachineAccessForModule(machine, module string) (nodeBridgeAccess, error) {
	m, err := workspaceMachine(machine)
	if err != nil {
		return nodeBridgeAccess{}, err
	}
	return nodeMachineModuleAccess(m, module)
}

func nodeMachineModuleAccess(m RemoteMachine, module string) (nodeBridgeAccess, error) {
	if capabilityDisabled("machine:"+m.ID, module) {
		return nodeBridgeAccess{}, errors.New("machine capability degraded; run Doctor")
	}
	if !hasNodeModule(m.Modules, module) {
		if module == "voice" {
			return nodeBridgeAccess{}, errors.New("node voice module unavailable; update or reconnect this machine")
		}
		if module == "observe" {
			return nodeBridgeAccess{}, errors.New(nodeObserveDisabled)
		}
		if module == "terminal" {
			return nodeBridgeAccess{}, errors.New(nodeTerminalDisabled)
		}
		return nodeBridgeAccess{}, errors.New("node module unavailable")
	}
	n := savedMachineNode(m)
	if n == nil || n.Direct || n.Role != "engine-node" || !validNodeCredential(n.WebKey) {
		return nodeBridgeAccess{}, errors.New("node machine credential unavailable; reconnect or unlock Loom")
	}
	if err := checkNodeHandshake(n.Handshake); err != nil {
		return nodeBridgeAccess{}, err
	}
	return nodeBridgeAccess{Machine: m.ID, URL: n.URL, Token: n.WebKey}, nil
}

func nodeMachineJSON(ctx context.Context, m RemoteMachine, method, path string, body io.Reader, result any) error {
	module := "engine"
	if path == "/api/node/observe" {
		module = "observe"
	}
	access, err := nodeMachineAccessForModule(m.ID, module)
	if err != nil {
		return err
	}
	base, _, err := cleanNodeURL(access.URL)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, method, base+path, body)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+access.Token)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	client := nodeClient
	resp, err := client.Do(request)
	if ctx.Err() == nil {
		workspaceSessions.observeHealth("node:"+m.ID, m.Name, m.ID, err == nil && resp.StatusCode == 200, false)
	}
	if err != nil {
		return errors.New("node machine unreachable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		if resp.StatusCode == 409 {
			if module == "observe" {
				return errors.New(nodeObserveDisabled)
			}
			return errors.New("node module unavailable")
		}
		return errors.New("node machine rejected the request; check credential and directory")
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(result); err != nil {
		return err
	}
	return nil
}
func refreshNodeMachine(ctx context.Context, m RemoteMachine) (RemoteMachine, error) {
	var info struct {
		OK       bool         `json:"ok"`
		Tools    []RemoteTool `json:"tools"`
		OS       string       `json:"os"`
		Home     string       `json:"home"`
		Hostname string       `json:"hostname"`
	}
	if err := nodeMachineJSON(ctx, m, http.MethodGet, "/api/node/inventory", nil, &info); err != nil {
		return m, err
	}
	if !info.OK {
		return m, errors.New("node inventory unavailable")
	}
	m.Tools, m.OS, m.Home, m.Hostname, m.CheckedAt = info.Tools, info.OS, info.Home, info.Hostname, time.Now().UnixMilli()
	return m, nil
}
func nodeFolders(ctx context.Context, m RemoteMachine, path string) (map[string]any, error) {
	var result map[string]any
	err := nodeMachineJSON(ctx, m, http.MethodGet, "/api/node/folders?"+url.Values{"path": {path}}.Encode(), nil, &result)
	return result, err
}
