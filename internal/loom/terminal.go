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
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// Terminals: real shells opened from Loom, on this machine or on a connected
// remote machine (through ssh), in a chosen folder, optionally running a
// command (an agent's own CLI to check or repair something, an app to run).
// A terminal outlives the browser tab: its process keeps running and the last
// output is replayed when a tab reattaches. The browser connects with a
// one-time ticket obtained through the authenticated API, because a WebSocket
// cannot carry the control key header.

const (
	maxTerminals     = 16
	terminalScroll   = 256 << 10 // output kept for reattaching tabs
	terminalTicketTT = 30 * time.Second
)

type Terminal struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Target    string `json:"target"` // "local" or a remote machine id
	Dir       string `json:"dir"`
	Command   string `json:"command,omitempty"`
	CreatedAt int64  `json:"created_at"`
	Running   bool   `json:"running"`
	ExitCode  int    `json:"exit_code"`

	proc    termProcess
	mu      sync.Mutex
	scroll  []byte
	clients map[chan []byte]struct{}
	done    chan struct{}
}

// termProcess is the platform pseudo-terminal (terminal_pty_*.go).
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
	term string
	exp  time.Time
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
		return nil, "", errors.New("commande invalide")
	}
	if target == "" || target == "local" {
		if dir == "" {
			dir, _ = os.UserHomeDir()
		}
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			return nil, "", errors.New("dossier introuvable sur cette machine")
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
		return nil, "", errors.New("machine inconnue")
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
	script := "exec \"${SHELL:-/bin/sh}\" -l"
	if command != "" {
		script = "exec \"${SHELL:-/bin/sh}\" -lc " + shellQuote(command)
	}
	if dir != "" {
		script = "cd " + shellQuote(dir) + " && " + script
	}
	args := sshArgs(m, key, script)
	// Interactive: force a remote PTY; sshArgs starts with -T (no PTY).
	for i, a := range args {
		if a == "-T" {
			args[i] = "-tt"
		}
	}
	home, _ := os.UserHomeDir()
	return append([]string{"ssh"}, args...), home, nil
}

func openTerminal(target, dir, command, title string) (*Terminal, error) {
	terminals.Lock()
	running := 0
	for _, t := range terminals.byID {
		if t.Running {
			running++
		}
	}
	terminals.Unlock()
	if running >= maxTerminals {
		return nil, errors.New("16 terminaux ouverts au maximum : ferme-en un")
	}
	argv, cwd, err := terminalCommand(target, dir, command)
	if err != nil {
		return nil, err
	}
	if _, err := exec.LookPath(argv[0]); err != nil {
		return nil, errors.New(argv[0] + " introuvable")
	}
	proc, err := startPTY(argv, cwd, []string{"TERM=xterm-256color", "COLORTERM=truecolor"})
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
	t := &Terminal{ID: randomID(8), Title: title, Target: target, Dir: dir, Command: command, CreatedAt: time.Now().UnixMilli(),
		Running: true, proc: proc, clients: map[chan []byte]struct{}{}, done: make(chan struct{})}
	terminals.Lock()
	terminals.byID[t.ID] = t
	terminals.Unlock()
	go t.pump()
	return t, nil
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

func (t *Terminal) snapshot() Terminal {
	t.mu.Lock()
	defer t.mu.Unlock()
	return Terminal{ID: t.ID, Title: t.Title, Target: t.Target, Dir: t.Dir, Command: t.Command, CreatedAt: t.CreatedAt, Running: t.Running, ExitCode: t.ExitCode}
}

func terminalByID(id string) *Terminal {
	terminals.Lock()
	defer terminals.Unlock()
	return terminals.byID[id]
}

// GET: list. POST {target, dir, command, title}: open.
func handleTerminals(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		terminals.Lock()
		list := []Terminal{}
		for _, t := range terminals.byID {
			list = append(list, t.snapshot())
		}
		terminals.Unlock()
		sort.Slice(list, func(i, j int) bool { return list[i].CreatedAt < list[j].CreatedAt })
		sendJSON(w, 200, map[string]any{"ok": true, "terminals": list, "supported": ptySupported})
		return
	}
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	if !ptySupported {
		sendJSON(w, 501, map[string]any{"ok": false, "error": "les terminaux ne sont pas encore disponibles sur ce système"})
		return
	}
	var req struct {
		Target  string `json:"target"`
		Dir     string `json:"dir"`
		Command string `json:"command"`
		Title   string `json:"title"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	if len([]rune(req.Title)) > 60 {
		req.Title = string([]rune(req.Title)[:60])
	}
	t, err := openTerminal(req.Target, strings.TrimSpace(req.Dir), req.Command, strings.TrimSpace(req.Title))
	if err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
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
		sendJSON(w, 404, map[string]any{"ok": false, "error": "terminal introuvable"})
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
		sendJSON(w, 404, map[string]any{"ok": false, "error": "terminal introuvable"})
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
	terminals.tickets[tk] = ticket{term: req.ID, exp: now.Add(terminalTicketTT)}
	terminals.Unlock()
	sendJSON(w, 200, map[string]any{"ok": true, "ticket": tk})
}

func useTicket(tk string) string {
	terminals.Lock()
	defer terminals.Unlock()
	v, ok := terminals.tickets[tk]
	delete(terminals.tickets, tk)
	if !ok || time.Now().After(v.exp) {
		return ""
	}
	return v.term
}

// GET /api/terminals/ws?ticket=: the terminal stream. Text frames from the
// browser are keystrokes; a JSON {"resize":[cols,rows]} frame resizes.
// Binary frames to the browser are output.
func handleTerminalWS(w http.ResponseWriter, r *http.Request) {
	id := useTicket(r.URL.Query().Get("ticket"))
	t := terminalByID(id)
	if t == nil {
		http.Error(w, "ticket invalide ou expiré", http.StatusUnauthorized)
		return
	}
	// Same-origin only (the default check), against cross-site hijacking.
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	conn.SetReadLimit(1 << 20)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
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
			if typ == websocket.MessageText && len(data) > 0 && data[0] == '{' {
				var msg struct {
					Resize []uint16 `json:"resize"`
				}
				if json.Unmarshal(data, &msg) == nil && len(msg.Resize) == 2 {
					_ = t.proc.Resize(msg.Resize[0], msg.Resize[1])
					continue
				}
			}
			if _, err := t.proc.Write(data); err != nil {
				return
			}
		}
	}()
	for {
		select {
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
