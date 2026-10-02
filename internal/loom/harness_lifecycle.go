package loom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"golang.org/x/mod/semver"
)

const harnessLifecyclePrefix = "harness_lifecycle:"
const harnessAutoInterval = 6 * time.Hour
const harnessActionTimeout = 15 * time.Minute
const harnessLogLimit = 32 << 10

type harnessLatestSpec struct {
	NPM    string `json:"npm,omitempty"`
	GitHub string `json:"github,omitempty"`
}

type harnessAutoResult struct {
	At   int64  `json:"at"`
	From string `json:"from"`
	To   string `json:"to"`
	OK   bool   `json:"ok"`
	Log  string `json:"log"`
}

type harnessLifecycleSetting struct {
	Target    string             `json:"target"`
	ID        string             `json:"id"`
	Auto      bool               `json:"auto"`
	LastAuto  *harnessAutoResult `json:"last_auto"`
	CheckedAt int64              `json:"checked_at,omitempty"`
}

type harnessLifecycleState struct {
	Target          string             `json:"target"`
	ID              string             `json:"id"`
	Installed       bool               `json:"installed"`
	Version         string             `json:"version"`
	Latest          string             `json:"latest"`
	UpdateAvailable bool               `json:"update_available"`
	RequiresMissing []string           `json:"requires_missing"`
	Auto            bool               `json:"auto"`
	LastAuto        *harnessAutoResult `json:"last_auto"`
	Unverified      bool               `json:"unverified"`
	Errors          []string           `json:"errors,omitempty"`
	Log             string             `json:"log"`
}

// One owner for locking, persistence and injectable execution/clock boundaries.
// Command definitions stay in the embedded catalog, never in per-harness Go code.
type harnessLifecycleService struct {
	mu           sync.Mutex
	busy         map[string]bool
	updating     map[string][2]string
	storeMu      sync.Mutex
	run          func(context.Context, *RemoteMachine, []string) (string, error)
	refresh      func(context.Context, *RemoteMachine, string) error
	latestGitHub func(context.Context, string) (string, error)
	active       func(string, string) bool
	reserveAuto  func(string, string) (func(), bool)
	now          func() time.Time
}

func newHarnessLifecycleService() *harnessLifecycleService {
	return &harnessLifecycleService{busy: map[string]bool{}, updating: map[string][2]string{}, run: runHarnessLifecycleCommand,
		refresh: refreshHarnessLifecycle, latestGitHub: readHarnessGitHubLatest,
		active: harnessDiscussionRunning, now: time.Now}
}

var harnessLifecycle = newHarnessLifecycleService()

func lifecycleKey(target, id string) string { return harnessLifecyclePrefix + target + ":" + id }
func (s *harnessLifecycleService) acquire(target, id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := lifecycleKey(target, id)
	if s.busy[key] {
		return false
	}
	s.busy[key] = true
	return true
}
func (s *harnessLifecycleService) release(target, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.busy, lifecycleKey(target, id))
}

func resolveHarnessTarget(target, id string) (string, *RemoteMachine, inspectSpec, error) {
	if target == "" {
		target = "local"
	}
	spec, ok := harnessInspectSpec(id)
	if !ok {
		return target, nil, spec, runtimeActionError{404, "unknown harness"}
	}
	if target == "local" {
		return target, nil, spec, nil
	}
	for _, m := range loadRemoteMachines() {
		if m.ID == target {
			valid, err := validRemoteMachine(m)
			if err != nil {
				return target, nil, spec, err
			}
			return target, &valid, spec, nil
		}
	}
	return target, nil, spec, runtimeActionError{404, "machine not found"}
}

func lifecycleOS(m *RemoteMachine) string {
	osName := runtime.GOOS
	if m != nil {
		osName = strings.ToLower(m.OS)
	}
	if strings.Contains(osName, "windows") || osName == "win32" {
		return "windows"
	}
	return "unix"
}

func lifecycleActionCommand(spec inspectSpec, osFamily, action string) ([]string, error) {
	var argv []string
	switch action {
	case "install":
		argv = spec.Install[osFamily]
	case "update":
		argv = spec.Update
		if spec.UpdateInstall {
			argv = spec.Install[osFamily]
		}
	default:
		return nil, errors.New("unknown action")
	}
	if len(argv) == 0 {
		return nil, errors.New("action not supported on this OS")
	}
	return append([]string{}, argv...), nil
}

// A URL argument ending in .sh/.ps1 represents an official script. Download it
// to a temporary file, then exec its interpreter; no local shell command string.
func lifecycleScriptArg(argv []string) int {
	for i, arg := range argv {
		if i > 0 && strings.HasPrefix(arg, "https://") && (strings.HasSuffix(arg, ".sh") || strings.HasSuffix(arg, ".ps1")) {
			return i
		}
	}
	return -1
}

func buildHarnessLifecycleCommand(m *RemoteMachine, key string, argv []string) ([]string, error) {
	if len(argv) == 0 {
		return nil, errors.New("empty command")
	}
	if m == nil {
		return append([]string{}, argv...), nil
	}
	if lifecycleOS(m) == "windows" {
		return windowsRemoteLifecycleCommand(*m, key, argv), nil
	}
	parts := make([]string, len(argv))
	for i, arg := range argv {
		parts[i] = shellQuote(arg)
	}
	script := remotePathPreamble
	if i := lifecycleScriptArg(argv); i >= 0 {
		parts[i] = `"$loom_installer"`
		script += "loom_installer=$(mktemp) || exit 1\ntrap 'rm -f \"$loom_installer\"' EXIT HUP INT TERM\n" +
			"curl -fsSL " + shellQuote(argv[i]) + " -o \"$loom_installer\" || exit 1\n" + strings.Join(parts, " ")
	} else {
		if argv[0] != "command" {
			script += "exec "
		}
		script += strings.Join(parts, " ")
	}
	return append([]string{"ssh"}, sshArgs(*m, key, "sh", "-c", shellQuote(script))...), nil
}

// Keep the actual tail bounded while the process writes, including stderr.
type harnessTail struct {
	mu   sync.Mutex
	data []byte
}

func (b *harnessTail) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	if n >= harnessLogLimit {
		b.data = append(b.data[:0], p[n-harnessLogLimit:]...)
	} else {
		if extra := len(b.data) + n - harnessLogLimit; extra > 0 {
			b.data = append(b.data[:0], b.data[extra:]...)
		}
		b.data = append(b.data, p...)
	}
	return n, nil
}
func (b *harnessTail) String() string { b.mu.Lock(); defer b.mu.Unlock(); return string(b.data) }

func lifecycleLocalPath() string {
	home, _ := os.UserHomeDir()
	dirs := []string{os.Getenv("PATH")}
	if runtime.GOOS == "windows" {
		dirs = append(dirs, filepath.Join(os.Getenv("APPDATA"), "npm"), filepath.Join(home, ".local", "bin"), filepath.Join(os.Getenv("LOCALAPPDATA"), "agy", "bin"), filepath.Join(os.Getenv("LOCALAPPDATA"), "hermes", "bin"))
	} else {
		dirs = append(dirs, filepath.Join(home, ".local", "bin"), filepath.Join(home, ".npm-global", "bin"), filepath.Join(home, ".bun", "bin"), "/usr/local/bin", "/opt/homebrew/bin")
		nvm, _ := filepath.Glob(filepath.Join(home, ".nvm", "versions", "node", "*", "bin"))
		dirs = append(dirs, nvm...)
	}
	return strings.Join(dirs, string(os.PathListSeparator))
}

func lifecycleLookPath(name string) (string, error) {
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	// Services launched before installation may have an older PATH.
	for _, d := range filepath.SplitList(lifecycleLocalPath()) {
		if d == "" {
			continue
		}
		if p, err := exec.LookPath(filepath.Join(d, name)); err == nil {
			return p, nil
		}
	}
	return "", exec.ErrNotFound
}

var npmShimPath = regexp.MustCompile(`(?i)"%dp0%[\\/]([^"\r\n]+)"`)

// Windows npm shims are batch files, not executables. Launch their JS entry
// with Node directly, preserving argv and never invoking cmd.exe/PowerShell.
func lifecycleWindowsShim(path string, args []string, read func(string) ([]byte, error), lookup func(string) (string, error)) ([]string, error) {
	if !strings.EqualFold(filepath.Ext(path), ".cmd") && !strings.EqualFold(filepath.Ext(path), ".bat") {
		return append([]string{path}, args...), nil
	}
	content, err := read(path)
	if err != nil {
		return nil, err
	}
	var match []string
	for _, candidate := range npmShimPath.FindAllStringSubmatch(string(content), -1) {
		if strings.HasPrefix(strings.ToLower(candidate[1]), `node_modules\`) || strings.HasPrefix(strings.ToLower(candidate[1]), "node_modules/") {
			match = candidate
			break
		}
	}
	if len(match) != 2 {
		return nil, errors.New("Windows launcher cannot execute without a shell")
	}
	node, err := lookup("node")
	if err != nil {
		return nil, err
	}
	entry := filepath.Join(filepath.Dir(path), filepath.FromSlash(strings.ReplaceAll(match[1], `\`, "/")))
	return append([]string{node, entry}, args...), nil
}

func harnessNativeArgv(argv []string) ([]string, error) {
	if len(argv) == 0 {
		return nil, errors.New("empty command")
	}
	path, err := lifecycleLookPath(argv[0])
	if err != nil {
		return nil, err
	}
	if runtime.GOOS == "windows" {
		return lifecycleWindowsShim(path, argv[1:], os.ReadFile, lifecycleLookPath)
	}
	return append([]string{path}, argv[1:]...), nil
}

func runHarnessLifecycleCommand(ctx context.Context, m *RemoteMachine, argv []string) (string, error) {
	if len(argv) == 0 {
		return "", errors.New("empty command")
	}
	if m == nil && argv[0] == "command" && len(argv) == 3 && argv[1] == "-v" {
		return lifecycleLookPath(argv[2])
	}
	var cleanup func()
	if m == nil {
		argv = append([]string{}, argv...)
		if i := lifecycleScriptArg(argv); i >= 0 {
			path, err := downloadHarnessInstaller(ctx, argv[i])
			if err != nil {
				return "", err
			}
			cleanup = func() { _ = os.Remove(path) }
			defer cleanup()
			argv[i] = path
		}
		var err error
		argv, err = harnessNativeArgv(argv)
		if err != nil {
			return "", err
		}
	} else {
		key, _, err := loomSSHKey()
		if err != nil {
			return "", err
		}
		var buildErr error
		argv, buildErr = buildHarnessLifecycleCommand(m, key, argv)
		if buildErr != nil {
			return "", buildErr
		}
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	if m == nil {
		cmd.Env = append(os.Environ(), "PATH="+lifecycleLocalPath())
	}
	out := &harnessTail{}
	cmd.Stdout, cmd.Stderr = out, out
	acpProcessGroup(cmd)
	cmd.Cancel = func() error { acpKillProcessGroup(cmd); return nil }
	cmd.WaitDelay = 2 * time.Second
	err := cmd.Run()
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	return out.String(), err
}

func downloadHarnessInstaller(ctx context.Context, url string) (string, error) {
	// Only the official https installers of the built-in catalog are run.
	if !strings.HasPrefix(url, "https://") {
		return "", errors.New("non-HTTPS installer refused")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("installer HTTP %d", resp.StatusCode)
	}
	suffix := "*.sh"
	if strings.HasSuffix(url, ".ps1") {
		suffix = "*.ps1"
	}
	f, err := os.CreateTemp("", "loom-installer-"+suffix)
	if err != nil {
		return "", err
	}
	n, copyErr := io.Copy(f, io.LimitReader(resp.Body, 4<<20+1))
	closeErr := f.Close()
	if copyErr != nil || closeErr != nil || n > 4<<20 {
		_ = os.Remove(f.Name())
		return "", errors.New("script download failed or too large")
	}
	return f.Name(), nil
}

func readHarnessGitHubLatest(ctx context.Context, repo string) (string, error) {
	if !regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`).MatchString(repo) {
		return "", errors.New("invalid GitHub repository")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/"+repo+"/releases/latest", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Loom")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("GitHub HTTP %d", resp.StatusCode)
	}
	var release struct {
		Tag string `json:"tag_name"`
	}
	err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&release)
	return release.Tag, err
}

var harnessVersionPattern = regexp.MustCompile(`(?:^|[^0-9A-Za-z])v?([0-9]+\.[0-9]+(?:\.[0-9]+)?(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?)`)

func normalizedHarnessVersion(text string) string {
	m := harnessVersionPattern.FindStringSubmatch(text)
	if len(m) != 2 {
		return ""
	}
	v := "v" + m[1]
	if !semver.IsValid(v) {
		return ""
	}
	return v
}
func harnessUpdateAvailable(installed, latest string) bool {
	a, b := normalizedHarnessVersion(installed), normalizedHarnessVersion(latest)
	return a != "" && b != "" && semver.Compare(a, b) < 0
}

func lifecycleMissingCommand(err error) bool {
	if errors.Is(err, exec.ErrNotFound) {
		return true
	}
	var exit *exec.ExitError
	return errors.As(err, &exit) && exit.ExitCode() == 1
}

func (s *harnessLifecycleService) setting(target, id string) harnessLifecycleSetting {
	s.storeMu.Lock()
	defer s.storeMu.Unlock()
	v := harnessLifecycleSetting{Target: target, ID: id}
	_ = getStoreJSON(bkState, lifecycleKey(target, id), &v)
	return v
}
func (s *harnessLifecycleService) setAuto(target, id string, auto bool) error {
	s.storeMu.Lock()
	defer s.storeMu.Unlock()
	v := harnessLifecycleSetting{Target: target, ID: id}
	_ = getStoreJSON(bkState, lifecycleKey(target, id), &v)
	v.Auto = auto
	return putStoreJSON(bkState, lifecycleKey(target, id), v)
}

func (s *harnessLifecycleService) check(ctx context.Context, target, id string, m *RemoteMachine, spec inspectSpec) (state harnessLifecycleState, checkErr error) {
	setting := s.setting(target, id)
	state = harnessLifecycleState{Target: target, ID: id, Auto: setting.Auto, LastAuto: setting.LastAuto, RequiresMissing: []string{}, Unverified: spec.Unverified}
	output := &harnessTail{}
	defer func() { state.Log = output.String() }()
	run := func(ctx context.Context, m *RemoteMachine, argv []string) (string, error) {
		out, err := s.run(ctx, m, argv)
		if out != "" {
			_, _ = output.Write([]byte(out + "\n"))
		}
		return out, err
	}
	probeCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	_, err := run(probeCtx, m, []string{"command", "-v", spec.Binary})
	if err != nil && !lifecycleMissingCommand(err) {
		return state, fmt.Errorf("detection failed: %w", err)
	}
	state.Installed = err == nil
	if state.Installed {
		out, e := run(probeCtx, m, spec.Version)
		if e != nil {
			state.Errors = append(state.Errors, "version : "+e.Error())
		} else {
			state.Version = strings.TrimSpace(strings.SplitN(out, "\n", 2)[0])
		}
	}
	requires := spec.Requires
	if tools, ok := spec.RequiresOS[lifecycleOS(m)]; ok {
		requires = tools
	}
	for _, tool := range requires {
		_, e := run(probeCtx, m, []string{"command", "-v", tool})
		if lifecycleMissingCommand(e) {
			state.RequiresMissing = append(state.RequiresMissing, tool)
		} else if e != nil {
			return state, fmt.Errorf("prerequisites: %w", e)
		}
	}
	if spec.Latest != nil {
		var out string
		var e error
		if spec.Latest.NPM != "" {
			out, e = run(probeCtx, m, []string{"npm", "view", spec.Latest.NPM, "version"})
		} else if spec.Latest.GitHub != "" {
			out, e = s.latestGitHub(probeCtx, spec.Latest.GitHub)
			if out != "" {
				_, _ = output.Write([]byte(out + "\n"))
			}
		}
		if e != nil {
			state.Errors = append(state.Errors, "latest : "+e.Error())
		} else {
			state.Latest = strings.TrimSpace(out)
		}
	}
	state.UpdateAvailable = state.Installed && harnessUpdateAvailable(state.Version, state.Latest)
	return state, nil
}

func (s *harnessLifecycleService) mutate(ctx context.Context, target, id, action string, m *RemoteMachine, spec inspectSpec) (harnessLifecycleState, error) {
	state := harnessLifecycleState{Target: target, ID: id, RequiresMissing: []string{}, Unverified: spec.Unverified}
	argv, err := lifecycleActionCommand(spec, lifecycleOS(m), action)
	if err != nil {
		return state, err
	}
	out, runErr := s.run(ctx, m, argv)
	// Even a failed installer may have changed files. Refresh observations in both cases.
	refreshErr := s.refresh(ctx, m, id)
	after, checkErr := s.check(ctx, target, id, m, spec)
	after.Log = out
	if runErr != nil {
		return after, fmt.Errorf("%s failed: %w", action, runErr)
	}
	if refreshErr != nil {
		return after, fmt.Errorf("actualisation : %w", refreshErr)
	}
	return after, checkErr
}

func (s *harnessLifecycleService) action(ctx context.Context, target, id, action string) (harnessLifecycleState, error) {
	target, m, spec, err := resolveHarnessTarget(target, id)
	if err != nil {
		return harnessLifecycleState{}, err
	}
	if action != "check" && action != "install" && action != "update" {
		return harnessLifecycleState{}, errors.New("unknown action")
	}
	if !s.acquire(target, id) {
		return harnessLifecycleState{}, runtimeActionError{409, "an action is already in progress for this harness on this machine"}
	}
	defer s.release(target, id)
	ctx, cancel := context.WithTimeout(ctx, harnessActionTimeout)
	defer cancel()
	if action == "check" {
		return s.check(ctx, target, id, m, spec)
	}
	return s.mutate(ctx, target, id, action, m, spec)
}

func refreshHarnessLifecycle(ctx context.Context, m *RemoteMachine, id string) error {
	if m != nil {
		checked, err := checkRemoteMachine(ctx, *m)
		if err != nil {
			return err
		}
		key, _, err := loomSSHKey()
		if err != nil {
			return err
		}
		agents := []acpAgent{}
		for _, old := range loadCustomACPAgents() {
			if old.Machine != m.ID {
				continue
			}
			for _, d := range remoteHarnessDefs {
				if old.ID == "custom-"+m.ID+"-"+d.ID {
					a, e := remoteAgent(checked, d.ID, key)
					if e != nil {
						return e
					}
					old = a
					break
				}
			}
			agents = append(agents, old)
		}
		if err := saveRemoteMachine(checked, agents); err != nil {
			return err
		}
		*m = checked
		for _, a := range agents {
			go refreshACPProbe(context.Background(), a)
		}
		return nil
	}
	inspectMu.Lock()
	delete(inspectCache, id)
	inspectMu.Unlock()
	for _, d := range runtimeCatalog() {
		a, ok := acpAgentFor(d.ID)
		if !ok || a.Remote || a.Machine != "" {
			continue
		}
		spec, _ := harnessInspectSpec(id)
		if a.ID != id && a.Command != spec.Binary {
			continue
		}
		acpProbeMu.Lock()
		err := putBytes(bkState, acpProbeKey+a.ID, nil)
		acpProbeMu.Unlock()
		if err != nil {
			return err
		}
		go refreshACPProbe(context.Background(), a)
	}
	return nil
}

func harnessRuntimeMatches(runtimeID, target, id string) bool {
	if target == "local" && runtimeID == id {
		return true
	}
	if target != "local" && runtimeID == "custom-"+target+"-"+id {
		return true
	}
	a, ok := acpAgentFor(runtimeID)
	if !ok {
		return false
	}
	spec, _ := harnessInspectSpec(id)
	return target == "local" && !a.Remote && a.Machine == "" && (a.Command == spec.Binary || strings.Contains(strings.Join(a.Detect, " "), spec.Binary))
}
func harnessDiscussionRunning(target, id string) bool {
	workspaceSessions.mu.Lock()
	defer workspaceSessions.mu.Unlock()
	for _, run := range workspaceSessions.runs {
		if harnessRuntimeMatches(run.session.RuntimeID, target, id) {
			return true
		}
	}
	return false
}

func harnessAutoDue(setting harnessLifecycleSetting, state harnessLifecycleState, now time.Time, active bool) bool {
	return setting.Auto && (setting.CheckedAt == 0 || now.Sub(time.UnixMilli(setting.CheckedAt)) >= harnessAutoInterval) &&
		state.Installed && state.UpdateAvailable && !active
}

// A cycle holds the same pair lock as HTTP across check and update. Settings
// are re-read before updating, so disabling auto during a check takes effect.
func (s *harnessLifecycleService) autoOne(ctx context.Context, target, id string) {
	target, m, spec, err := resolveHarnessTarget(target, id)
	if err != nil || !s.acquire(target, id) {
		return
	}
	defer s.release(target, id)
	setting := s.setting(target, id)
	now := s.now()
	if !setting.Auto || setting.CheckedAt != 0 && now.Sub(time.UnixMilli(setting.CheckedAt)) < harnessAutoInterval || s.active(target, id) {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, harnessActionTimeout)
	defer cancel()
	state, checkErr := s.check(ctx, target, id, m, spec)
	setting = s.setting(target, id)
	var result *harnessAutoResult
	if checkErr == nil && harnessAutoDue(setting, state, now, s.active(target, id)) {
		reserve := s.reserveAuto
		if reserve == nil {
			reserve = func(target, id string) (func(), bool) { return reserveHarnessAutoUpdate(s, target, id) }
		}
		release, allowed := reserve(target, id)
		if !allowed {
			return
		}
		after, updateErr := s.mutate(ctx, target, id, "update", m, spec)
		release()
		log := after.Log
		if updateErr != nil {
			log = tailHarnessText(log + "\n" + updateErr.Error())
		}
		result = &harnessAutoResult{At: s.now().UnixMilli(), From: state.Version, To: after.Version, OK: updateErr == nil, Log: log}
	} else if checkErr != nil {
		result = &harnessAutoResult{At: now.UnixMilli(), OK: false, Log: tailHarnessText(checkErr.Error())}
	}
	s.storeMu.Lock()
	defer s.storeMu.Unlock()
	current := harnessLifecycleSetting{Target: target, ID: id}
	_ = getStoreJSON(bkState, lifecycleKey(target, id), &current)
	current.CheckedAt = now.UnixMilli()
	if result != nil {
		current.LastAuto = result
	}
	_ = putStoreJSON(bkState, lifecycleKey(target, id), current)
}
func tailHarnessText(text string) string {
	if len(text) > harnessLogLimit {
		return text[len(text)-harnessLogLimit:]
	}
	return text
}
func (s *harnessLifecycleService) autoCycle(ctx context.Context) {
	for key, value := range allKV(bkState) {
		if ctx.Err() != nil {
			return
		}
		if !strings.HasPrefix(key, harnessLifecyclePrefix) {
			continue
		}
		var setting harnessLifecycleSetting
		if json.Unmarshal([]byte(value), &setting) == nil && setting.Auto {
			s.autoOne(ctx, setting.Target, setting.ID)
		}
	}
}
func (s *harnessLifecycleService) autoLoop(ctx context.Context) {
	ticker := time.NewTicker(harnessAutoInterval)
	defer ticker.Stop()
	for {
		s.autoCycle(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func sendHarnessLifecycle(w http.ResponseWriter, state harnessLifecycleState, err error) {
	status := http.StatusOK
	response := map[string]any{"ok": err == nil, "state": state, "log": state.Log}
	if err != nil {
		status = http.StatusBadRequest
		var actionError runtimeActionError
		if errors.As(err, &actionError) {
			status = actionError.status
		}
		response["error"] = err.Error()
	}
	sendJSON(w, status, response)
}
func handleHarnessLifecycle(w http.ResponseWriter, r *http.Request) {
	target, id, action := r.URL.Query().Get("target"), r.URL.Query().Get("id"), "check"
	if r.Method != http.MethodGet {
		if !workspaceMethod(w, r, http.MethodPost) {
			return
		}
		var req struct {
			Target string `json:"target"`
			ID     string `json:"id"`
			Action string `json:"action"`
		}
		if !workspaceDecode(w, r, &req) {
			return
		}
		target, id, action = req.Target, req.ID, req.Action
	}
	state, err := harnessLifecycle.action(r.Context(), target, id, action)
	sendHarnessLifecycle(w, state, err)
}
func handleHarnessLifecycleAuto(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		Target string `json:"target"`
		ID     string `json:"id"`
		Auto   bool   `json:"auto"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	target, _, _, err := resolveHarnessTarget(req.Target, req.ID)
	if err != nil {
		sendHarnessLifecycle(w, harnessLifecycleState{}, err)
		return
	}
	if err := harnessLifecycle.setAuto(target, req.ID, req.Auto); err != nil {
		sendJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "auto": req.Auto, "last_auto": harnessLifecycle.setting(target, req.ID).LastAuto})
}

// Resolve the old runtime-path API to the same pair, including connected ACP agents.
func harnessLifecycleRuntimeTarget(runtimeID string) (string, string) {
	if _, ok := harnessInspectSpec(runtimeID); ok {
		return "local", runtimeID
	}
	if a, ok := acpAgentFor(runtimeID); ok && a.Machine != "" {
		for _, d := range remoteHarnessDefs {
			if runtimeID == "custom-"+a.Machine+"-"+d.ID {
				return a.Machine, d.ID
			}
		}
	}
	return "local", runtimeID
}

func reserveHarnessAutoUpdate(s *harnessLifecycleService, target, id string) (func(), bool) {
	workspaceSessions.mu.Lock()
	defer workspaceSessions.mu.Unlock()
	for _, run := range workspaceSessions.runs {
		if harnessRuntimeMatches(run.session.RuntimeID, target, id) {
			return nil, false
		}
	}
	key := lifecycleKey(target, id)
	s.mu.Lock()
	s.updating[key] = [2]string{target, id}
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		delete(s.updating, key)
		s.mu.Unlock()
	}, true
}
func (s *harnessLifecycleService) updatingRuntime(runtimeID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, pair := range s.updating {
		if harnessRuntimeMatches(runtimeID, pair[0], pair[1]) {
			return true
		}
	}
	return false
}
