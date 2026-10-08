package loom

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/lucas-lepajollec/loom/internal/loom/web"
)

const nodeHarnessDisabled = "node harness module is disabled"
const nodeHarnessDisabledKey = "node_harness_disabled"

func nodeHarnessEnabled() bool { return !getBool(bkState, nodeHarnessDisabledKey) }
func hasNodeModule(modules []string, id string) bool {
	for _, module := range modules {
		if module == id {
			return true
		}
	}
	return false
}
func usesNodeHarness(m RemoteMachine) bool {
	return m.NodeID != "" && (hasNodeModule(m.Modules, "harness") || m.User == "")
}

func probeNodeHarness(ctx context.Context) (RemoteMachine, error) {
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-s")
	cmd.Stdin = strings.NewReader(remoteProbeScript)
	acpProcessGroup(cmd)
	cmd.Cancel = func() error { acpKillProcessGroup(cmd); return nil }
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	if err != nil {
		return RemoteMachine{}, errors.New("node tool probe failed")
	}
	return parseRemoteProbe(string(out))
}

// One server owns its slots. Hooks let tests exercise the actual transport with
// deterministic inventories and agents, without launching installed harnesses.
type nodeHarnessServer struct {
	mu        sync.Mutex
	closing   bool
	processes map[*exec.Cmd]bool
	slots     chan struct{}
	probe     func(context.Context) (RemoteMachine, error)
	command   func(RemoteMachine, string, string) (*exec.Cmd, error)
}

func newNodeHarnessServer(limit int) *nodeHarnessServer {
	return &nodeHarnessServer{processes: map[*exec.Cmd]bool{}, slots: make(chan struct{}, limit), probe: probeNodeHarness, command: nodeHarnessCommand}
}
func nodeHarnessCommand(m RemoteMachine, harness, cwd string) (*exec.Cmd, error) {
	for _, d := range remoteHarnessDefs {
		if d.ID != harness {
			continue
		}
		have := map[string]bool{}
		for _, tool := range m.Tools {
			have[tool.ID] = true
		}
		for _, need := range d.Needs {
			if !have[need] {
				return nil, fmt.Errorf("node harness requires %s", need)
			}
		}
		launch := remoteLaunch(d.ID, d.Launch)
		if len(launch) == 0 {
			return nil, errors.New("unknown launcher")
		}
		parts := []string{}
		for _, arg := range launch {
			parts = append(parts, shellQuote(arg))
		}
		script := "exec " + strings.Join(parts, " ")
		if dirs := remoteToolDirs(m); len(dirs) > 0 {
			script = "PATH=" + shellQuote(strings.Join(dirs, ":")) + ":\"$PATH\" " + script
		}
		cmd := exec.Command("sh", "-c", script)
		cmd.Dir = cwd
		return cmd, nil
	}
	return nil, errors.New("harness not supported remotely")
}
func nodeHarnessRequest(w http.ResponseWriter, r *http.Request) bool {
	if !workspaceMethod(w, r, http.MethodGet) {
		return false
	}
	if !nodeHarnessEnabled() {
		sendJSON(w, 409, map[string]any{"ok": false, "error": nodeHarnessDisabled})
		return false
	}
	return true
}
func (s *nodeHarnessServer) inventory(w http.ResponseWriter, r *http.Request) {
	if !nodeHarnessRequest(w, r) {
		return
	}
	m, err := s.probe(r.Context())
	if err != nil {
		sendJSON(w, 503, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "os": m.OS, "home": m.Home, "hostname": m.Hostname, "tools": m.Tools})
}
func nodeHarnessDirectory(dir string) (string, error) {
	if dir == "" {
		dir, _ = os.UserHomeDir()
	}
	if !filepath.IsAbs(dir) || strings.ContainsAny(dir, "\x00\r\n") {
		return "", errors.New("absolute directory required")
	}
	dir = filepath.Clean(dir)
	st, err := os.Stat(dir)
	if err != nil || !st.IsDir() {
		return "", errors.New("directory not found on the node")
	}
	return dir, nil
}
func knownRemoteHarness(id string) bool {
	for _, d := range remoteHarnessDefs {
		if d.ID == id {
			return true
		}
	}
	return false
}
func (s *nodeHarnessServer) acp(w http.ResponseWriter, r *http.Request) {
	if !nodeHarnessRequest(w, r) {
		return
	}
	harness := r.URL.Query().Get("harness")
	if !knownRemoteHarness(harness) {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "harness not supported remotely"})
		return
	}
	cwd, err := nodeHarnessDirectory(r.URL.Query().Get("cwd"))
	if err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		sendJSON(w, 429, map[string]any{"ok": false, "error": "node agent process limit reached"})
		return
	}
	m, err := s.probe(r.Context())
	if err != nil {
		sendJSON(w, 503, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	cmd, err := s.command(m, harness, cwd)
	if err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	acpProcessGroup(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		sendJSON(w, 500, map[string]any{"ok": false, "error": "node agent pipe unavailable"})
		return
	}
	defer stdin.Close()
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		sendJSON(w, 500, map[string]any{"ok": false, "error": "node agent pipe unavailable"})
		return
	}
	defer stdout.Close()
	// Drain all stderr, logging only a bounded prefix without unbounded lines.
	cmd.Stderr = &nodeHarnessLog{remaining: 32 << 10, harness: harness}
	conn, err := web.AcceptWebSocket(w, r, acpMaxFrame)
	if err != nil {
		return
	}
	defer conn.CloseNow()
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		conn.Close(websocket.StatusGoingAway, "node stopping")
		return
	}
	err = cmd.Start()
	if err == nil {
		s.processes[cmd] = true
	}
	s.mu.Unlock()
	if err != nil {
		conn.Close(websocket.StatusInternalError, "node agent launch failed")
		return
	}
	defer func() {
		acpKillProcessGroup(cmd)
		s.mu.Lock()
		delete(s.processes, cmd)
		s.mu.Unlock()
	}()
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	done := make(chan error, 2)
	stream := websocket.NetConn(ctx, conn, websocket.MessageBinary)
	conn.SetReadLimit(acpMaxFrame)
	go func() { _, err := io.Copy(stdin, stream); done <- err }()
	go func() {
		buf := make([]byte, 32<<10)
		for {
			n, err := stdout.Read(buf)
			if n > 0 {
				if e := conn.Write(ctx, websocket.MessageBinary, buf[:n]); e != nil {
					done <- e
					return
				}
			}
			if err != nil {
				done <- err
				return
			}
		}
	}()
	<-done
	cancel()
	conn.CloseNow()
	acpKillProcessGroup(cmd)
	stdin.Close()
	stdout.Close()
	// Reap the owned child before releasing its slot.
	_ = cmd.Wait()
	<-done
}

type nodeHarnessLog struct {
	remaining int
	harness   string
}

func (l *nodeHarnessLog) Write(p []byte) (int, error) {
	n := len(p)
	if l.remaining > 0 {
		chunk := p
		if len(chunk) > l.remaining {
			chunk = chunk[:l.remaining]
		}
		log.Printf("[node harness %s] %q", l.harness, chunk)
		l.remaining -= len(chunk)
	}
	return n, nil
}

func handleNodeFolders(w http.ResponseWriter, r *http.Request) {
	if !nodeHarnessRequest(w, r) {
		return
	}
	dir, err := nodeHarnessDirectory(r.URL.Query().Get("path"))
	if err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "node directory cannot be read"})
		return
	}
	folders := []string{}
	for _, entry := range entries {
		if entry.IsDir() {
			folders = append(folders, filepath.Join(dir, entry.Name()))
		}
	}
	sendJSON(w, 200, map[string]any{"ok": true, "path": dir, "parent": filepath.Dir(dir), "folders": folders})
}

// Workspace creation remains explicit, separate from the read-only picker.
func handleNodeWorkspace(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	if !nodeHarnessEnabled() {
		sendJSON(w, 409, map[string]any{"ok": false, "error": nodeHarnessDisabled})
		return
	}
	var req struct {
		Path string `json:"path"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	if !filepath.IsAbs(req.Path) || strings.ContainsAny(req.Path, "\x00\r\n") {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "absolute directory required"})
		return
	}
	if err := os.MkdirAll(req.Path, 0755); err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "node workspace cannot be created"})
		return
	}
	dir, err := nodeHarnessDirectory(req.Path)
	if err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "path": dir})
}

// HTTP shutdown does not own hijacked WebSockets; explicitly stop only this
// server's child groups when a foreground node exits as well as under systemd.
func (s *nodeHarnessServer) stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closing = true
	for cmd := range s.processes {
		acpKillProcessGroup(cmd)
	}
}
