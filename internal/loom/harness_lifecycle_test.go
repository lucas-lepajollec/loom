package loom

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf16"
)

func TestHarnessLifecycleCatalog(t *testing.T) {
	var specs map[string]inspectSpec
	if err := json.Unmarshal(harnessInspectJSON, &specs); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"codex", "claude-code", "pi", "hermes", "opencode", "antigravity"} {
		if _, ok := specs[id]; !ok {
			t.Fatalf("missing %s", id)
		}
	}
	for id, s := range specs {
		t.Run(id, func(t *testing.T) {
			if s.Binary == "" || len(s.Version) < 2 || s.Version[0] != s.Binary {
				t.Fatalf("invalid version command: %+v", s)
			}
			if len(s.Update) == 0 && !s.UpdateInstall {
				t.Fatal("missing update")
			}
			if !strings.HasPrefix(s.Source, "https://") {
				t.Fatal("missing official source")
			}
			if len(s.Requires) == 0 {
				t.Fatal("missing prerequisites")
			}
			for _, family := range []string{"unix", "windows"} {
				argv, err := lifecycleActionCommand(s, family, "install")
				if err != nil || len(argv) == 0 {
					t.Fatalf("%s: %v", family, err)
				}
				for _, arg := range argv {
					if arg == "-c" || arg == "-Command" || strings.ContainsRune(arg, '\x00') {
						t.Fatalf("local shell command: %v", argv)
					}
				}
				if _, err := lifecycleActionCommand(s, family, "update"); err != nil {
					t.Fatal(err)
				}
			}
			if s.Latest != nil && (s.Latest.NPM == "") == (s.Latest.GitHub == "") {
				t.Fatal("latest must have one source")
			}
		})
	}
}

func TestHarnessLifecycleCommandArgv(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	argv := []string{"npm", "install", "-g", "@openai/codex@latest"}
	local, err := buildHarnessLifecycleCommand(nil, "", argv)
	if err != nil || !reflect.DeepEqual(local, argv) {
		t.Fatalf("%v %v", local, err)
	}
	m := RemoteMachine{ID: "box", Host: "host.example", User: "user", Port: 2222, OS: "Linux"}
	remote, err := buildHarnessLifecycleCommand(&m, "/key with space", argv)
	want := []string{"ssh", "-T", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10", "-o", "StrictHostKeyChecking=accept-new", "-i", "/key with space", "-p", "2222", "user@host.example", "sh", "-c", shellQuote(remoteOutputStart + remotePathPreamble + remoteNPMPrefixScript(argv) + "exec npm install -g --prefix \"$loom_npm_prefix\" @openai/codex@latest")}
	if err != nil || !reflect.DeepEqual(remote, want) {
		t.Fatalf("got %q\nwant %q\n%v", remote, want, err)
	}
	hostile := []string{"tool", "a' ; $(touch /tmp/not-created)\nvalue"}
	remote, err = buildHarnessLifecycleCommand(&m, "", hostile)
	want = append([]string{"ssh", "-T", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10", "-o", "StrictHostKeyChecking=accept-new", "-p", "2222", "user@host.example", "sh", "-c"}, shellQuote(remoteOutputStart+remotePathPreamble+"exec tool "+shellQuote(hostile[1])))
	if err != nil || !reflect.DeepEqual(remote, want) {
		t.Fatalf("unsafe argv: %q", remote)
	}
	probe, _ := buildHarnessLifecycleCommand(&m, "", []string{"command", "-v", "npm"})
	if probe[len(probe)-1] != shellQuote(remoteOutputStart+remotePathPreamble+"command -v npm") {
		t.Fatal(probe)
	}
	spec, _ := harnessInspectSpec("hermes")
	script, _ := buildHarnessLifecycleCommand(&m, "", spec.Install["unix"])
	expected := remoteOutputStart + remotePathPreamble + "loom_installer=$(mktemp) || exit 1\ntrap 'rm -f \"$loom_installer\"' EXIT HUP INT TERM\ncurl -fsSL https://hermes-agent.nousresearch.com/install.sh -o \"$loom_installer\" || exit 1\nbash \"$loom_installer\" --non-interactive"
	if script[len(script)-1] != shellQuote(expected) {
		t.Fatalf("script: %s", script[len(script)-1])
	}
}

func TestHarnessLifecycleWindowsCommandAndShim(t *testing.T) {
	m := RemoteMachine{Host: "win.example", User: "user", Port: 22, OS: "Windows"}
	argv := []string{"npm", "view", "@openai/codex", "version"}
	got, err := buildHarnessLifecycleCommand(&m, "", argv)
	script := remoteWindowsPathPreamble + "& 'npm' 'view' '@openai/codex' 'version'\nexit $LASTEXITCODE"
	words := utf16.Encode([]rune(script))
	payload := make([]byte, len(words)*2)
	for i, w := range words {
		binary.LittleEndian.PutUint16(payload[i*2:], w)
	}
	want := []string{"ssh", "-T", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10", "-o", "StrictHostKeyChecking=accept-new", "user@win.example", "powershell", "-NoProfile", "-NonInteractive", "-EncodedCommand", shellQuote(base64.StdEncoding.EncodeToString(payload))}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("%q\n%q\n%v", got, want, err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "npm.cmd")
	content := []byte(`@echo off
IF EXIST "%dp0%\node.exe" (
 "%dp0%\node.exe" "%dp0%\node_modules\npm\bin\npm-cli.js" %*
) ELSE (
 node "%dp0%\node_modules\npm\bin\npm-cli.js" %*
)`)
	native, err := lifecycleWindowsShim(path, argv[1:], func(string) ([]byte, error) { return content, nil }, func(string) (string, error) { return "node.exe", nil })
	want = append([]string{"node.exe", filepath.Join(dir, "node_modules", "npm", "bin", "npm-cli.js")}, argv[1:]...)
	if err != nil || !reflect.DeepEqual(native, want) {
		t.Fatalf("%q\n%q\n%v", native, want, err)
	}
	_, err = lifecycleWindowsShim(path, nil, func(string) ([]byte, error) { return []byte("arbitrary batch"), nil }, nil)
	if err == nil {
		t.Fatal("accepted arbitrary .cmd")
	}
}

func TestHarnessLifecycleVersions(t *testing.T) {
	for _, tc := range []struct {
		from, to string
		want     bool
	}{
		{"codex-cli 0.159.2", "0.160.0", true}, {"v1.2.3", "1.2.4", true}, {"2.1.0 (Claude Code)", "2.1.1", true},
		{"1.2.3", "v1.2.3", false}, {"1.10.0", "1.9.9", false}, {"1.2", "1.2.1", true},
		{"1.2.3-rc.2", "1.2.3", true}, {"1.2.3-beta.2", "1.2.3-beta.10", true}, {"1.2.3+build", "1.2.3", false},
		{"unknown", "1.2.3", false}, {"", "1.2.3", false}, {"1.2.3", "", false}, {"1.2.3", "unknown", false},
	} {
		if got := harnessUpdateAvailable(tc.from, tc.to); got != tc.want {
			t.Errorf("%q -> %q: %v", tc.from, tc.to, got)
		}
	}
}

func TestHarnessLifecycleOneAtATime(t *testing.T) {
	testHome(t)
	s := newHarnessLifecycleService()
	started, finish := make(chan struct{}), make(chan struct{})
	var once sync.Once
	s.run = func(context.Context, *RemoteMachine, []string) (string, error) {
		once.Do(func() { close(started); <-finish })
		return "1.0.0", nil
	}
	done := make(chan error, 1)
	go func() { _, err := s.action(context.Background(), "local", "codex", "check"); done <- err }()
	<-started
	_, err := s.action(context.Background(), "local", "codex", "install")
	var actionErr runtimeActionError
	if !errors.As(err, &actionErr) || actionErr.status != 409 {
		t.Errorf("expected 409: %v", err)
	}
	if !s.acquire("other", "codex") || !s.acquire("local", "pi") {
		t.Error("independent pairs blocked")
	}
	s.release("other", "codex")
	s.release("local", "pi")
	close(finish)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !s.acquire("local", "codex") {
		t.Fatal("lock not released")
	}
	s.release("local", "codex")
}

func fakeHarnessLifecycle(t *testing.T) (*harnessLifecycleService, *string, *int, *int) {
	t.Helper()
	testHome(t)
	s := newHarnessLifecycleService()
	version := "codex-cli 1.0.0"
	updates, refreshes := 0, 0
	s.run = func(ctx context.Context, m *RemoteMachine, argv []string) (string, error) {
		if argv[0] == "command" {
			return "/bin/tool", nil
		}
		if reflect.DeepEqual(argv, []string{"codex", "--version"}) {
			return version, nil
		}
		if reflect.DeepEqual(argv, []string{"npm", "view", "@openai/codex", "version"}) {
			return "1.1.0\n", nil
		}
		if reflect.DeepEqual(argv, []string{"npm", "install", "-g", "@openai/codex@latest"}) {
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) < 14*time.Minute {
				t.Error("missing 15 minute action timeout")
			}
			updates++
			version = "codex-cli 1.1.0"
			return "updated", nil
		}
		t.Errorf("unexpected command: %q", argv)
		return "", errors.New("unexpected command")
	}
	s.refresh = func(context.Context, *RemoteMachine, string) error { refreshes++; return nil }
	s.active = func(string, string) bool { return false }
	s.reserveAuto = func(string, string) (func(), bool) { return func() {}, true }
	return s, &version, &updates, &refreshes
}

func TestHarnessLifecycleAutoClockAndVersions(t *testing.T) {
	s, version, updates, refreshes := fakeHarnessLifecycle(t)
	now := time.Unix(1700000000, 0)
	s.now = func() time.Time { return now }
	s.autoCycle(context.Background())
	if *updates != 0 {
		t.Fatal("auto must default to off")
	}
	if err := s.setAuto("local", "codex", true); err != nil {
		t.Fatal(err)
	}
	s.autoCycle(context.Background())
	saved := s.setting("local", "codex")
	if *updates != 1 || *refreshes != 1 || saved.LastAuto == nil || !saved.LastAuto.OK || saved.LastAuto.From != "codex-cli 1.0.0" || saved.LastAuto.To != *version || saved.LastAuto.At != now.UnixMilli() || saved.LastAuto.Log != "updated" {
		t.Fatalf("updates=%d refreshes=%d saved=%+v", *updates, *refreshes, saved)
	}
	*version = "codex-cli 1.0.0"
	now = now.Add(harnessAutoInterval - time.Millisecond)
	s.autoCycle(context.Background())
	if *updates != 1 {
		t.Fatal("ran before six hours")
	}
	now = now.Add(time.Millisecond)
	s.autoCycle(context.Background())
	if *updates != 2 {
		t.Fatal("did not run at six hours")
	}
	now = now.Add(harnessAutoInterval)
	s.autoCycle(context.Background())
	if *updates != 2 {
		t.Fatal("updated latest version")
	}
	if err := s.setAuto("local", "codex", false); err != nil {
		t.Fatal(err)
	}
	*version = "codex-cli 1.0.0"
	now = now.Add(harnessAutoInterval)
	s.autoCycle(context.Background())
	if *updates != 2 || s.setting("local", "codex").LastAuto == nil {
		t.Fatal("disable failed or erased last result")
	}
}

func TestHarnessLifecycleAutoSkipsAndFailures(t *testing.T) {
	for _, scenario := range []string{"active", "started-during-check", "disabled-during-check", "missing", "unknown-latest", "failed-update"} {
		t.Run(scenario, func(t *testing.T) {
			s, _, updates, _ := fakeHarnessLifecycle(t)
			if err := s.setAuto("local", "codex", true); err != nil {
				t.Fatal(err)
			}
			original := s.run
			active := false
			if scenario == "active" {
				active = true
			}
			s.active = func(string, string) bool { return active }
			s.run = func(ctx context.Context, m *RemoteMachine, argv []string) (string, error) {
				if argv[0] == "command" && argv[2] == "codex" && scenario == "missing" {
					return "", exec.ErrNotFound
				}
				if argv[0] == "npm" && argv[1] == "view" {
					if scenario == "started-during-check" {
						active = true
					}
					if scenario == "disabled-during-check" {
						if err := s.setAuto("local", "codex", false); err != nil {
							t.Fatal(err)
						}
					}
					if scenario == "unknown-latest" {
						return "", errors.New("offline")
					}
				}
				if argv[0] == "npm" && argv[1] == "install" && scenario == "failed-update" {
					return strings.Repeat("x", harnessLogLimit), errors.New("install failed")
				}
				return original(ctx, m, argv)
			}
			s.autoOne(context.Background(), "local", "codex")
			if *updates != 0 {
				t.Fatal("unexpected update")
			}
			if scenario == "failed-update" {
				last := s.setting("local", "codex").LastAuto
				if last == nil || last.OK || len(last.Log) > harnessLogLimit || !strings.Contains(last.Log, "install failed") {
					t.Fatalf("%+v", last)
				}
			}
		})
	}
}

func TestHarnessLifecycleRunningTargetAndReservation(t *testing.T) {
	old := workspaceSessions
	workspaceSessions = newRuntimeSessions()
	defer func() { workspaceSessions = old }()
	workspaceSessions.runs["run"] = &runtimeRun{session: RuntimeSession{RuntimeID: "custom-box-codex"}}
	if !harnessDiscussionRunning("box", "codex") || harnessDiscussionRunning("local", "codex") || harnessDiscussionRunning("box", "pi") {
		t.Fatal("incorrect target match")
	}
	s := newHarnessLifecycleService()
	if _, ok := reserveHarnessAutoUpdate(s, "box", "codex"); ok {
		t.Fatal("reserved active harness")
	}
	release, ok := reserveHarnessAutoUpdate(s, "local", "codex")
	if !ok || !s.updatingRuntime("codex") || s.updatingRuntime("custom-box-codex") {
		t.Fatal("invalid reservation")
	}
	release()
	if s.updatingRuntime("codex") {
		t.Fatal("reservation leaked")
	}
}

func TestHarnessLifecycleHTTPAndLegacy(t *testing.T) {
	s, _, updates, _ := fakeHarnessLifecycle(t)
	old := harnessLifecycle
	harnessLifecycle = s
	defer func() { harnessLifecycle = old }()
	mux := newWebMux()
	for _, tc := range []struct {
		method, url, body string
		code              int
	}{
		{"GET", "/api/harness/lifecycle?target=local&id=codex", "", 200},
		{"POST", "/api/harness/lifecycle/auto", `{"target":"local","id":"codex","auto":true}`, 200},
		{"POST", "/api/harness/lifecycle", `{"target":"local","id":"codex","action":"check"}`, 200},
		{"POST", "/api/harness/lifecycle", `{"id":"unknown","action":"install"}`, 404},
		{"POST", "/api/harness/lifecycle", `{"target":"missing","id":"codex","action":"install"}`, 404},
		{"POST", "/api/harness/lifecycle", `{"id":"codex","action":"delete"}`, 400},
		{"PUT", "/api/harness/lifecycle", "", 405},
	} {
		req := localTestRequest(tc.method, tc.url, strings.NewReader(tc.body))
		w := httptest.NewRecorder()
		req.Header.Set("Content-Type", "application/json")
		mux.ServeHTTP(w, req)
		if w.Code != tc.code {
			t.Fatalf("%s %s: %d %s", tc.method, tc.url, w.Code, w.Body.String())
		}
	}
	// Avoid the legacy handler's optional full local account inspection by using
	// a connected machine runtime; it still dispatches the same update action.
	m := RemoteMachine{ID: "box", Host: "host.example", User: "user", Port: 22, OS: "Linux"}
	if err := putStoreJSON(bkState, remoteMachinesState, []RemoteMachine{m}); err != nil {
		t.Fatal(err)
	}
	agent := acpAgent{ID: "custom-box-codex", Machine: "box", Command: "ssh", Remote: true, Custom: true}
	registeredRuntimes.upsert(&acpAdapter{agent: agent})
	defer registeredRuntimes.remove(agent.ID)
	w := httptest.NewRecorder()
	req := localTestRequest("POST", "/api/runtimes/custom-box-codex/update", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	mux.ServeHTTP(w, req)
	if w.Code != 200 || *updates != 1 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if !s.acquire("local", "codex") {
		t.Fatal("lock busy")
	}
	defer s.release("local", "codex")
	w = httptest.NewRecorder()
	req = localTestRequest("POST", "/api/harness/lifecycle", strings.NewReader(`{"id":"codex","action":"check"}`))
	req.Header.Set("Content-Type", "application/json")
	mux.ServeHTTP(w, req)
	if w.Code != 409 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}

func TestHarnessLifecycleProcessHelper(t *testing.T) {
	args := os.Args
	if len(args) < 2 || !strings.HasPrefix(args[len(args)-1], "loom-lifecycle-") {
		return
	}
	switch args[len(args)-1] {
	case "loom-lifecycle-tail":
		fmtText := strings.Repeat("x", 40<<10) + "THE-END"
		_, _ = io.WriteString(os.Stdout, fmtText)
		os.Exit(0)
	case "loom-lifecycle-wait":
		time.Sleep(time.Minute)
		os.Exit(0)
	default:
		_ = json.NewEncoder(os.Stdout).Encode(args[len(args)-2:])
		os.Exit(0)
	}
}
func TestHarnessLifecycleExecTailArgvAndCancellation(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	out, err := runHarnessLifecycleCommand(context.Background(), nil, []string{exe, "-test.run=^TestHarnessLifecycleProcessHelper$", "loom-lifecycle-tail"})
	if err != nil || len(out) != harnessLogLimit || !strings.HasSuffix(out, "THE-END") {
		t.Fatalf("len=%d %v", len(out), err)
	}
	hostile := "a' ; $(echo bad)\nhello"
	out, err = runHarnessLifecycleCommand(context.Background(), nil, []string{exe, "-test.run=^TestHarnessLifecycleProcessHelper$", hostile, "loom-lifecycle-argv"})
	var got []string
	_ = json.Unmarshal([]byte(out), &got)
	if err != nil || !reflect.DeepEqual(got, []string{hostile, "loom-lifecycle-argv"}) {
		t.Fatalf("%q %v", out, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err = runHarnessLifecycleCommand(ctx, nil, []string{exe, "-test.run=^TestHarnessLifecycleProcessHelper$", "loom-lifecycle-wait"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}

// Socket-free HTTP transport for release metadata and installer downloads.
type lifecycleRoundTrip func(*http.Request) (*http.Response, error)

func (f lifecycleRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestHarnessLifecycleLatestAndInstallerDownload(t *testing.T) {
	old := http.DefaultClient
	defer func() { http.DefaultClient = old }()
	http.DefaultClient = &http.Client{Transport: lifecycleRoundTrip(func(r *http.Request) (*http.Response, error) {
		body := "echo installer"
		if strings.Contains(r.URL.Host, "github") {
			body = `{"tag_name":"v1.2.3"}`
			if r.URL.Path != "/repos/owner/repo/releases/latest" {
				t.Fatal(r.URL)
			}
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
	})}
	got, err := readHarnessGitHubLatest(context.Background(), "owner/repo")
	if err != nil || got != "v1.2.3" {
		t.Fatalf("%q %v", got, err)
	}
	if _, err := readHarnessGitHubLatest(context.Background(), "owner/repo?injected"); err == nil {
		t.Fatal("invalid repo accepted")
	}
	path, err := downloadHarnessInstaller(context.Background(), "https://official.example/install.ps1")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	body, err := os.ReadFile(path)
	if err != nil || string(body) != "echo installer" || filepath.Ext(path) != ".ps1" {
		t.Fatalf("%q %v", body, err)
	}
}

func TestHarnessLifecycleRemoteWindowsWorkdir(t *testing.T) {
	for input, want := range map[string]string{`c:\Users\person\app\..\project`: `C:/Users/person/project`, `D:/work/project/`: `D:/work/project`} {
		got, err := remoteWorkdir(input)
		if err != nil || got != want {
			t.Fatalf("%q => %q %v", input, got, err)
		}
	}
	if _, err := remoteWorkdir(`C:relative`); err == nil {
		t.Fatal("drive-relative directory accepted")
	}
}
