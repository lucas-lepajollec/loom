package loom

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/coder/websocket"
)

const nodeBridgeCwd = "@loom-workdir"
const nodeBridgeStateEnv = "LOOM_NODE_BRIDGE_STATE"
const nodeBridgeKeyEnv = "LOOM_NODE_BRIDGE_KEY"

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
	if nodeHarnessEnabled() {
		modules = append(modules, "harness")
	}
	if nodeTerminalEnabled() {
		modules = append(modules, "terminal")
	}
	if nodeObserveEnabled() {
		modules = append(modules, "observe")
	}
	return modules
}
func remoteToolDirs(m RemoteMachine) []string {
	dirs := []string{}
	seen := map[string]bool{}
	for _, tool := range m.Tools {
		if dir := filepath.Dir(tool.Path); tool.Path != "" && !seen[dir] {
			seen[dir] = true
			dirs = append(dirs, dir)
		}
	}
	return dirs
}
func nodeAgent(m RemoteMachine, harness string) (acpAgent, error) {
	if !hasNodeModule(m.Modules, "harness") {
		return acpAgent{}, errors.New(nodeHarnessDisabled)
	}
	if !knownRemoteHarness(harness) {
		return acpAgent{}, errors.New("harness not supported remotely")
	}
	executable, err := os.Executable()
	if err != nil {
		return acpAgent{}, err
	}
	for _, d := range remoteHarnessDefs {
		if d.ID == harness {
			return acpAgent{ID: remoteRuntimeID(m.ID, harness), Name: d.Name, Logo: d.Logo, Command: executable,
				Args: []string{"node-bridge", m.ID, harness, nodeBridgeCwd}, Remote: true, Custom: true, Machine: m.ID, RemoteHome: m.Home}, nil
		}
	}
	return acpAgent{}, errors.New("harness not supported remotely")
}
func acpSessionArgs(agent acpAgent, cwd string) []string {
	args := append([]string(nil), agent.Args...)
	if agent.Remote && len(args) == 4 && args[0] == "node-bridge" && args[3] == nodeBridgeCwd {
		args[3] = cwd
	}
	return dshPatchArgs(agent, args)
}
func nodeMachineAccess(machine string) (nodeBridgeAccess, error) {
	return nodeMachineAccessForModule(machine, "harness")
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
		return nodeBridgeAccess{}, errors.New(nodeHarnessDisabled)
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

// A subprocess cannot read the main's in-memory vault key. For encrypted state
// only, export the selected machine credential to one encrypted, private launch
// record. Its independent key lives only in the bridge environment; never pass
// the vault key, inference key or other machines' credentials. The bridge
// consumes/removes the record and the parent also removes it on failure/close.
func nodeBridgeLaunchEnv(args []string) ([]string, func(), error) {
	noop := func() {}
	if len(args) != 4 || args[0] != "node-bridge" {
		return nil, noop, nil
	}
	env := []string{"LOOM_HOME=" + LoomHome(), nodeBridgeStateEnv + "=", nodeBridgeKeyEnv + "="}
	if !memEncActive() {
		return env, noop, nil
	}
	access, err := nodeMachineAccess(args[1])
	if err != nil {
		return nil, noop, err
	}
	key, err := randBytes(32)
	if err != nil {
		return nil, noop, err
	}
	plain, _ := json.Marshal(access)
	encrypted, err := gcmSeal(key, plain, nil)
	if err != nil {
		return nil, noop, err
	}
	dir, err := os.MkdirTemp("", "loom-node-bridge-")
	if err != nil {
		return nil, noop, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	file := filepath.Join(dir, "access")
	if err := os.WriteFile(file, encrypted, 0600); err != nil {
		cleanup()
		return nil, noop, err
	}
	env = append(env, nodeBridgeStateEnv+"="+file, nodeBridgeKeyEnv+"="+base64.StdEncoding.EncodeToString(key))
	return env, cleanup, nil
}
func readNodeBridgeAccess(machine string) (nodeBridgeAccess, error) {
	file, keyText := os.Getenv(nodeBridgeStateEnv), os.Getenv(nodeBridgeKeyEnv)
	_ = os.Unsetenv(nodeBridgeStateEnv)
	_ = os.Unsetenv(nodeBridgeKeyEnv)
	if file == "" {
		return nodeMachineAccess(machine)
	}
	defer os.Remove(file)
	key, err := base64.StdEncoding.DecodeString(keyText)
	if err != nil || len(key) != 32 {
		return nodeBridgeAccess{}, errors.New("invalid bridge launch state")
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return nodeBridgeAccess{}, errors.New("bridge launch state unavailable")
	}
	plain, err := gcmOpen(key, raw, nil)
	if err != nil {
		return nodeBridgeAccess{}, errors.New("bridge launch state unreadable")
	}
	var access nodeBridgeAccess
	if json.Unmarshal(plain, &access) != nil || access.Machine != machine || !validNodeCredential(access.Token) {
		return access, errors.New("invalid bridge machine state")
	}
	return access, nil
}
func cmdNodeBridge(args []string) error {
	return runNodeBridge(context.Background(), args, os.Stdin, os.Stdout)
}
func runNodeBridge(ctx context.Context, args []string, in io.Reader, out io.Writer) error {
	if len(args) != 3 {
		return errors.New("usage: loom node-bridge <machine-id> <harness> <cwd>")
	}
	if !knownRemoteHarness(args[1]) {
		return errors.New("harness not supported remotely")
	}
	if args[2] != "" {
		if _, err := remoteWorkdir(args[2]); err != nil {
			return err
		}
	}
	access, err := readNodeBridgeAccess(args[0])
	if err != nil {
		return err
	}
	base, _, err := cleanNodeURL(access.URL)
	if err != nil {
		return err
	}
	u, _ := url.Parse(base + "/api/node/harness/acp")
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	q := url.Values{"harness": {args[1]}, "cwd": {args[2]}}
	u.RawQuery = q.Encode()
	header := http.Header{"Authorization": {"Bearer " + access.Token}}
	dialCtx, dialCancel := context.WithTimeout(ctx, 30*time.Second)
	defer dialCancel()
	// Timeout applies to the handshake only. Stream lifetime is the parent ACP
	// process lifetime; redirects are refused just like other node requests.
	client := &http.Client{Transport: nodeClient.Transport, CheckRedirect: nodeClient.CheckRedirect}
	conn, resp, err := websocket.Dial(dialCtx, u.String(), &websocket.DialOptions{HTTPHeader: header, HTTPClient: client})
	if err != nil {
		if resp != nil && resp.StatusCode == 409 {
			return errors.New(nodeHarnessDisabled)
		}
		if resp != nil && resp.StatusCode == 429 {
			return errors.New("node agent process limit reached")
		}
		return errors.New("node ACP connection failed; check machine access, harness and directory")
	}
	defer conn.CloseNow()
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stream := websocket.NetConn(streamCtx, conn, websocket.MessageBinary)
	conn.SetReadLimit(acpMaxFrame)
	done := make(chan error, 2)
	go func() { _, err := io.Copy(stream, in); done <- err }()
	go func() { _, err := io.Copy(out, stream); done <- err }()
	select {
	case err := <-done:
		cancel()
		conn.CloseNow()
		if err == io.EOF || websocket.CloseStatus(err) == websocket.StatusNormalClosure {
			return nil
		}
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func nodeMachineJSON(ctx context.Context, m RemoteMachine, method, path string, body io.Reader, result any) error {
	module := "harness"
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
	if path == "/api/node/harness/inventory" || path == "/api/node/harness/lifecycle" {
		copied := *nodeClient
		copied.Timeout = 30 * time.Second
		if path == "/api/node/harness/lifecycle" {
			copied.Timeout = harnessActionTimeout
		}
		client = &copied
	}
	resp, err := client.Do(request)
	if ctx.Err() == nil {
		workspaceSessions.observeHealth("node:"+m.ID, m.Name, m.ID, err == nil && resp.StatusCode == 200, false)
	}
	if err != nil {
		return errors.New("node machine unreachable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 && path != "/api/node/harness/lifecycle" {
		if resp.StatusCode == 409 {
			if module == "observe" {
				return errors.New(nodeObserveDisabled)
			}
			return errors.New(nodeHarnessDisabled)
		}
		return errors.New("node machine rejected the request; check credential and directory")
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(result); err != nil {
		return err
	}
	if resp.StatusCode != 200 {
		// Lifecycle failures carry useful state and the installer log. Preserve
		// them above, along with contention/authorization status below.
		return runtimeActionError{resp.StatusCode, "node harness lifecycle failed"}
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
	if err := nodeMachineJSON(ctx, m, http.MethodGet, "/api/node/harness/inventory", nil, &info); err != nil {
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
