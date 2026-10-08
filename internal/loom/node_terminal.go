package loom

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"sync"

	"github.com/coder/websocket"
	"github.com/lucas-lepajollec/loom/internal/loom/web"
)

const nodeTerminalDisabled = "node terminal module is disabled"
const nodeTerminalDisabledKey = "node_terminal_disabled"
const defaultNodeTerminalLimit = 8

func nodeTerminalEnabled() bool { return !getBool(bkState, nodeTerminalDisabledKey) }
func usesNodeTerminal(m RemoteMachine) bool {
	return m.NodeID != "" && hasNodeModule(m.Modules, "terminal")
}
func machineTerminalsSupported(m RemoteMachine) bool { return usesNodeTerminal(m) || m.User != "" }
func terminalsSupported() bool {
	if ptySupported {
		return true
	}
	for _, m := range loadRemoteMachines() {
		if usesNodeTerminal(m) {
			return true
		}
	}
	return false
}

// Hijacked sockets outlive HTTP shutdown, so this owner tracks only its PTYs.
type nodeTerminalServer struct {
	mu        sync.Mutex
	closing   bool
	processes map[termProcess]bool
	slots     chan struct{}
	start     func([]string, string, []string, uint16, uint16) (termProcess, error)
}

func newNodeTerminalServer(limit int) *nodeTerminalServer {
	return &nodeTerminalServer{processes: map[termProcess]bool{}, slots: make(chan struct{}, limit), start: startPTYSize}
}
func nodeTerminalSize(value string, fallback uint16) (uint16, error) {
	if value == "" {
		return fallback, nil
	}
	n, err := strconv.ParseUint(value, 10, 16)
	if err != nil {
		return 0, errors.New("invalid size")
	}
	return uint16(n), nil
}
func (s *nodeTerminalServer) ws(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodGet) {
		return
	}
	if !nodeTerminalEnabled() {
		sendJSON(w, 409, map[string]any{"ok": false, "error": nodeTerminalDisabled})
		return
	}
	if !ptySupported {
		sendJSON(w, 501, map[string]any{"ok": false, "error": "node PTY unavailable"})
		return
	}
	cwd, err := nodeHarnessDirectory(r.URL.Query().Get("cwd"))
	if err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	argv, cwd, err := terminalCommand("local", cwd, r.URL.Query().Get("command"))
	if err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	cols, colErr := nodeTerminalSize(r.URL.Query().Get("cols"), 100)
	rows, rowErr := nodeTerminalSize(r.URL.Query().Get("rows"), 30)
	if colErr != nil || rowErr != nil || !validTerminalSize(cols, rows) {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "invalid size"})
		return
	}
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		sendJSON(w, 429, map[string]any{"ok": false, "error": "node terminal process limit reached"})
		return
	}
	conn, err := web.AcceptWebSocket(w, r, 1<<20)
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
	proc, err := s.start(argv, cwd, terminalEnvironment("local"), cols, rows)
	if err == nil {
		s.processes[proc] = true
	}
	s.mu.Unlock()
	if err != nil {
		conn.Close(websocket.StatusInternalError, "node terminal launch failed")
		return
	}
	defer func() {
		_ = proc.Close()
		s.mu.Lock()
		delete(s.processes, proc)
		s.mu.Unlock()
	}()
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	inputDone, outputDone := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(inputDone)
		for {
			typ, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			if cols, rows, ok := terminalResizeMessage(typ, data); ok {
				_ = proc.Resize(cols, rows)
				continue
			}
			if _, err := proc.Write(data); err != nil {
				return
			}
		}
	}()
	go func() {
		defer close(outputDone)
		buf := make([]byte, 32<<10)
		for {
			n, err := proc.Read(buf)
			if n > 0 {
				if err := conn.Write(ctx, websocket.MessageBinary, buf[:n]); err != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	outputEnded := false
	select {
	case <-inputDone:
	case <-outputDone:
		outputEnded = true
	case <-ctx.Done():
	}
	// Kill/reap before releasing the slot, and unblock both transport directions.
	_ = proc.Close()
	code, _ := proc.Wait()
	if outputEnded {
		data, _ := json.Marshal(map[string]any{"exit": true, "exit_code": code})
		_ = conn.Write(ctx, websocket.MessageText, data)
	}
	cancel()
	conn.CloseNow()
	<-inputDone
	<-outputDone
}
func (s *nodeTerminalServer) stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closing = true
	for proc := range s.processes {
		_ = proc.Close()
	}
}
