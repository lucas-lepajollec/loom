package loom

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"sync"
	"time"
)

// Login is an explicit action, separate from catalog consent. The native CLI
// owns OAuth and stores its credentials; Loom retains only a short-lived device
// code in memory. No prompts, account tokens or raw CLI errors are forwarded.
type harnessAccountState struct {
	ID      string `json:"id"`
	Runtime string `json:"runtime"`
	State   string `json:"state"`
	URL     string `json:"url,omitempty"`
	Code    string `json:"code,omitempty"`
	Error   string `json:"error,omitempty"`
	Expires int64  `json:"expires_at"`
	Input   bool   `json:"input_required,omitempty"`
}

type harnessAccountJob struct {
	mu sync.Mutex
	harnessAccountState
	cancel context.CancelFunc
	codes  chan string
}

var harnessAccounts = struct {
	sync.Mutex
	jobs map[string]*harnessAccountJob
}{jobs: map[string]*harnessAccountJob{}}

func (j *harnessAccountJob) snapshot() harnessAccountState {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.harnessAccountState
}

func (j *harnessAccountJob) finish(state, message string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.State == "starting" || j.State == "waiting" {
		j.State, j.Error, j.URL, j.Code = state, message, "", ""
		j.Input = false
	}
}

func harnessAccountCommand(ctx context.Context, agent acpAgent) (*exec.Cmd, func(), error) {
	target, id := harnessLifecycleRuntimeTarget(agent.ID)
	_, machine, _, err := resolveHarnessTarget(target, id)
	if err != nil {
		return nil, nil, err
	}
	var argv []string
	switch usageHarnessID(agent) {
	case "codex":
		argv = []string{"codex", "app-server"}
	case "claude-code":
		argv = []string{"claude", "auth", "login", "--claudeai"}
	case "antigravity":
		argv = []string{"agy"}
	default:
		return nil, nil, errors.New("browser account login unavailable for this harness")
	}
	if machine == nil {
		argv, err = harnessNativeArgv(argv)
	} else {
		var key string
		key, _, err = loomSSHKey()
		if err == nil {
			if lifecycleOS(machine) == "windows" {
				argv, err = buildHarnessLifecycleCommand(machine, key, argv)
			} else {
				argv = nativeAccountSSHCommand(*machine, key, argv)
			}
		}
	}
	if err != nil {
		return nil, nil, errors.New("Native CLI unavailable on this machine")
	}
	dir, err := os.MkdirTemp("", "loom-account-")
	if err != nil {
		return nil, nil, errors.New("temporary login directory unavailable")
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = dir
	if machine == nil {
		cmd.Env = append(os.Environ(), "PATH="+lifecycleLocalPath())
	}
	if usageHarnessID(agent) != "codex" {
		if machine != nil {
			// Allocate the remote TTY for a native sign-in TUI, not an ACP session.
			for i := range cmd.Args {
				if cmd.Args[i] == "-T" {
					cmd.Args[i] = "-tt"
					break
				}
			}
		} else {
			cmd.Env = append(cmd.Env, "BROWSER=true", "SSH_CONNECTION=loom-native-browser-login", "TERM=xterm-256color")
		}
	}
	cmd.Stderr = io.Discard
	acpProcessGroup(cmd)
	cmd.Cancel = func() error { acpKillProcessGroup(cmd); return nil }
	cmd.WaitDelay = time.Second
	return cmd, func() { _ = os.RemoveAll(dir) }, nil
}

var harnessDeviceCode = regexp.MustCompile(`^[A-Za-z0-9-]{4,64}$`)

func runCodexAccount(ctx context.Context, cmd *exec.Cmd, publish func(string, string)) error {
	in, err := cmd.StdinPipe()
	if err != nil {
		return errors.New("native login unavailable")
	}
	out, err := cmd.StdoutPipe()
	if err != nil || cmd.Start() != nil {
		return errors.New("native login unavailable")
	}
	stop := context.AfterFunc(ctx, func() { _ = out.Close() })
	defer stop()
	defer func() { _ = in.Close(); acpKillProcessGroup(cmd); _ = cmd.Wait() }()
	scanner := bufio.NewScanner(io.LimitReader(out, 2<<20))
	scanner.Buffer(make([]byte, 4096), 512<<10)
	enc := json.NewEncoder(in)
	if enc.Encode(map[string]any{"id": 1, "method": "initialize", "params": map[string]any{"clientInfo": map[string]string{"name": "loom", "title": "Loom account login", "version": Version}}}) != nil {
		return errors.New("native login initialization failed")
	}
	if _, err := readRPCResult(scanner, 1); err != nil {
		return errors.New("native login initialization failed")
	}
	if enc.Encode(map[string]any{"method": "initialized"}) != nil || enc.Encode(map[string]any{"id": 2, "method": "account/login/start", "params": map[string]string{"type": "chatgptDeviceCode"}}) != nil {
		return errors.New("native login unavailable")
	}
	data, err := readRPCResult(scanner, 2)
	if err != nil {
		return errors.New("device login unavailable: update Codex or use its native terminal login")
	}
	var result struct{ Type, LoginID, VerificationURL, UserCode string }
	if json.Unmarshal(data, &result) != nil || result.Type != "chatgptDeviceCode" || result.LoginID == "" || result.VerificationURL != "https://auth.openai.com/codex/device" || !harnessDeviceCode.MatchString(result.UserCode) {
		return errors.New("unsupported native device login response")
	}
	publish(result.VerificationURL, result.UserCode)
	for scanner.Scan() {
		var msg struct {
			Method string `json:"method"`
			Params struct {
				LoginID string `json:"loginId"`
				Success bool   `json:"success"`
			} `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &msg) != nil {
			return errors.New("native login protocol failed")
		}
		if msg.Method == "account/login/completed" && msg.Params.LoginID == result.LoginID {
			if msg.Params.Success {
				return nil
			}
			return errors.New("native sign-in failed or was declined; check device login access in your ChatGPT account")
		}
	}
	return errors.New("native login ended before completion")
}

// GET reads an existing job. POST starts/cancels a fixed native login command.
// All routes use the normal authenticated, same-origin Loom API boundary.
func handleHarnessAccount(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		workspaceMethod(w, r, http.MethodPost)
		return
	}
	agent, ok := acpAgentFor(r.PathValue("id"))
	if !ok || !browserAccountSupported(agent) || workspaceTarget(agent) == "" {
		sendJSON(w, 501, map[string]any{"ok": false, "error": "use this harness's native account login"})
		return
	}
	jobID, cancelJob := r.URL.Query().Get("job"), false
	suppliedCode := ""
	if r.Method == http.MethodPost {
		var req struct {
			Job     string `json:"job"`
			Cancel  bool   `json:"cancel"`
			Consent bool   `json:"consent"`
			Code    string `json:"code"`
		}
		if !workspaceDecode(w, r, &req) {
			return
		}
		jobID, cancelJob, suppliedCode = req.Job, req.Cancel, req.Code
		if jobID == "" && (!req.Consent || cancelJob || suppliedCode != "") {
			sendJSON(w, 400, map[string]any{"ok": false, "error": "confirm native account sign-in"})
			return
		}
	}
	// Stopping an already-owned login remains possible after the Loom vault
	// locks. Authenticated cancellation exposes no code or account credentials.
	if !cancelJob && !usageVaultAccess(w) {
		return
	}
	harnessAccounts.Lock()
	defer harnessAccounts.Unlock()
	for id, job := range harnessAccounts.jobs {
		if job.snapshot().Expires <= time.Now().UnixMilli() {
			job.cancel()
			delete(harnessAccounts.jobs, id)
		}
	}
	if jobID != "" {
		job := harnessAccounts.jobs[jobID]
		if job == nil || job.snapshot().Runtime != agent.ID {
			sendJSON(w, 404, map[string]any{"ok": false, "error": "login expired or not found"})
			return
		}
		if suppliedCode != "" {
			job.mu.Lock()
			if cancelJob || !job.Input || job.State != "waiting" || !nativeAuthorizationCode.MatchString(suppliedCode) {
				job.mu.Unlock()
				sendJSON(w, 400, map[string]any{"ok": false, "error": "authorization code not expected or invalid"})
				return
			}
			job.Input = false
			job.codes <- suppliedCode
			job.mu.Unlock()
		}
		if cancelJob {
			job.finish("cancelled", "")
			job.cancel()
			delete(harnessAccounts.jobs, jobID)
		}
		sendJSON(w, 200, map[string]any{"ok": true, "login": job.snapshot()})
		return
	}
	if r.Method != http.MethodPost {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "login job required"})
		return
	}
	for _, job := range harnessAccounts.jobs {
		s := job.snapshot()
		if s.Runtime == agent.ID && (s.State == "starting" || s.State == "waiting") {
			sendJSON(w, 409, map[string]any{"ok": false, "error": "an account login is already in progress"})
			return
		}
	}
	if len(harnessAccounts.jobs) >= 16 {
		sendJSON(w, 429, map[string]any{"ok": false, "error": "too many login attempts; wait for expiry"})
		return
	}
	var nonce [24]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		sendJSON(w, 500, map[string]any{"ok": false, "error": "login unavailable"})
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	cmd, cleanup, err := harnessAccountCommand(ctx, agent)
	if err != nil {
		cancel()
		sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	job := &harnessAccountJob{harnessAccountState: harnessAccountState{ID: hex.EncodeToString(nonce[:]), Runtime: agent.ID, State: "starting", Expires: time.Now().Add(10 * time.Minute).UnixMilli()}, cancel: cancel, codes: make(chan string, 1)}
	harnessAccounts.jobs[job.ID] = job
	go func() {
		defer cancel()
		defer cleanup()
		var err error
		if usageHarnessID(agent) == "codex" {
			err = runCodexAccount(ctx, cmd, func(url, code string) {
				job.mu.Lock()
				defer job.mu.Unlock()
				if job.State == "starting" {
					job.State, job.URL, job.Code = "waiting", url, code
				}
			})
		} else {
			err = runBrowserAccount(ctx, cmd, usageHarnessID(agent), job)
		}
		if ctx.Err() != nil {
			job.finish("expired", "native login expired or cancelled")
		} else if err != nil {
			job.finish("error", err.Error())
		} else {
			job.finish("completed", "")
		}
	}()
	sendJSON(w, 202, map[string]any{"ok": true, "login": job.snapshot()})
}
