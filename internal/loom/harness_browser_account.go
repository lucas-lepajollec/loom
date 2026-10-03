package loom

import (
	"context"
	"errors"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

func browserAccountSupported(a acpAgent) bool {
	switch usageHarnessID(a) {
	case "codex", "claude-code", "antigravity":
		return true
	}
	return false
}

var nativeAuthorizationCode = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.~/#+-]{3,999}$`)
var nativeBrowserURL = regexp.MustCompile(`https://[^\x00-\x20\x7f<>"']+`)
var nativeANSI = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)

// Accept only the native providers' authorize pages, never arbitrary links from
// terminal output. OAuth URLs and returned codes live only in an expiring job.
func accountAuthorizationURL(output, runtime string) string {
	for _, raw := range nativeBrowserURL.FindAllString(output, -1) {
		u, err := url.Parse(raw)
		if err != nil || u.User != nil || u.Port() != "" || u.Scheme != "https" || u.Fragment != "" {
			continue
		}
		allowed := runtime == "claude-code" && (u.Hostname() == "claude.ai" || u.Hostname() == "console.anthropic.com") && u.Path == "/oauth/authorize"
		allowed = allowed || runtime == "antigravity" && u.Hostname() == "accounts.google.com" && u.Path == "/o/oauth2/auth"
		if allowed && u.Query().Get("state") != "" && u.Query().Get("client_id") != "" {
			return raw
		}
	}
	return ""
}

// Native auth runs in a private pseudo-terminal, separate from saved user
// terminals. Loom forwards only the authorize link and code prompt. It never
// submits a model prompt, /login, /logout, permissions or onboarding answers.
func runBrowserAccount(ctx context.Context, cmd *exec.Cmd, runtime string, job *harnessAccountJob) error {
	proc, err := startPTY(cmd.Args, cmd.Dir, cmd.Env)
	if err != nil {
		return errors.New("native browser login unavailable; try native login")
	}
	var closeOnce sync.Once
	closeNative := func() { closeOnce.Do(func() { _ = proc.Close() }) }
	stop := context.AfterFunc(ctx, closeNative)
	defer func() { stop(); closeNative(); _, _ = proc.Wait() }()
	readerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	chunks := make(chan []byte)
	go func() {
		defer close(chunks)
		b := make([]byte, 4096)
		for {
			n, e := proc.Read(b)
			if n > 0 {
				c := append([]byte(nil), b[:n]...)
				select {
				case chunks <- c:
				case <-readerCtx.Done():
					return
				}
			}
			if e != nil {
				return
			}
		}
	}()
	buffer := ""
	total := 0
	submitted := false
	googleSelected := false
	initial := time.NewTimer(45 * time.Second)
	defer initial.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-initial.C:
			return errors.New("native login did not provide a browser link; use native login or verify an existing account")
		case code := <-job.codes:
			if submitted {
				return errors.New("native authorization code was already submitted")
			}
			submitted = true
			if _, err := proc.Write([]byte(code + "\r")); err != nil {
				return errors.New("native code submission failed")
			}
		case chunk, ok := <-chunks:
			if !ok {
				return errors.New("native login ended before completion; try native login")
			}
			total += len(chunk)
			if total > 1<<20 {
				return errors.New("native login output limit reached")
			}
			// Reply to standard terminal discovery queries without exposing a terminal.
			replies := [][2]string{{"\x1b[6n", "\x1b[1;1R"}, {"\x1b[c", "\x1b[?1;2c"}, {"\x1b[>c", "\x1b[>0;10;1c"}, {"\x1b]11;", "\x1b]11;rgb:0000/0000/0000\x1b\\"}}
			for _, r := range replies {
				if strings.Contains(string(chunk), r[0]) {
					_, _ = proc.Write([]byte(r[1]))
				}
			}
			buffer += string(chunk)
			if len(buffer) > 65536 {
				buffer = buffer[len(buffer)-65536:]
			}
			clean := nativeANSI.ReplaceAllString(buffer, "")
			lower := strings.ToLower(clean)
			if runtime == "antigravity" && !googleSelected && strings.Contains(clean, "Select login method:") && strings.Contains(clean, "> 1. Google OAuth") {
				// The user explicitly chose Google in Loom; select only this known native
				// sign-in choice. Never answer project, permission or license prompts.
				googleSelected = true
				_, _ = proc.Write([]byte("\r"))
			}
			authURL := accountAuthorizationURL(clean, runtime)
			if authURL != "" {
				initial.Stop()
				job.mu.Lock()
				if job.State == "starting" || job.State == "waiting" {
					job.State, job.URL = "waiting", authURL
					job.Input = !job.submitted && !submitted && (strings.Contains(lower, "paste code") || strings.Contains(lower, "authorization code") || strings.Contains(lower, "enter the code"))
				}
				job.mu.Unlock()
			}
			if runtime == "claude-code" && (strings.Contains(lower, "login successful") || strings.Contains(lower, "successfully logged in")) || runtime == "antigravity" && strings.Contains(lower, "authentication successful!") {
				return nil
			}
		}
	}
}

// Sign-in happens in a new remote temporary directory, not a user's project.
// The shell only launches the fixed native command and cleans its own directory.
func nativeAccountSSHCommand(machine RemoteMachine, key string, argv []string) []string {
	parts := make([]string, len(argv))
	for i, arg := range argv {
		parts[i] = shellQuote(arg)
	}
	script := remotePathPreamble + `loom_account_dir=$(mktemp -d) || exit 1
trap 'rm -rf "$loom_account_dir"' EXIT
trap 'exit 130' HUP INT TERM
cd "$loom_account_dir" || exit 1
export BROWSER=true TERM=xterm-256color
` + strings.Join(parts, " ")
	return append([]string{"ssh"}, sshArgs(machine, key, "sh", "-c", shellQuote(script))...)
}
