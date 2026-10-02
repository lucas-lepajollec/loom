package loom

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Remote machines: another computer reachable over SSH where harnesses run
// (for example Hermes in a container). Loom connects with its own key, reads
// what is installed there with a small script, and registers each chosen
// harness as a remote ACP agent (custom-<machine>-<harness>). Nothing is
// installed during linking besides Loom's public key, added by the user.
// Explicit lifecycle actions can subsequently install or update harnesses.

type RemoteTool struct {
	ID      string `json:"id"`
	Path    string `json:"path"`
	Version string `json:"version,omitempty"`
}

type RemoteMachine struct {
	ID        string       `json:"id"`
	Name      string       `json:"name"`
	Host      string       `json:"host"`
	User      string       `json:"user"`
	Port      int          `json:"port"`
	Hostname  string       `json:"hostname,omitempty"`
	Home      string       `json:"home,omitempty"`
	OS        string       `json:"os,omitempty"`
	Tools     []RemoteTool `json:"tools,omitempty"`
	Harnesses []string     `json:"harnesses,omitempty"` // registered harness ids on this machine
	Folders   []string     `json:"folders,omitempty"`   // favourite work folders there (suggestions)
	CheckedAt int64        `json:"checked_at,omitempty"`
}

const remoteMachinesState = "remote_machines"

// remoteHarnessDefs: harnesses Loom knows how to launch in ACP mode on another
// machine. `needs` are the remote commands that must exist; `launch` is run
// there (a leading "npx" reuses the same pinned adapters as local agents).
var remoteHarnessDefs = []struct {
	ID, Name, Logo string
	Needs          []string
	Launch         []string
}{
	{"hermes", "Hermes", "hermes", []string{"hermes"}, []string{"hermes", "acp"}},
	{"claude-code", "Claude Code", "claudecode", []string{"claude", "npx"}, nil},
	{"codex", "Codex", "codex", []string{"codex", "npx"}, nil},
	{"pi", "Pi", "pi", []string{"pi", "npx"}, nil},
	{"gemini", "Gemini", "gemini", []string{"gemini"}, []string{"gemini", "--acp"}},
	{"opencode", "OpenCode", "opencode", []string{"opencode"}, []string{"opencode", "acp"}},
}

// remoteLaunch returns the ACP command for a harness id, reusing the local
// definition (pinned adapter versions) when Loom has one.
func remoteLaunch(id string, fixed []string) []string {
	if fixed != nil {
		return fixed
	}
	for _, a := range builtinACPAgents() {
		if a.ID == id {
			return append([]string{a.Command}, a.Args...)
		}
	}
	return nil
}

// remoteProbeScript prints one line "LOOM-MACHINE {json}" describing the
// machine. POSIX sh, no dependency; the user can run it by hand and Loom runs
// it over SSH. Common user install folders are added to PATH because a
// non-interactive SSH shell does not read the user's profile.
const remotePathPreamble = `P="$HOME/.local/bin:$HOME/.npm-global/bin:$HOME/.bun/bin:$HOME/.cargo/bin:/usr/local/bin:/opt/homebrew/bin:$PATH"
for d in "$HOME"/.nvm/versions/node/*/bin; do [ -d "$d" ] && P="$d:$P"; done
export PATH="$P"
`

const remoteProbeScript = remotePathPreamble + `T=""; command -v timeout >/dev/null 2>&1 && T="timeout 5"
j=""
for t in hermes claude codex pi gemini opencode agy npm npx node; do
  p=$(command -v "$t" 2>/dev/null) || continue
  v=$($T "$p" --version </dev/null 2>/dev/null | head -n 1 | tr -d '"\\' | cut -c1-60)
  j="$j{\"id\":\"$t\",\"path\":\"$p\",\"version\":\"$v\"},"
done
ip=$(hostname -I 2>/dev/null | awk '{print $1}')
[ -z "$ip" ] && ip=$(ipconfig getifaddr en0 2>/dev/null)
printf 'LOOM-MACHINE {"user":"%s","host":"%s","hostname":"%s","home":"%s","os":"%s","tools":[%s]}\n' "$(id -un)" "$ip" "$(hostname)" "$HOME" "$(uname -s)" "${j%,}"
`

func loadRemoteMachines() []RemoteMachine {
	list := []RemoteMachine{}
	_ = getStoreJSON(bkState, remoteMachinesState, &list)
	return list
}

// loomSSHKey returns Loom's private key path and public key, creating the key
// pair on first use (ed25519, no passphrase, readable by this user only).
func loomSSHKey() (string, string, error) {
	dir := filepath.Join(LoomHome(), "ssh")
	key := filepath.Join(dir, "loom_ed25519")
	if _, err := os.Stat(key); err != nil {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", "", err
		}
		host, _ := os.Hostname()
		if out, err := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-C", "loom@"+host, "-f", key).CombinedOutput(); err != nil {
			return "", "", errors.New("could not create SSH key: " + strings.TrimSpace(string(out)))
		}
	}
	pub, err := os.ReadFile(key + ".pub")
	if err != nil {
		return "", "", err
	}
	return key, strings.TrimSpace(string(pub)), nil
}

// remoteSetupBlock is what the user pastes on the remote machine: it allows
// Loom's key (once) and prints the machine description.
func remoteSetupBlock(pub string) string {
	return "mkdir -p ~/.ssh && chmod 700 ~/.ssh && touch ~/.ssh/authorized_keys && chmod 600 ~/.ssh/authorized_keys\n" +
		"grep -qxF '" + pub + "' ~/.ssh/authorized_keys || echo '" + pub + "' >> ~/.ssh/authorized_keys\n" +
		remoteProbeScript
}

var remoteHostRe = regexp.MustCompile(`^[A-Za-z0-9._:\-\[\]]{1,253}$`)
var remoteUserRe = regexp.MustCompile(`^[A-Za-z0-9._][A-Za-z0-9._\-]{0,63}$`)

func validRemoteMachine(m RemoteMachine) (RemoteMachine, error) {
	m.Name, m.Host, m.User = strings.TrimSpace(m.Name), strings.TrimSpace(m.Host), strings.TrimSpace(m.User)
	if m.Port == 0 {
		m.Port = 22
	}
	if !remoteHostRe.MatchString(m.Host) || strings.HasPrefix(m.Host, "-") {
		return m, errors.New("invalid machine address")
	}
	if !remoteUserRe.MatchString(m.User) {
		return m, errors.New("invalid user")
	}
	if m.Port < 1 || m.Port > 65535 {
		return m, errors.New("invalid port")
	}
	if m.Name == "" {
		m.Name = m.Host
	}
	if len([]rune(m.Name)) > 40 {
		return m, errors.New("name too long (40 characters)")
	}
	if m.ID == "" {
		m.ID = strings.Trim(acpCustomIDRe.ReplaceAllString(strings.ToLower(m.Name), "-"), "-")
		if m.ID == "" {
			m.ID = "machine"
		}
	}
	return m, nil
}

// sshArgs builds the ssh invocation for a machine; remote is the command run
// there (already a single shell string).
func sshArgs(m RemoteMachine, key string, remote ...string) []string {
	args := []string{"-T", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10", "-o", "StrictHostKeyChecking=accept-new"}
	if key != "" {
		args = append(args, "-i", key)
		// Keys this user already has keep working (ssh stops trying the
		// default ones as soon as -i is given).
		if home, err := os.UserHomeDir(); err == nil {
			for _, name := range []string{"id_ed25519", "id_ecdsa", "id_rsa"} {
				if p := filepath.Join(home, ".ssh", name); regularFile(p) {
					args = append(args, "-i", p)
				}
			}
		}
	}
	if m.Port != 22 {
		args = append(args, "-p", strconv.Itoa(m.Port))
	}
	return append(append(args, m.User+"@"+m.Host), remote...)
}

func shellQuote(s string) string {
	if s != "" && regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`).MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

// parseRemoteProbe reads the "LOOM-MACHINE {json}" line from the script output.
func parseRemoteProbe(out string) (RemoteMachine, error) {
	var info RemoteMachine
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if i := strings.Index(line, "LOOM-MACHINE "); i >= 0 {
			if err := json.Unmarshal([]byte(line[i+len("LOOM-MACHINE "):]), &info); err != nil {
				return info, errors.New("unreadable machine response")
			}
			return info, nil
		}
	}
	return info, errors.New("the machine did not return its description")
}

// checkRemoteMachine connects with Loom's key and runs the description script.
func checkRemoteMachine(ctx context.Context, m RemoteMachine) (RemoteMachine, error) {
	key, _, err := loomSSHKey()
	if err != nil {
		return m, err
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ssh", sshArgs(m, key, "sh", "-s")...)
	cmd.Stdin = strings.NewReader(remoteProbeScript)
	out, err := cmd.Output()
	if err != nil {
		// Native Windows OpenSSH normally has no POSIX sh.
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() != 255 {
			win := exec.CommandContext(ctx, "ssh", windowsRemoteSSHArgs(m, key, remoteWindowsProbeScript)...)
			out, err = win.Output()
		}
	}
	if err != nil {
		msg := ""
		if ee, ok := err.(*exec.ExitError); ok {
			msg = strings.TrimSpace(string(ee.Stderr))
		}
		switch {
		case strings.Contains(msg, "Permission denied"):
			return m, errors.New("connection refused: Loom's key is not authorized on this machine yet (paste the installation block into its terminal)")
		case strings.Contains(msg, "timed out") || strings.Contains(msg, "No route") || strings.Contains(msg, "Connection refused"):
			return m, errors.New("machine unreachable: check the address, port and that SSH is active")
		case strings.Contains(msg, "Host key verification failed") || strings.Contains(msg, "REMOTE HOST IDENTIFICATION HAS CHANGED"):
			return m, errors.New("the machine's SSH identity changed: verify it is the correct machine (~/.ssh/known_hosts)")
		}
		if msg == "" {
			msg = err.Error()
		}
		return m, errors.New("could not connect: " + lastLine(msg))
	}
	info, err := parseRemoteProbe(string(out))
	if err != nil {
		return m, err
	}
	m.Hostname, m.Home, m.OS, m.Tools, m.CheckedAt = info.Hostname, info.Home, info.OS, info.Tools, time.Now().UnixMilli()
	return m, nil
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

// remoteOffers lists the harnesses that can run on a checked machine.
func remoteOffers(m RemoteMachine) []map[string]any {
	have := map[string]RemoteTool{}
	for _, t := range m.Tools {
		have[t.ID] = t
	}
	out := []map[string]any{}
	for _, d := range remoteHarnessDefs {
		ok := true
		for _, n := range d.Needs {
			if _, found := have[n]; !found {
				ok = false
			}
		}
		version := have[d.Needs[0]].Version
		_, installed := have[d.Needs[0]]
		missing := ""
		if installed && !ok {
			missing = "Node.js (npx) required on the machine for the ACP adapter"
		}
		out = append(out, map[string]any{"id": d.ID, "name": d.Name, "logo": d.Logo, "installed": installed, "ready": ok, "version": version, "missing": missing})
	}
	return out
}

// remoteAgent builds the ACP agent for one harness on a machine. The remote
// command runs through `sh -lc` with the folders where the tools were found
// added to PATH, so npx and node resolve like in the user's terminal.
func remoteAgent(m RemoteMachine, harness string, key string) (acpAgent, error) {
	for _, d := range remoteHarnessDefs {
		if d.ID != harness {
			continue
		}
		launch := remoteLaunch(d.ID, d.Launch)
		if launch == nil {
			return acpAgent{}, errors.New("unknown launcher")
		}
		dirs := []string{}
		seen := map[string]bool{}
		for _, t := range m.Tools {
			if dir := filepath.Dir(t.Path); t.Path != "" && !seen[dir] {
				seen[dir] = true
				dirs = append(dirs, dir)
			}
		}
		parts := []string{}
		for _, p := range launch {
			parts = append(parts, shellQuote(p))
		}
		script := "exec " + strings.Join(parts, " ")
		if len(dirs) > 0 {
			script = "PATH=" + shellQuote(strings.Join(dirs, ":")) + ":\"$PATH\" " + script
		}
		args := sshArgs(m, key, "sh", "-c", shellQuote(script))
		if lifecycleOS(&m) == "windows" {
			args = windowsRemoteLifecycleCommand(m, key, launch)[1:]
		}
		a := acpAgent{
			ID: "custom-" + m.ID + "-" + d.ID, Name: d.Name, Logo: d.Logo, Command: "ssh",
			Args: args, Remote: true, Custom: true, Machine: m.ID, RemoteHome: m.Home,
		}
		return a, nil
	}
	return acpAgent{}, errors.New("harness not supported remotely")
}

func machineName(id string) string {
	for _, m := range loadRemoteMachines() {
		if m.ID == id {
			return m.Name
		}
	}
	return ""
}

// GET: machines, Loom's public key and the setup block.
// POST {machine, harnesses}: check, save the machine and (re)register its harnesses.
func handleRemoteMachines(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		_, pub, err := loomSSHKey()
		if err != nil {
			sendJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		list := loadRemoteMachines()
		offers := map[string]any{}
		for _, m := range list {
			offers[m.ID] = remoteOffers(m)
		}
		sendJSON(w, 200, map[string]any{"ok": true, "machines": list, "offers": offers, "key": pub, "setup": remoteSetupBlock(pub), "script": remoteProbeScript})
		return
	}
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		Machine   RemoteMachine `json:"machine"`
		Harnesses []string      `json:"harnesses"`
		CheckOnly bool          `json:"check_only"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	m, err := validRemoteMachine(req.Machine)
	if err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	m, err = checkRemoteMachine(r.Context(), m)
	if err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if req.CheckOnly {
		sendJSON(w, 200, map[string]any{"ok": true, "machine": m, "offers": remoteOffers(m)})
		return
	}
	key, _, _ := loomSSHKey()
	agents := []acpAgent{}
	for _, h := range req.Harnesses {
		a, err := remoteAgent(m, h, key)
		if err != nil {
			sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		agents = append(agents, a)
	}
	if err := saveRemoteMachine(m, agents); err != nil {
		sendJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	for _, a := range agents {
		go refreshACPProbe(context.Background(), a)
	}
	sendJSON(w, 200, map[string]any{"ok": true, "machine": m})
}

// saveRemoteMachine stores the machine and replaces its harnesses in the
// custom agents list and the live registry.
func saveRemoteMachine(m RemoteMachine, agents []acpAgent) error {
	m.Harnesses = nil
	for _, a := range agents {
		m.Harnesses = append(m.Harnesses, a.ID)
	}
	machines := []RemoteMachine{}
	replaced := false
	for _, old := range loadRemoteMachines() {
		if old.ID == m.ID {
			if m.Folders == nil {
				m.Folders = old.Folders // re-checking a machine keeps its folders
			}
			old, replaced = m, true
		}
		machines = append(machines, old)
	}
	if !replaced {
		machines = append(machines, m)
	}
	kept := []acpAgent{}
	removed := []string{}
	for _, a := range loadCustomACPAgents() {
		if a.Machine == m.ID {
			removed = append(removed, a.ID)
			continue
		}
		kept = append(kept, a)
	}
	kept = append(kept, agents...)
	if err := putStoreJSON(bkState, acpCustomState, kept); err != nil {
		return err
	}
	if err := putStoreJSON(bkState, remoteMachinesState, machines); err != nil {
		return err
	}
	for _, id := range removed {
		registeredRuntimes.remove(id)
	}
	for _, a := range agents {
		registeredRuntimes.upsert(&acpAdapter{agent: a})
	}
	return nil
}

func handleRemoteMachineDelete(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	machines := []RemoteMachine{}
	found := false
	for _, m := range loadRemoteMachines() {
		if m.ID == req.ID {
			found = true
			continue
		}
		machines = append(machines, m)
	}
	if !found {
		sendJSON(w, 404, map[string]any{"ok": false, "error": "machine not found"})
		return
	}
	kept := []acpAgent{}
	for _, a := range loadCustomACPAgents() {
		if a.Machine == req.ID {
			registeredRuntimes.remove(a.ID)
			continue
		}
		kept = append(kept, a)
	}
	if err := putStoreJSON(bkState, acpCustomState, kept); err != nil {
		sendJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if err := putStoreJSON(bkState, remoteMachinesState, machines); err != nil {
		sendJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true})
}

func regularFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}
