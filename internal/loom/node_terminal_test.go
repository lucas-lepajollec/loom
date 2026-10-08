package loom

import (
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
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/lucas-lepajollec/loom/internal/loom/web"
)

func nodeTerminalFixture(t *testing.T, limit int) (*nodeTerminalServer, http.Handler, string) {
	t.Helper()
	testHome(t)
	server := newNodeTerminalServer(limit)
	t.Cleanup(server.stop)
	token := "terminal-machine-fixture"
	return server, newEngineWorkerMuxModules(token, newNodeHarnessServer(8), server), token
}

func requireTerminalPTY(t *testing.T) {
	t.Helper()
	if !ptySupported {
		t.Skip("native pseudo-terminal unavailable")
	}
	argv, dir, err := terminalCommand("local", t.TempDir(), "exit")
	if err != nil {
		t.Fatal(err)
	}
	proc, err := startPTY(argv, dir, nil)
	if err != nil {
		t.Skipf("native pseudo-terminal unavailable: %v", err)
	}
	_ = proc.Close()
	_, _ = proc.Wait()
}

func TestNodeTerminalAuthValidationLimitAndDisabled(t *testing.T) {
	server, mux, token := nodeTerminalFixture(t, 1)
	path := "/api/node/terminal/ws"
	for _, credential := range []string{"", "inference-key"} {
		if w := nodeHarnessGet(mux, path, credential); w.Code != 401 {
			t.Fatal(w.Code)
		}
	}
	if w := nodeHarnessGet(mux, path+"?token="+token, ""); w.Code != 401 {
		t.Fatal("URL token accepted")
	}
	r := httptest.NewRequest("GET", path, nil)
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Origin", "http://localhost")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("browser accepted", w.Code)
	}
	r = httptest.NewRequest("POST", path, nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 405 {
		t.Fatal("method accepted", w.Code)
	}
	if ptySupported {
		dir := t.TempDir()
		file := filepath.Join(dir, "file")
		if err := os.WriteFile(file, nil, 0600); err != nil {
			t.Fatal(err)
		}
		for _, q := range []url.Values{
			{"cwd": {"relative"}}, {"cwd": {file}}, {"cwd": {filepath.Join(dir, "missing")}},
			{"cwd": {dir + "\n"}}, {"cwd": {dir}, "command": {"a\x00b"}},
			{"cwd": {dir}, "command": {strings.Repeat("x", 2001)}},
			{"cwd": {dir}, "cols": {"0"}}, {"cwd": {dir}, "cols": {"1001"}},
			{"cwd": {dir}, "rows": {"501"}}, {"cwd": {dir}, "rows": {"-1"}},
			{"cwd": {dir}, "cols": {"no"}},
		} {
			if w := nodeHarnessGet(mux, path+"?"+q.Encode(), token); w.Code != 400 {
				t.Fatal(q, w.Code, w.Body.String())
			}
		}
		server.slots <- struct{}{}
		w = nodeHarnessGet(mux, path+"?cwd="+url.QueryEscape(dir), token)
		<-server.slots
		if w.Code != 429 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	descriptor, err := describeNode()
	if err != nil || !hasNodeModule(descriptor.Modules, "terminal") {
		t.Fatal(descriptor, err)
	}
	if err := putBool(bkState, nodeTerminalDisabledKey, true); err != nil {
		t.Fatal(err)
	}
	descriptor, _ = describeNode()
	if hasNodeModule(descriptor.Modules, "terminal") {
		t.Fatal("disabled module advertised")
	}
	if w := nodeHarnessGet(mux, path, token); w.Code != 409 || !strings.Contains(w.Body.String(), nodeTerminalDisabled) {
		t.Fatal(w.Code, w.Body.String())
	}
	// Terminal and harness opt-outs are independent.
	if !nodeHarnessEnabled() {
		t.Fatal("terminal opt-out disabled harness")
	}
}

func TestNodeTerminalPTYWebSocket(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("node mode is Linux-only")
	}
	server, mux, token := nodeTerminalFixture(t, 1)
	requireTerminalPTY(t)
	t.Setenv("SHELL", "/bin/sh")
	cwd := t.TempDir()
	t.Setenv("HOME", cwd)
	pidFile := filepath.Join(cwd, "pid")
	childFile := filepath.Join(cwd, "child")
	command := "echo $$ > pid; sleep 300 & echo $! > child; stty -echo; printf 'READY '; stty size; pwd; while IFS= read -r line; do case $line in size) stty size;; quit) exit 7;; *) printf '%s\\n' \"$line\";; esac; done"
	address := nodeTerminalPipeServer(t, mux)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	endpoint := "ws" + strings.TrimPrefix(address, "http") + "/api/node/terminal/ws?" + url.Values{"cwd": {cwd}, "command": {command}, "cols": {"90"}, "rows": {"27"}}.Encode()
	opts := &websocket.DialOptions{HTTPClient: nodeClient, HTTPHeader: http.Header{"Authorization": {"Bearer " + token}}}
	conn, _, err := websocket.Dial(ctx, endpoint, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	readUntil := func(want string) string {
		t.Helper()
		var output string
		for !strings.Contains(output, want) {
			typ, data, err := conn.Read(ctx)
			if err != nil || typ != websocket.MessageBinary {
				t.Fatal(output, typ, err)
			}
			output += string(data)
		}
		return output
	}
	output := readUntil(cwd)
	if !strings.Contains(output, "27 90") {
		t.Fatal("initial dimensions", output)
	}
	if _, response, err := websocket.Dial(ctx, endpoint, opts); err == nil || response == nil || response.StatusCode != 429 {
		t.Fatal("limit", response, err)
	}
	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"resize":[120,40]}`)); err != nil {
		t.Fatal(err)
	}
	if err := conn.Write(ctx, websocket.MessageBinary, []byte("size\n")); err != nil {
		t.Fatal(err)
	}
	readUntil("40 120")
	// JSON typed as input is binary, and must remain input.
	if err := conn.Write(ctx, websocket.MessageBinary, []byte("{\"resize\":[1,1]}\n")); err != nil {
		t.Fatal(err)
	}
	readUntil(`{"resize":[1,1]}`)
	pid, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	child, err := os.ReadFile(childFile)
	if err != nil {
		t.Fatal(err)
	}
	conn.CloseNow()
	waitNodeTerminalSlot(t, server)
	if err := exec.Command("kill", "-0", strings.TrimSpace(string(pid))).Run(); err == nil {
		t.Fatal("shell survived close")
	}
	stat, err := os.ReadFile("/proc/" + strings.TrimSpace(string(child)) + "/stat")
	if err == nil && !strings.Contains(string(stat), ") Z ") {
		t.Fatal("descendant survived close")
	}
	conn, _, err = websocket.Dial(ctx, endpoint, opts)
	if err != nil {
		t.Fatal("slot not reusable", err)
	}
	defer conn.CloseNow()
	readUntil(cwd)
	if err := conn.Write(ctx, websocket.MessageText, []byte("quit\n")); err != nil {
		t.Fatal(err)
	}
	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if typ == websocket.MessageText {
			var exit struct {
				Exit bool `json:"exit"`
				Code int  `json:"exit_code"`
			}
			if json.Unmarshal(data, &exit) != nil || !exit.Exit || exit.Code != 7 {
				t.Fatal(string(data))
			}
			break
		}
	}
	waitNodeTerminalSlot(t, server)
	// Empty cwd and command select the user's home and interactive login shell.
	endpoint = "ws" + strings.TrimPrefix(address, "http") + "/api/node/terminal/ws"
	conn, _, err = websocket.Dial(ctx, endpoint, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	if err := conn.Write(ctx, websocket.MessageText, []byte("pwd\n")); err != nil {
		t.Fatal(err)
	}
	readUntil(cwd)
	server.stop()
	for {
		if _, _, err := conn.Read(ctx); err != nil {
			break
		}
	}
	waitNodeTerminalSlot(t, server)
}

func waitNodeTerminalSlot(t *testing.T, s *nodeTerminalServer) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for len(s.slots) > 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if len(s.slots) != 0 {
		t.Fatal("node terminal slot leaked")
	}
}

// Exercise the real HTTP upgrade and WebSocket protocol without binding a
// socket, so main-side transport coverage also runs in restricted sandboxes.
type terminalPipeListener struct {
	connections chan net.Conn
	done        chan struct{}
	once        sync.Once
}

func (l *terminalPipeListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.connections:
		return conn, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}
func (l *terminalPipeListener) Close() error {
	l.once.Do(func() { close(l.done) })
	return nil
}
func (l *terminalPipeListener) Addr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 2511}
}
func nodeTerminalPipeServer(t *testing.T, handler http.Handler) string {
	t.Helper()
	listener := &terminalPipeListener{connections: make(chan net.Conn), done: make(chan struct{})}
	server := &http.Server{Handler: handler}
	go func() { _ = server.Serve(listener) }()
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		client, server := net.Pipe()
		select {
		case listener.connections <- server:
			return client, nil
		case <-ctx.Done():
			client.Close()
			server.Close()
			return nil, ctx.Err()
		case <-listener.done:
			client.Close()
			server.Close()
			return nil, net.ErrClosed
		}
	}}
	previous := nodeClient
	nodeClient = &http.Client{Transport: transport, CheckRedirect: previous.CheckRedirect}
	t.Cleanup(func() { nodeClient = previous; transport.CloseIdleConnections(); _ = server.Close() })
	return "http://127.0.0.1:2511"
}

func TestNodeTerminalBackendRegistryAndTickets(t *testing.T) {
	testHome(t)
	token := "selected-terminal-token"
	received := make(chan []byte, 4)
	closed := make(chan struct{})
	address := nodeTerminalPipeServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token || strings.Contains(r.URL.String(), token) {
			t.Error("credential escaped header")
		}
		if r.URL.Path != "/api/node/terminal/ws" || r.URL.Query().Get("cwd") != "/remote/app" || r.URL.Query().Get("command") != "cat" || r.URL.Query().Get("cols") != "100" {
			t.Error("wrong launch", r.URL)
		}
		conn, err := web.AcceptWebSocket(w, r, 1<<20)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		defer close(closed)
		for {
			typ, data, err := conn.Read(r.Context())
			if err != nil {
				return
			}
			received <- append([]byte(nil), data...)
			if typ == websocket.MessageBinary {
				if err := conn.Write(r.Context(), websocket.MessageBinary, data); err != nil {
					return
				}
			}
		}
	}))
	m, err := savePairedMachine(RemoteMachine{Name: "terminal fixture", Host: "127.0.0.1"}, &engineNode{URL: address, WebKey: token, Role: "engine-node", NodeID: "terminal-fixture", Modules: []string{"engine", "terminal"}, Handshake: 1})
	if err != nil {
		t.Fatal(err)
	}
	// Exercise the API rather than bypassing the existing registry.
	body := `{"target":"` + m.ID + `","dir":"/remote/app","command":"cat","request_id":"` + randomID(8) + `"}`
	call := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/api/terminals", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		handleTerminals(w, r)
		return w
	}
	w := call()
	var opened struct {
		Terminal TerminalInfo `json:"terminal"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &opened) != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	term := terminalByID(opened.Terminal.ID)
	t.Cleanup(func() {
		_ = term.proc.Close()
		<-term.done
		terminals.Lock()
		delete(terminals.byID, term.ID)
		terminals.Unlock()
		terminalRequests.Lock()
		for id, entry := range terminalRequests.items {
			if entry.terminal == term {
				delete(terminalRequests.items, id)
			}
		}
		terminalRequests.Unlock()
	})
	retry := call()
	if retry.Code != 200 || !bytes.Contains(retry.Body.Bytes(), []byte(term.ID)) {
		t.Fatal("retry started another terminal")
	}
	out, _ := term.attach()
	payload := []byte(`{"resize":[1,1]}`)
	if _, err := term.proc.Write(payload); err != nil {
		t.Fatal(err)
	}
	select {
	case b := <-out:
		if !bytes.Equal(b, payload) {
			t.Fatal(string(b))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("echo absent")
	}
	term.detach(out)
	again, scroll := term.attach()
	defer term.detach(again)
	if !bytes.Equal(scroll, payload) || !term.snapshot().Running {
		t.Fatal("reattach changed registry process", string(scroll))
	}
	if err := term.proc.Resize(120, 40); err != nil {
		t.Fatal(err)
	}
	if err := term.proc.Resize(0, 40); err == nil {
		t.Fatal("bad size accepted")
	}
	if got := <-received; !bytes.Equal(got, payload) {
		t.Fatal(string(got))
	}
	if got := <-received; string(got) != `{"resize":[120,40]}` {
		t.Fatal(string(got))
	}
	ticketBody := `{"id":"` + term.ID + `"}`
	w = httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/terminals/ticket", strings.NewReader(ticketBody))
	r.Header.Set("Content-Type", "application/json")
	handleTerminalTicket(w, r)
	var ticketReply struct {
		Ticket string `json:"ticket"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &ticketReply) != nil || useTicket(ticketReply.Ticket) != term.ID || useTicket(ticketReply.Ticket) != "" {
		t.Fatal("node ticket behavior", w.Code, w.Body.String())
	}
	_ = term.proc.Close()
	select {
	case <-term.done:
	case <-time.After(5 * time.Second):
		t.Fatal("node close did not exit registry")
	}
	if term.snapshot().Running || term.snapshot().ExitCode != -1 {
		t.Fatal(term.snapshot())
	}
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("node socket not closed")
	}
}

func TestNodeTerminalBackendExitCode(t *testing.T) {
	testHome(t)
	address := nodeTerminalPipeServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := web.AcceptWebSocket(w, r, 1<<20)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		_ = conn.Write(r.Context(), websocket.MessageBinary, []byte("final output"))
		_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"exit":true,"exit_code":7}`))
	}))
	m, err := savePairedMachine(RemoteMachine{Name: "exit fixture", Host: "127.0.0.1"}, &engineNode{URL: address, WebKey: "exit-fixture", Role: "engine-node", NodeID: "exit", Modules: []string{"engine", "terminal"}, Handshake: 1})
	if err != nil {
		t.Fatal(err)
	}
	proc, err := startNodeTerminal(m, "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer proc.Close()
	output, err := io.ReadAll(proc)
	if err != nil || string(output) != "final output" {
		t.Fatal(string(output), err)
	}
	if code, err := proc.Wait(); code != 7 || err != nil {
		t.Fatal(code, err)
	}
}

func TestNodeTerminalMachineCapabilities(t *testing.T) {
	testHome(t)
	machines := []RemoteMachine{
		{ID: "paired", NodeID: "one", Modules: []string{"engine", "terminal"}},
		{ID: "no-terminal", NodeID: "two", Modules: []string{"engine"}},
		{ID: "ssh", User: "alice", Host: "example.invalid", Port: 22},
	}
	if err := putStoreJSON(bkState, remoteMachinesState, machines); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	handleRemoteMachines(w, httptest.NewRequest("GET", "/api/machines", nil))
	var response struct {
		Machines []struct {
			RemoteMachine
			Capabilities []string `json:"capabilities"`
		} `json:"machines"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &response) != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, m := range response.Machines {
		if hasNodeModule(m.Capabilities, "terminals") != (m.ID != "no-terminal") {
			t.Fatal(m)
		}
		if m.ID == "paired" && !hasNodeModule(m.Modules, "terminal") {
			t.Fatal("module absent")
		}
	}
	if _, err := openTerminal("no-terminal", "", "", ""); err == nil || !strings.Contains(err.Error(), "terminals need SSH") {
		t.Fatal(err)
	}
	if _, err := nodeMachineModuleAccess(machines[1], "terminal"); err == nil || err.Error() != nodeTerminalDisabled {
		t.Fatal(err)
	}
}

func TestNodeTerminalInitOptOut(t *testing.T) {
	if runtime.GOOS != "linux" || os.Geteuid() == 0 {
		t.Skip("node CLI requires a non-root Linux user")
	}
	home := testHome(t)
	t.Setenv("LOOM_SERVICE", os.Getenv("LOOM_SERVICE"))
	t.Setenv("LOOM_UI_SERVICE", os.Getenv("LOOM_UI_SERVICE"))
	for _, args := range [][]string{{"init", "--home", home, "--no-terminal"}, {"init", "--home", home}, {"init", "--home", home, "--no-terminal=false"}} {
		if err := cmdNode(args); err != nil {
			t.Fatal(err)
		}
		enabled := strings.HasSuffix(args[len(args)-1], "=false")
		if nodeTerminalEnabled() != enabled {
			t.Fatal("module choice not retained", args)
		}
	}
}
