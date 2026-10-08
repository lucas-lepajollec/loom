package loom

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/lucas-lepajollec/loom/internal/loom/resources"
)

func nodeHarnessFixture(t *testing.T, limit int) (*nodeHarnessServer, http.Handler, string) {
	t.Helper()
	testHome(t)
	token := "node-management-fixture"
	server := newNodeHarnessServer(limit)
	server.probe = func(context.Context) (RemoteMachine, error) {
		return RemoteMachine{OS: "Linux", Home: t.TempDir(), Hostname: "paired-fixture", Tools: []RemoteTool{{ID: "hermes", Path: "/usr/bin/hermes", Version: "1.0"}}}, nil
	}
	return server, newEngineWorkerMuxHarness(token, server), token
}
func nodeHarnessHTTP(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("loopback unavailable: %v", err)
	}
	srv := httptest.NewUnstartedServer(handler)
	srv.Listener.Close()
	srv.Listener = listener
	srv.Start()
	t.Cleanup(srv.Close)
	return srv
}
func nodeHarnessGet(handler http.Handler, path, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}
func TestNodeHarnessInventoryAndValidation(t *testing.T) {
	_, mux, token := nodeHarnessFixture(t, 8)
	dir := t.TempDir()
	if w := nodeHarnessGet(mux, "/api/node/harness/inventory", token); w.Code != 200 || !strings.Contains(w.Body.String(), `"version":"1.0"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, path := range []string{"/api/node/harness/inventory", "/api/node/folders", "/api/node/harness/acp?harness=hermes"} {
		for _, credential := range []string{"", "inference-key"} {
			if w := nodeHarnessGet(mux, path, credential); w.Code != 401 {
				t.Fatal(path, w.Code)
			}
		}
	}
	for _, path := range []string{"/api/node/harness/acp?harness=unknown", "/api/node/harness/acp?harness=hermes&cwd=relative", "/api/node/harness/acp?harness=hermes&cwd=" + url.QueryEscape(filepath.Join(dir, "absent")), "/api/node/folders?path=relative"} {
		if w := nodeHarnessGet(mux, path, token); w.Code != 400 {
			t.Fatal(path, w.Code, w.Body.String())
		}
	}
	for _, header := range []string{"Origin"} {
		r := httptest.NewRequest("GET", "/api/node/harness/inventory", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set(header, "http://localhost")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatal("browser accepted", header, w.Code)
		}
	}
	descriptor, err := describeNode()
	if err != nil || descriptor.Handshake != 1 || !hasNodeModule(descriptor.Modules, "harness") {
		t.Fatal(descriptor, err)
	}
	if err := putBool(bkState, nodeHarnessDisabledKey, true); err != nil {
		t.Fatal(err)
	}
	descriptor, _ = describeNode()
	if hasNodeModule(descriptor.Modules, "harness") {
		t.Fatal("disabled advertised")
	}
	for _, path := range []string{"/api/node/harness/inventory", "/api/node/harness/acp?harness=hermes", "/api/node/folders"} {
		if w := nodeHarnessGet(mux, path, token); w.Code != 409 || !strings.Contains(w.Body.String(), nodeHarnessDisabled) {
			t.Fatal(path, w.Code, w.Body.String())
		}
	}
}
func TestNodeHarnessLaunchUsesSharedDefinitionsAndPath(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("node launch is Linux-only")
	}
	dir := t.TempDir()
	binary := filepath.Join(dir, "hermes")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf '%s\\n' \"$1\"\npwd\n"), 0700); err != nil {
		t.Fatal(err)
	}
	m := RemoteMachine{Tools: []RemoteTool{{ID: "hermes", Path: binary}}}
	cmd, err := nodeHarnessCommand(m, "hermes", dir)
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.Output()
	if err != nil || string(out) != "acp\n"+dir+"\n" {
		t.Fatal(string(out), err)
	}
	if _, err := nodeHarnessCommand(m, "codex", dir); err == nil {
		t.Fatal("missing adapter requirements accepted")
	}
}
func TestNodeHarnessWebSocketEchoAndLimit(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("node launch is Linux-only")
	}
	server, mux, token := nodeHarnessFixture(t, 1)
	cwd := t.TempDir()
	pidFile := filepath.Join(cwd, "pid")
	childFile := filepath.Join(cwd, "child-pid")
	server.command = func(_ RemoteMachine, _, dir string) (*exec.Cmd, error) {
		cmd := exec.Command("sh", "-c", "echo $$ > "+shellQuote(pidFile)+"; sleep 300 & echo $! > "+shellQuote(childFile)+"; cat")
		cmd.Dir = dir
		return cmd, nil
	}
	srv := nodeHarnessHTTP(t, mux)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	endpoint := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/node/harness/acp?harness=hermes&cwd=" + url.QueryEscape(cwd)
	opts := &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + token}}}
	conn, _, err := websocket.Dial(ctx, endpoint, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	payload := []byte("{\"id\":1,\"method\":\"echo\"}\n{\"id\":2}\n")
	if err := conn.Write(ctx, websocket.MessageBinary, payload[:5]); err != nil {
		t.Fatal(err)
	}
	if err := conn.Write(ctx, websocket.MessageBinary, payload[5:]); err != nil {
		t.Fatal(err)
	}
	received := []byte{}
	for len(received) < len(payload) {
		typ, data, err := conn.Read(ctx)
		if err != nil || typ != websocket.MessageBinary {
			t.Fatal(typ, err)
		}
		received = append(received, data...)
	}
	if !bytes.Equal(received, payload) {
		t.Fatal(string(received))
	}
	_, response, err := websocket.Dial(ctx, endpoint, opts)
	if err == nil || response == nil || response.StatusCode != 429 {
		t.Fatal("limit not enforced", response, err)
	}
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	childPID, err := os.ReadFile(childFile)
	if err != nil {
		t.Fatal(err)
	}
	conn.CloseNow()
	deadline := time.Now().Add(3 * time.Second)
	for len(server.slots) > 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if len(server.slots) != 0 {
		t.Fatal("child not reaped / slot leaked")
	}
	// POSIX kill -0 checks the actual owned process has gone, not only its slot.
	if err := exec.Command("kill", "-0", strings.TrimSpace(string(raw))).Run(); err == nil {
		t.Fatal("agent survived connection close")
	}
	childStat, err := os.ReadFile("/proc/" + strings.TrimSpace(string(childPID)) + "/stat")
	if err == nil && !strings.Contains(string(childStat), ") Z ") {
		t.Fatal("owned descendant survived close")
	}
	conn2, _, err := websocket.Dial(ctx, endpoint, opts)
	if err != nil {
		t.Fatal("slot not reusable", err)
	}
	conn2.CloseNow()
	// Shutdown also tears down an active hijacked connection's owned child.
	deadline = time.Now().Add(3 * time.Second)
	for len(server.slots) > 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	conn3, _, err := websocket.Dial(ctx, endpoint, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer conn3.CloseNow()
	server.stop()
	if _, _, err := conn3.Read(ctx); err == nil {
		t.Fatal("node shutdown left agent connected")
	}
}
func TestNodeMachineOffersFoldersWorkspacesAndBridge(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("node launch is Linux-only")
	}
	server, mux, token := nodeHarnessFixture(t, 8)
	cwd := t.TempDir()
	server.command = func(_ RemoteMachine, _, dir string) (*exec.Cmd, error) {
		cmd := exec.Command("cat")
		cmd.Dir = dir
		return cmd, nil
	}
	srv := nodeHarnessHTTP(t, mux)
	m, err := savePairedMachine(RemoteMachine{Name: "node fixture", Host: "127.0.0.1"}, &engineNode{URL: srv.URL, WebKey: token, Role: "engine-node", NodeID: "fixture", Modules: []string{"engine", "harness"}, Handshake: 1})
	if err != nil {
		t.Fatal(err)
	}
	m, err = checkRemoteMachine(context.Background(), m)
	if err != nil || m.Home == "" || m.CheckedAt == 0 {
		t.Fatal(m, err)
	}
	if err := saveRemoteMachine(m, nil); err != nil {
		t.Fatal(err)
	}
	var installation agentInstallation
	for _, i := range agentInstallations() {
		if i.Machine == m.ID && i.Harness == "hermes" {
			installation = i
		}
	}
	if !installation.Installed || !installation.Ready || installation.Managed || installation.Enabled {
		t.Fatal(installation)
	}
	agent, err := nodeAgent(m, "hermes")
	if err != nil || agent.Command == "ssh" || !agent.Remote || agent.Machine != m.ID {
		t.Fatal(agent, err)
	}
	if strings.Contains(strings.Join(agent.Args, " "), token) {
		t.Fatal("token in argv")
	}
	if args := acpSessionArgs(agent, cwd); args[3] != cwd || agent.Args[3] != nodeBridgeCwd {
		t.Fatal(args, agent.Args)
	}
	if err := saveRemoteMachine(m, []acpAgent{agent}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { registeredRuntimes.remove(agent.ID) })
	for _, i := range agentInstallations() {
		if i.RuntimeID == agent.ID && (!i.Enabled || !i.Managed) {
			t.Fatal(i)
		}
	}
	if _, _, err := terminalCommand(m.ID, cwd, ""); err == nil || !strings.Contains(err.Error(), "terminals need SSH for now") {
		t.Fatal(err)
	}
	child := filepath.Join(cwd, "child")
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, "file"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	handleMachineFolders(w, httptest.NewRequest("GET", "/api/machines/folders?machine="+m.ID+"&path="+url.QueryEscape(cwd), nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), child) || strings.Contains(w.Body.String(), `"file"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	created := filepath.Join(cwd, "workspace")
	got, err := prepareWorkspace(context.Background(), resources.Workspace{Target: m.ID, Path: created}, true)
	if err != nil || got != created {
		t.Fatal(got, err)
	}
	if _, err := prepareWorkspace(context.Background(), resources.Workspace{Target: m.ID, Path: created}, false); err != nil {
		t.Fatal(err)
	}
	// Real WebSocket transport; bridge reads URL and token from saved local state.
	input, send := io.Pipe()
	output, receive := io.Pipe()
	defer input.Close()
	defer send.Close()
	defer output.Close()
	defer receive.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- runNodeBridge(ctx, []string{m.ID, "hermes", cwd}, input, receive) }()
	line := "{\"jsonrpc\":\"2.0\",\"id\":1}\n"
	if _, err := send.Write([]byte(line)); err != nil {
		t.Fatal(err)
	}
	gotLine, err := bufio.NewReader(output).ReadString('\n')
	if err != nil || gotLine != line {
		t.Fatal(gotLine, err)
	}
	send.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	access, err := readNodeBridgeAccess(m.ID)
	if err != nil || access.Token != token {
		t.Fatal("saved credential unavailable", err)
	}
	public, _ := json.Marshal(loadRemoteMachines())
	if bytes.Contains(public, []byte(token)) {
		t.Fatal("token in machine JSON")
	}
}
func TestNodeBridgeEncryptedSelectedCredential(t *testing.T) {
	testHome(t)
	key, _ := randBytes(32)
	setMemDEK(key)
	defer clearMemDEK()
	cfg := ReadConfig()
	cfg["MEM_ENCRYPTED"] = "1"
	WriteConfig(cfg)
	m, err := savePairedMachine(RemoteMachine{Name: "vault fixture", Host: "127.0.0.1"}, &engineNode{URL: "http://127.0.0.1:2511", WebKey: "only-machine-token", APIKey: "never-pass-inference", Role: "engine-node", NodeID: "vault-fixture", Modules: []string{"engine", "harness"}, Handshake: 1})
	if err != nil {
		t.Fatal(err)
	}
	env, cleanup, err := nodeBridgeLaunchEnv([]string{"node-bridge", m.ID, "hermes", "/tmp"})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	for _, entry := range env {
		pair := strings.SplitN(entry, "=", 2)
		t.Setenv(pair[0], pair[1])
	}
	file := os.Getenv(nodeBridgeStateEnv)
	raw, err := os.ReadFile(file)
	if err != nil || bytes.Contains(raw, []byte("only-machine-token")) {
		t.Fatal("launch state not encrypted", err)
	}
	clearMemDEK()
	access, err := readNodeBridgeAccess(m.ID)
	if err != nil || access.Token != "only-machine-token" {
		t.Fatal("bridge cannot use selected encrypted credential", err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("launch state not consumed")
	}
	if os.Getenv(nodeBridgeKeyEnv) != "" || os.Getenv(nodeBridgeStateEnv) != "" {
		t.Fatal("launch environment retained")
	}
}

// Exercise main-side integration even when the sandbox cannot open sockets.
func TestNodeMachineIntegrationWithoutSockets(t *testing.T) {
	_, mux, token := nodeHarnessFixture(t, 8)
	previous := nodeClient
	nodeClient = &http.Client{Transport: envRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "Bearer "+token || strings.Contains(r.URL.String(), token) {
			t.Error("machine credential not confined to header")
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w.Result(), nil
	}), CheckRedirect: previous.CheckRedirect}
	defer func() { nodeClient = previous }()
	m, err := savePairedMachine(RemoteMachine{Name: "node fixture", Host: "127.0.0.1"}, &engineNode{URL: "http://127.0.0.1:2511", WebKey: token, Role: "engine-node", NodeID: "fixture", Modules: []string{"engine", "harness"}, Handshake: 1})
	if err != nil {
		t.Fatal(err)
	}
	m, err = checkRemoteMachine(context.Background(), m)
	if err != nil || m.OS != "Linux" || m.Home == "" || m.CheckedAt == 0 {
		t.Fatal(m, err)
	}
	if err := saveRemoteMachine(m, nil); err != nil {
		t.Fatal(err)
	}
	saved := loadRemoteMachines()[0]
	if saved.NodeID != "fixture" || saved.Handshake != 1 || !hasNodeModule(saved.Modules, "harness") {
		t.Fatal(saved)
	}
	for _, i := range agentInstallations() {
		if i.Machine == m.ID && i.Harness == "hermes" && (!i.Ready || !i.Installed || i.Managed || i.Enabled) {
			t.Fatal(i)
		}
	}
	agent, err := remoteAgent(m, "hermes", "")
	if err != nil || agent.Args[0] != "node-bridge" || agent.RemoteHome != m.Home || !agent.Remote {
		t.Fatal(agent, err)
	}
	if err := saveRemoteMachine(m, []acpAgent{agent}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { registeredRuntimes.remove(agent.ID) })
	w := httptest.NewRecorder()
	handleAgentInstallations(w, httptest.NewRequest("GET", "/api/agents/installations", nil))
	var installations struct {
		Installations []agentInstallation `json:"installations"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &installations) != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	found := false
	for _, i := range installations.Installations {
		if i.RuntimeID == agent.ID {
			found = true
			if !i.Enabled || !i.Managed {
				t.Fatal(i)
			}
		}
	}
	if !found {
		t.Fatal("paired installation absent")
	}
	if err := setRemoteHarness(m.ID, "hermes", false); err != nil {
		t.Fatal(err)
	}
	if _, exists := registeredRuntimes.lookup(agent.ID); exists {
		t.Fatal("disabled runtime retained")
	}
	root := t.TempDir()
	dir := filepath.Join(root, "workspace")
	got, err := prepareWorkspace(context.Background(), resources.Workspace{Target: m.ID, Path: dir}, true)
	if err != nil || got != dir {
		t.Fatal(got, err)
	}
	if _, err := prepareWorkspace(context.Background(), resources.Workspace{Target: m.ID, Path: dir}, false); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	handleMachineFolders(w, httptest.NewRequest("GET", "/api/machines/folders?machine="+m.ID+"&path="+url.QueryEscape(root), nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), dir) {
		t.Fatal(w.Code, w.Body.String())
	}
	if _, _, err := terminalCommand(m.ID, dir, ""); err == nil || !strings.Contains(err.Error(), "terminals need SSH for now") {
		t.Fatal(err)
	}
	// The full node mux supplies auth and explicit disabled errors to the main.
	if err := putBool(bkState, nodeHarnessDisabledKey, true); err != nil {
		t.Fatal(err)
	}
	if _, err := checkRemoteMachine(context.Background(), m); err == nil || err.Error() != nodeHarnessDisabled {
		t.Fatal(err)
	}
	if _, err := nodeFolders(context.Background(), m, root); err == nil || err.Error() != nodeHarnessDisabled {
		t.Fatal(err)
	}
}

func TestNodePairDisabledHarnessModules(t *testing.T) {
	mux, _ := pairTestNode(t)
	if err := putBool(bkState, nodeHarnessDisabledKey, true); err != nil {
		t.Fatal(err)
	}
	code := pairTestCode(t, false, time.Now())
	w := pairTestExchange(mux, code, "paired-main", "127.0.0.1:1", false)
	var exchange nodePairExchange
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &exchange) != nil || exchange.Node.Handshake != 1 || hasNodeModule(exchange.Node.Modules, "harness") || !hasEngineModule(exchange.Node.Modules) {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestNodeHarnessLimitWithoutSockets(t *testing.T) {
	server, mux, token := nodeHarnessFixture(t, 1)
	server.slots <- struct{}{}
	defer func() { <-server.slots }()
	w := nodeHarnessGet(mux, "/api/node/harness/acp?harness=hermes&cwd="+url.QueryEscape(t.TempDir()), token)
	if w.Code != 429 || !strings.Contains(w.Body.String(), "node agent process limit reached") {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestNodeHarnessInitOptOut(t *testing.T) {
	if runtime.GOOS != "linux" || os.Geteuid() == 0 {
		t.Skip("node CLI requires a non-root Linux user")
	}
	home := testHome(t)
	t.Setenv("LOOM_SERVICE", os.Getenv("LOOM_SERVICE"))
	t.Setenv("LOOM_UI_SERVICE", os.Getenv("LOOM_UI_SERVICE"))
	for _, args := range [][]string{{"init", "--home", home, "--no-harness"}, {"init", "--home", home}, {"init", "--home", home, "--no-harness=false"}} {
		if err := cmdNode(args); err != nil {
			t.Fatal(err)
		}
		enabled := strings.HasSuffix(args[len(args)-1], "=false")
		if nodeHarnessEnabled() != enabled {
			t.Fatal("module choice not retained", args)
		}
	}
}
