package loom

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/coder/websocket"
	"github.com/lucas-lepajollec/loom/internal/loom/web"
)

// Terminals: real shells opened from Loom, on this machine or on a connected
// remote machine (through SSH or its paired node), in a chosen folder,
// optionally running a command (an agent's own CLI to check or repair something, an app to run).
// A terminal outlives the browser tab: its process keeps running and the last
// output is replayed when a tab reattaches. The browser connects with a
// one-time ticket obtained through the authenticated API, because a WebSocket
// cannot carry the control key header.

const (
	maxTerminals     = 16
	terminalScroll   = 256 << 10 // output kept for reattaching tabs
	terminalTicketTT = 30 * time.Second
)

// TerminalInfo is what the browser sees of a terminal.
type TerminalInfo struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Target    string `json:"target"` // "local" or a remote machine id
	Dir       string `json:"dir"`
	Command   string `json:"command,omitempty"`
	CreatedAt int64  `json:"created_at"`
	Running   bool   `json:"running"`
	ExitCode  int    `json:"exit_code"`
}

type Terminal struct {
	TerminalInfo

	proc    termProcess
	mu      sync.Mutex
	scroll  []byte
	clients map[chan []byte]struct{}
	done    chan struct{}
}

// termProcess abstracts a local PTY, SSH PTY or paired-node WebSocket.
type termProcess interface {
	Read([]byte) (int, error)
	Write([]byte) (int, error)
	Resize(cols, rows uint16) error
	Close() error
	Wait() (int, error)
}

var terminals = struct {
	sync.Mutex
	byID    map[string]*Terminal
	tickets map[string]ticket
}{byID: map[string]*Terminal{}, tickets: map[string]ticket{}}

type ticket struct {
	term  string
	exp   time.Time
	grant controlGrant
}

func randomID(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// terminalCommand builds the argv for a terminal. Locally: the user's shell,
// or the shell running the command. Remote: ssh -tt to the machine, cd to the
// folder, then the login shell or the command.
func terminalCommand(target, dir, command string) ([]string, string, error) {
	command = strings.TrimSpace(command)
	if strings.ContainsAny(command, "\x00") || len(command) > 2000 {
		return nil, "", errors.New("invalid command")
	}
	if target == "" || target == "local" {
		if dir == "" {
			dir, _ = os.UserHomeDir()
		}
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			return nil, "", errors.New("directory not found on this machine")
		}
		if runtime.GOOS == "windows" {
			return windowsTerminalShellCommand(windowsTerminalShell(exec.LookPath, os.Getenv("ComSpec")), command), dir, nil
		}
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/sh"
		}
		if command == "" {
			return []string{shell, "-l"}, dir, nil
		}
		return []string{shell, "-lc", command}, dir, nil
	}
	var m RemoteMachine
	found := false
	for _, x := range loadRemoteMachines() {
		if x.ID == target {
			m, found = x, true
		}
	}
	if !found {
		return nil, "", errors.New("unknown machine")
	}
	if m.User == "" {
		return nil, "", errors.New("terminals need SSH for now")
	}
	if dir == "" {
		dir = m.Home
	}
	if dir != "" {
		clean, err := remoteWorkdir(dir)
		if err != nil {
			return nil, "", err
		}
		dir = clean
	}
	key, _, err := loomSSHKey()
	if err != nil {
		return nil, "", err
	}
	args := remoteTerminalArgs(m, key, dir, command)
	home, _ := os.UserHomeDir()
	return append([]string{"ssh"}, args...), home, nil
}

func remoteTerminalArgs(m RemoteMachine, key, dir, command string) []string {
	// Without a remote command, sshd starts exactly one login shell. Starting
	// another -l shell through the SSH command shell runs startup banners twice.
	args := sshArgs(m, key)
	if command != "" {
		script := command
		if dir != "" {
			script = "cd " + shellQuote(dir) + " && " + script
		}
		args = sshArgs(m, key, remotePathPreamble+script)
	}
	if lifecycleOS(&m) == "windows" {
		args = windowsRemoteSSHArgs(m, key, windowsRemoteTerminalScript(dir, command))
		// A terminal must keep stdin interactive, unlike lifecycle commands.
		for i, a := range args {
			if a == "-NonInteractive" {
				args = append(args[:i], args[i+1:]...)
				break
			}
		}
		if command == "" {
			for i, a := range args {
				if a == "-EncodedCommand" {
					args = append(args[:i], append([]string{"-NoExit"}, args[i:]...)...)
					break
				}
			}
		}
	}
	// Interactive: force a remote PTY; sshArgs starts with -T (no PTY).
	for i, a := range args {
		if a == "-T" {
			args[i] = "-tt"
		}
	}
	return args
}

var terminalOpenMu sync.Mutex

func openTerminal(target, dir, command, title string) (*Terminal, error) {
	command = strings.TrimSpace(command)
	terminalOpenMu.Lock()
	defer terminalOpenMu.Unlock()
	terminals.Lock()
	running := 0
	for _, t := range terminals.byID {
		if t.snapshot().Running {
			running++
		}
	}
	terminals.Unlock()
	if running >= maxTerminals {
		return nil, errors.New("maximum 16 open terminals: close one")
	}
	proc, input, err := startTerminalProcess(target, dir, command)
	if err != nil {
		return nil, err
	}
	if title == "" {
		title = filepath.Base(dir)
		if command != "" {
			title = strings.Trim(strings.Fields(command)[0], ";&|")
		} else if target != "" && target != "local" {
			title = target
		}
		if title == "" || title == "." || title == "/" {
			title = "Terminal"
		}
	}
	if target == "" {
		target = "local"
	}
	t := &Terminal{TerminalInfo: TerminalInfo{ID: randomID(8), Title: title, Target: target, Dir: dir, Command: command, CreatedAt: time.Now().UnixMilli(), Running: true},
		proc: proc, clients: map[chan []byte]struct{}{}, done: make(chan struct{})}
	terminals.Lock()
	terminals.byID[t.ID] = t
	terminals.Unlock()
	go t.pump()
	// Queue the selected directory as the first input to the single remote
	// login shell. BatchMode disables password prompts; no user startup files
	// are changed. Commands already receive their directory in SSH argv.
	if input != "" {
		if _, err := proc.Write([]byte(input)); err != nil {
			_ = proc.Close()
			return nil, err
		}
	}
	return t, nil
}

// Select the transport before building SSH argv or injecting login-shell input.
func startTerminalProcess(target, dir, command string) (termProcess, string, error) {
	if target != "" && target != "local" {
		m, err := workspaceMachine(target)
		if err != nil {
			return nil, "", err
		}
		if usesNodeTerminal(m) {
			proc, err := startNodeTerminal(m, dir, command)
			return proc, "", err
		}
	}
	if !ptySupported {
		return nil, "", errors.New("terminals are not available on this system yet")
	}
	input, err := remoteTerminalInitialInput(target, dir, command)
	if err != nil {
		return nil, "", err
	}
	argv, cwd, err := terminalCommand(target, dir, command)
	if err != nil {
		return nil, "", err
	}
	if _, err := exec.LookPath(argv[0]); err != nil {
		return nil, "", errors.New(argv[0] + " not found")
	}
	proc, err := startPTY(argv, cwd, terminalEnvironment(target))
	return proc, input, err
}

func terminalEnvironment(target string) []string {
	env := []string{"TERM=xterm-256color", "COLORTERM=truecolor"}
	if target == "" || target == "local" {
		env = append(env, "PATH="+lifecycleLocalPath())
	}
	return env
}

func validTerminalSize(cols, rows uint16) bool {
	return cols > 0 && rows > 0 && cols <= 1000 && rows <= 500
}

// Both browser and node sockets use the same resize control framing.
func terminalResizeMessage(typ websocket.MessageType, data []byte) (uint16, uint16, bool) {
	if typ == websocket.MessageText && len(data) > 0 && data[0] == '{' {
		var msg struct {
			Resize []uint16 `json:"resize"`
		}
		if json.Unmarshal(data, &msg) == nil && len(msg.Resize) == 2 {
			return msg.Resize[0], msg.Resize[1], true
		}
	}
	return 0, 0, false
}

// pump copies the process output to the scrollback and to every attached tab.
func (t *Terminal) pump() {
	buf := make([]byte, 32<<10)
	for {
		n, err := t.proc.Read(buf)
		if n > 0 {
			chunk := append([]byte(nil), buf[:n]...)
			t.mu.Lock()
			t.scroll = append(t.scroll, chunk...)
			if len(t.scroll) > terminalScroll {
				t.scroll = t.scroll[len(t.scroll)-terminalScroll:]
			}
			for c := range t.clients {
				select {
				case c <- chunk:
				default: // a slow tab drops output rather than blocking the shell
				}
			}
			t.mu.Unlock()
		}
		if err != nil {
			break
		}
	}
	code, _ := t.proc.Wait()
	t.mu.Lock()
	t.Running, t.ExitCode = false, code
	for c := range t.clients {
		close(c)
		delete(t.clients, c)
	}
	t.mu.Unlock()
	close(t.done)
}

func (t *Terminal) attach() (chan []byte, []byte) {
	t.mu.Lock()
	defer t.mu.Unlock()
	c := make(chan []byte, 256)
	if t.Running {
		t.clients[c] = struct{}{}
	} else {
		close(c)
	}
	return c, append([]byte(nil), t.scroll...)
}

func (t *Terminal) detach(c chan []byte) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.clients[c]; ok {
		delete(t.clients, c)
		close(c)
	}
}

func (t *Terminal) snapshot() TerminalInfo {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.TerminalInfo
}

func terminalByID(id string) *Terminal {
	terminals.Lock()
	defer terminals.Unlock()
	return terminals.byID[id]
}

func remoteTerminalInitialInput(target, dir, command string) (string, error) {
	if target == "" || target == "local" || strings.TrimSpace(command) != "" || dir == "" {
		return "", nil
	}
	if strings.IndexFunc(dir, unicode.IsControl) >= 0 {
		return "", errors.New("control characters are not allowed in an interactive working directory")
	}
	machine, err := workspaceMachine(target)
	if err != nil {
		return "", err
	}
	if lifecycleOS(&machine) == "windows" {
		return "", nil
	}
	clean, err := remoteWorkdir(dir)
	if err != nil {
		return "", err
	}
	if clean == machine.Home {
		return "", nil
	}
	return "cd " + shellQuote(clean) + "\r", nil
}

type terminalRequest struct {
	signature string
	terminal  *Terminal
	at        time.Time
}

var terminalRequests = struct {
	sync.Mutex
	items map[string]terminalRequest
}{items: map[string]terminalRequest{}}

// GET: list. POST {target, dir, command, title, request_id?}: open.
func handleTerminals(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		terminals.Lock()
		list := []TerminalInfo{}
		for _, t := range terminals.byID {
			list = append(list, t.snapshot())
		}
		terminals.Unlock()
		sort.Slice(list, func(i, j int) bool { return list[i].CreatedAt < list[j].CreatedAt })
		sendJSON(w, 200, map[string]any{"ok": true, "terminals": list, "supported": terminalsSupported()})
		return
	}
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		RequestID string `json:"request_id"`
		Target    string `json:"target"`
		Dir       string `json:"dir"`
		Command   string `json:"command"`
		Title     string `json:"title"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	if !ptySupported {
		m, _ := workspaceMachine(req.Target)
		if !usesNodeTerminal(m) {
			sendJSON(w, 501, map[string]any{"ok": false, "error": "terminals are not available on this system yet"})
			return
		}
	}
	if len([]rune(req.Title)) > 60 {
		req.Title = string([]rune(req.Title)[:60])
	}
	// Retries of one user action must not start another process. Entries are
	// bounded and expire; closing a terminal does not replay its command.
	if req.RequestID != "" {
		if !nativeSessionIDPattern.MatchString(req.RequestID) {
			sendJSON(w, 400, map[string]any{"ok": false, "error": "invalid terminal request ID"})
			return
		}
		terminalRequests.Lock()
		defer terminalRequests.Unlock()
		for id, entry := range terminalRequests.items {
			if time.Since(entry.at) > 2*time.Minute {
				delete(terminalRequests.items, id)
			}
		}
		signature, _ := json.Marshal([]string{req.Target, req.Dir, req.Command, req.Title})
		if prior, found := terminalRequests.items[req.RequestID]; found {
			if prior.signature != string(signature) {
				sendJSON(w, 409, map[string]any{"ok": false, "error": "terminal request ID reused with another command"})
				return
			}
			if terminalByID(prior.terminal.ID) == nil {
				sendJSON(w, 409, map[string]any{"ok": false, "error": "this terminal has been closed"})
				return
			}
			sendJSON(w, 200, map[string]any{"ok": true, "terminal": prior.terminal.snapshot()})
			return
		}
		if len(terminalRequests.items) >= 128 {
			sendJSON(w, 429, map[string]any{"ok": false, "error": "too many terminal requests"})
			return
		}
	}
	t, err := openTerminal(req.Target, strings.TrimSpace(req.Dir), req.Command, strings.TrimSpace(req.Title))
	if err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if req.RequestID != "" {
		signature, _ := json.Marshal([]string{req.Target, req.Dir, req.Command, req.Title})
		terminalRequests.items[req.RequestID] = terminalRequest{string(signature), t, time.Now()}
	}
	sendJSON(w, 200, map[string]any{"ok": true, "terminal": t.snapshot()})
}

// POST {id}: close (kills the process) and forget it.
func handleTerminalClose(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	t := terminalByID(req.ID)
	if t == nil {
		sendJSON(w, 404, map[string]any{"ok": false, "error": "terminal not found"})
		return
	}
	_ = t.proc.Close()
	select {
	case <-t.done:
	case <-time.After(3 * time.Second):
	}
	terminals.Lock()
	delete(terminals.byID, req.ID)
	terminals.Unlock()
	sendJSON(w, 200, map[string]any{"ok": true})
}

// POST {id}: a one-time ticket to open the WebSocket (valid 30 s).
func handleTerminalTicket(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	if terminalByID(req.ID) == nil {
		sendJSON(w, 404, map[string]any{"ok": false, "error": "terminal not found"})
		return
	}
	grant, err := controlOwner(r)
	if err != nil {
		webAuthUnavailable(w)
		return
	}
	tk := randomID(24)
	terminals.Lock()
	now := time.Now()
	for k, v := range terminals.tickets {
		if now.After(v.exp) {
			delete(terminals.tickets, k)
		}
	}
	if len(terminals.tickets) >= 128 {
		terminals.Unlock()
		sendJSON(w, 429, map[string]any{"ok": false, "error": "too many terminal tickets"})
		return
	}
	terminals.tickets[hashWebKey(tk)] = ticket{term: req.ID, exp: now.Add(terminalTicketTT), grant: grant}
	terminals.Unlock()
	sendJSON(w, 200, map[string]any{"ok": true, "ticket": tk})
}

func takeTerminalTicket(tk string) (ticket, bool) {
	terminals.Lock()
	v, ok := terminals.tickets[hashWebKey(tk)]
	delete(terminals.tickets, hashWebKey(tk))
	terminals.Unlock()
	return v, ok && time.Now().Before(v.exp) && v.grant.valid()
}
func useTicket(tk string) string {
	v, ok := takeTerminalTicket(tk)
	if !ok {
		return ""
	}
	return v.term
}

// GET /api/terminals/ws?ticket=: the terminal stream. Text frames from the
// browser are keystrokes; a JSON {"resize":[cols,rows]} frame resizes.
// Binary frames to the browser are output.
func handleTerminalWS(w http.ResponseWriter, r *http.Request) {
	tk, valid := takeTerminalTicket(r.URL.Query().Get("ticket"))
	t := terminalByID(tk.term)
	if !valid || t == nil {
		http.Error(w, "invalid or expired ticket", http.StatusUnauthorized)
		return
	}
	// Same-origin only (the default check), against cross-site hijacking.
	conn, err := web.AcceptWebSocket(w, r, 1<<20)
	if err != nil {
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	defer conn.CloseNow()
	// Keep established sockets revocable, including quiet terminals.
	recheck := time.NewTicker(time.Second)
	defer recheck.Stop()
	out, scroll := t.attach()
	defer t.detach(out)
	if len(scroll) > 0 {
		_ = conn.Write(ctx, websocket.MessageBinary, scroll)
	}
	go func() {
		defer cancel()
		for {
			typ, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			if !tk.grant.valid() {
				return
			}
			if cols, rows, ok := terminalResizeMessage(typ, data); ok {
				_ = t.proc.Resize(cols, rows)
				continue
			}
			if _, err := t.proc.Write(data); err != nil {
				return
			}
		}
	}()
	for {
		select {
		case <-recheck.C:
			if !tk.grant.valid() {
				conn.Close(websocket.StatusPolicyViolation, "access revoked")
				return
			}
		case chunk, ok := <-out:
			if !ok {
				_ = conn.Write(ctx, websocket.MessageText, []byte(`{"exit":true}`))
				conn.Close(websocket.StatusNormalClosure, "")
				return
			}
			if err := conn.Write(ctx, websocket.MessageBinary, chunk); err != nil {
				return
			}
		case <-ctx.Done():
			conn.Close(websocket.StatusNormalClosure, "")
			return
		}
	}
}

// shutdownTerminals stops every terminal process when Loom exits.
func shutdownTerminals() {
	terminals.Lock()
	list := []*Terminal{}
	for _, t := range terminals.byID {
		list = append(list, t)
	}
	terminals.Unlock()
	for _, t := range list {
		_ = t.proc.Close()
	}
}
