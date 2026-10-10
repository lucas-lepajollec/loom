package loom

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestNodeEnvironmentMissingModuleNeverUsesSSH(t *testing.T) {
	testHome(t)
	e := newEnvironment()
	e.machines = func() []RemoteMachine {
		return []RemoteMachine{{ID: "node-only", NodeID: "paired", Modules: []string{"engine"}}}
	}
	calls := 0
	e.sshKey = func() (string, string, error) { calls++; return "fake", "", nil }
	e.run = func(context.Context, string, []string, string) ([]byte, error) {
		calls++
		return []byte("LOOM-TCP\n"), nil
	}
	q := e.check(context.Background(), "example.invalid:80", "node-only")
	if q.OK || !strings.Contains(q.Error, "observe") || calls != 0 {
		t.Fatalf("unsupported check claimed success or used SSH: %+v calls=%d", q, calls)
	}
	w := envTestRequest(e.docker, "GET", "/api/env/docker?machine=node-only", "")
	if !strings.Contains(w.Body.String(), `"unsupported":true`) || !strings.Contains(w.Body.String(), `"ok":false`) {
		t.Fatalf("unsupported Docker advertised as disabled/working: %s", w.Body)
	}
	w = envTestRequest(e.docker, "POST", "/api/env/docker", `{"machine":"node-only","enabled":true}`)
	if w.Code != 409 {
		t.Fatalf("unsupported Docker enabled: %d %s", w.Code, w.Body)
	}
}

func TestNodeUsageCommandUsesBridgeAndNeverSSH(t *testing.T) {
	testHome(t)
	m, err := savePairedMachine(RemoteMachine{Name: "fixture", Host: "127.0.0.1"}, &engineNode{URL: "http://127.0.0.1:2511", WebKey: "fixture-token", Role: "engine-node", NodeID: "fixture", Modules: []string{"harness"}, Handshake: 1})
	if err != nil {
		t.Fatal(err)
	}
	old := nodeClient
	calls := 0
	nodeClient = &http.Client{Transport: envRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Path != "/api/node/harness/usage" || r.Header.Get("Authorization") != "Bearer fixture-token" {
			t.Errorf("wrong bridge request: %s", r.URL)
		}
		w := httptest.NewRecorder()
		w.WriteString(`{"ok":true,"output":"fixture usage"}`)
		return w.Result(), nil
	})}
	defer func() { nodeClient = old }()
	a := acpAgent{ID: remoteRuntimeID(m.ID, "hermes"), Remote: true, Machine: m.ID}
	out, err := harnessUsageCommand(context.Background(), a, []string{"hermes", "insights", "--days", "7"})
	if err != nil || string(out) != "fixture usage" || calls != 1 {
		t.Fatalf("Node usage bypassed bridge: %q %v calls=%d", out, err, calls)
	}
	m.Modules = nil
	if err := putStoreJSON(bkState, remoteMachinesState, []RemoteMachine{m}); err != nil {
		t.Fatal(err)
	}
	_, err = harnessUsageCommand(context.Background(), a, []string{"hermes", "usage"})
	if err == nil || !strings.Contains(err.Error(), "harness") || calls != 1 {
		t.Fatalf("unsupported usage not explicit: %v calls=%d", err, calls)
	}
}

func TestNodeEnvironmentRoutesAuthenticatedAndDisabled(t *testing.T) {
	_, mux, token := nodeHarnessFixture(t, 1)
	for _, path := range []string{"/api/node/env/docker", "/api/node/env/check?target=example.invalid:80", "/api/node/harness/usage?harness=hermes&reading=quota"} {
		if w := nodeHarnessGet(mux, path, ""); w.Code != 401 {
			t.Errorf("route unprotected or absent %s: %d", path, w.Code)
		}
	}
	if err := putBool(bkState, nodeObserveDisabledKey, true); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/node/env/docker", "/api/node/env/check?target=example.invalid:80"} {
		w := nodeHarnessGet(mux, path, token)
		if w.Code != 409 || !strings.Contains(w.Body.String(), `"unsupported":true`) {
			t.Errorf("disabled observe not explicit: %d %s", w.Code, w.Body)
		}
	}
	if err := putBool(bkState, nodeHarnessDisabledKey, true); err != nil {
		t.Fatal(err)
	}
	w := nodeHarnessGet(mux, "/api/node/harness/usage?harness=hermes&reading=quota", token)
	if w.Code != 409 {
		t.Fatal(w.Code, w.Body)
	}
}

func TestNodeOldObservationRoutesAreUnsupported(t *testing.T) {
	testHome(t)
	m, err := savePairedMachine(RemoteMachine{Name: "old fixture", Host: "127.0.0.1"}, &engineNode{URL: "http://127.0.0.1:2511", WebKey: "fixture-token", Role: "engine-node", NodeID: "old", Modules: []string{"observe", "harness"}, Handshake: 1})
	if err != nil {
		t.Fatal(err)
	}
	old := nodeClient
	nodeClient = &http.Client{Transport: envRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		w := httptest.NewRecorder()
		w.WriteHeader(404)
		return w.Result(), nil
	})}
	defer func() { nodeClient = old }()
	e := newEnvironment()
	w := envTestRequest(e.docker, "POST", "/api/env/docker", `{"machine":"`+m.ID+`","enabled":true}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	w = envTestRequest(e.docker, "GET", "/api/env/docker?machine="+m.ID, "")
	if !strings.Contains(w.Body.String(), `"unsupported":true`) {
		t.Fatalf("old Node route shown as supported: %s", w.Body)
	}
	q := e.check(context.Background(), "example.invalid:80", m.ID)
	raw, _ := json.Marshal(q)
	if !strings.Contains(string(raw), `"unsupported":true`) {
		t.Fatalf("old Node check shown as supported: %s", raw)
	}
}

func TestNodeClaudeQuotaIsNotAdvertisedAsReadOnly(t *testing.T) {
	testHome(t)
	m := RemoteMachine{ID: "node", NodeID: "paired", Modules: []string{"harness"}}
	if err := putStoreJSON(bkState, remoteMachinesState, []RemoteMachine{m}); err != nil {
		t.Fatal(err)
	}
	a := acpAgent{ID: remoteRuntimeID(m.ID, "claude-code"), Machine: m.ID, Remote: true}
	if harnessHasQuota(a) {
		t.Fatal("Node advertises unsupported Claude /usage launcher as a quota reading")
	}
}

func TestMachineOperationSupportMatrix(t *testing.T) {
	testHome(t)
	for _, tc := range []struct {
		name string
		m    RemoteMachine
		node bool
		ssh  bool
	}{
		{"ssh", RemoteMachine{ID: "ssh", User: "fixture", OS: "Linux"}, false, true},
		{"node", RemoteMachine{ID: "node", NodeID: "paired", Modules: []string{"engine", "observe", "harness", "terminal", "voice"}}, true, false},
		{"hybrid", RemoteMachine{ID: "hybrid", NodeID: "paired", User: "fixture", OS: "Linux", Modules: []string{"observe", "harness", "terminal", "engine", "voice"}}, true, true},
		{"missing modules", RemoteMachine{ID: "missing", NodeID: "paired"}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			operations := machineOperations(tc.m)
			for _, id := range []string{"agents.list", "agents.install", "agents.update", "agents.execute", "workspaces.prepare", "terminals", "metrics", "environment.check", "environment.docker", "usage.hermes", "usage.opencode", "quota.hermes"} {
				row := operations[id]
				supported := tc.node || tc.ssh
				if row.Supported != supported || row.Unsupported == supported || supported && row.Transport != map[bool]string{true: "node", false: "ssh"}[tc.node] || !supported && row.Error == "" {
					t.Errorf("%s: %+v", id, row)
				}
			}
			for _, id := range []string{"engine", "voice", "folders.browse"} {
				if operations[id].Supported != tc.node {
					t.Errorf("%s: %+v", id, operations[id])
				}
			}
			for _, id := range []string{"usage.claude-code", "usage.codex", "usage.pi", "usage.openclaw", "usage.antigravity", "usage.deepseek-harness", "usage.acp"} {
				if !operations[id].Unsupported {
					t.Errorf("unimplemented native reading %s", id)
				}
			}
		})
	}
	windows := machineOperations(RemoteMachine{ID: "win", User: "fixture", OS: "Windows"})
	for _, id := range []string{"metrics", "environment.check", "environment.docker"} {
		if !windows[id].Unsupported {
			t.Errorf("Unix-only SSH operation advertised on Windows: %s", id)
		}
	}
	m := RemoteMachine{ID: "hybrid", User: "fixture", NodeID: "paired", Modules: []string{"observe"}}
	m.Modules = nil
	if got, err := machineOperationTransport(m, "observe", true); got != "ssh" || err != nil {
		t.Fatalf("hybrid fallback unavailable: %s %v", got, err)
	}
}

func TestNodeReadOnlyObservationIntegration(t *testing.T) {
	_, worker, token := nodeHarnessFixture(t, 1)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	dir := t.TempDir()
	for _, h := range []string{"hermes", "opencode"} {
		stats := harnessUsageFixture(t, map[string]string{"hermes": "hermes_insights.txt", "opencode": "opencode_stats.txt"}[h])
		quota := harnessUsageFixture(t, "hermes_usage.txt")
		script := "#!/bin/sh\nif [ \"$1\" = usage ]; then\ncat <<'QUOTA'\n" + quota + "QUOTA\nelse\ncat <<'STATS'\n" + stats + "STATS\nfi\n"
		if err := os.WriteFile(filepath.Join(dir, h), []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	observe := newEnvironment()
	observe.run = func(ctx context.Context, name string, args []string, stdin string) ([]byte, error) {
		if name != "docker" || !reflect.DeepEqual(args, []string{"ps", "-a", "--format", "{{json .}}"}) || stdin != "" {
			t.Fatalf("arbitrary Node command: %s %+v", name, args)
		}
		return []byte(`{"Names":"fixture","Image":"fixture:v1","State":"running"}`), nil
	}
	observe.transport = envRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "http://example.invalid:80/path" {
			t.Fatalf("target changed: %s", r.URL)
		}
		w := httptest.NewRecorder()
		w.WriteHeader(302)
		return w.Result(), nil
	})
	observe.dial = func(ctx context.Context, network, address string) (net.Conn, error) {
		if network != "tcp" || address != "example.invalid:81" {
			t.Fatalf("TCP target changed: %s %s", network, address)
		}
		a, b := net.Pipe()
		b.Close()
		return a, nil
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/node/env/docker", nodeAuth(hashWebKey(token), observe.nodeDocker))
	mux.HandleFunc("/api/node/env/check", nodeAuth(hashWebKey(token), observe.nodeCheck))
	mux.Handle("/api/node/harness/usage", worker)
	old := nodeClient
	calls := 0
	nodeClient = &http.Client{Transport: envRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Header.Get("Authorization") != "Bearer "+token || strings.Contains(r.URL.String(), token) {
			t.Fatal("credential leaked")
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w.Result(), nil
	}), CheckRedirect: old.CheckRedirect}
	defer func() { nodeClient = old }()
	m, err := savePairedMachine(RemoteMachine{Name: "fixture", Host: "127.0.0.1", User: "hybrid-ssh"}, &engineNode{URL: "http://127.0.0.1:2511", WebKey: token, Role: "engine-node", NodeID: "fixture", Modules: []string{"observe", "harness"}, Handshake: 1})
	if err != nil {
		t.Fatal(err)
	}
	main := newEnvironment()
	main.run = func(context.Context, string, []string, string) ([]byte, error) {
		t.Fatal("Node operation used SSH")
		return nil, nil
	}
	main.sshKey = func() (string, string, error) {
		t.Fatal("Node operation requested SSH credentials")
		return "", "", nil
	}
	w := envTestRequest(main.docker, "GET", "/api/env/docker?machine="+m.ID, "")
	if calls != 0 || w.Code != 200 {
		t.Fatalf("disabled Docker made request: %d %s", calls, w.Body)
	}
	w = envTestRequest(main.docker, "POST", "/api/env/docker", `{"machine":"`+m.ID+`","enabled":true}`)
	if w.Code != 200 {
		t.Fatal(w.Body)
	}
	w = envTestRequest(main.docker, "GET", "/api/env/docker?machine="+m.ID, "")
	if !strings.Contains(w.Body.String(), `"name":"fixture"`) || !strings.Contains(w.Body.String(), `"transport":"node"`) {
		t.Fatal(w.Body)
	}
	for _, target := range []string{"http://example.invalid:80/path", "example.invalid:81"} {
		q := main.check(context.Background(), target, m.ID)
		if !q.OK || q.Transport != "node" || q.From != m.ID {
			t.Fatalf("Node reachability: %+v", q)
		}
	}
	for _, h := range []string{"hermes", "opencode"} {
		a := acpAgent{ID: remoteRuntimeID(m.ID, h), Machine: m.ID, Remote: true, Custom: true}
		isolateRuntimeRegistry(t, &acpAdapter{agent: a})
		q := readNativeHarnessUsage(context.Background(), a.ID, 7)
		if q.Error != "" || q.TotalTokens == 0 || q.Transport != "node" || !strings.Contains(q.Source, "Loom Node") {
			t.Fatalf("native stats: %+v", q)
		}
		if h == "hermes" {
			quota, err := readHarnessQuota(context.Background(), a)
			if err != nil || len(quota.Windows) == 0 {
				t.Fatalf("Node quota: %+v %v", quota, err)
			}
		}
	}
	for _, path := range []string{
		"/api/node/harness/usage?harness=claude-code&reading=quota",
		"/api/node/harness/usage?harness=hermes&reading=generate",
		"/api/node/harness/usage?harness=opencode&reading=stats&days=8",
	} {
		w := nodeHarnessGet(worker, path, token)
		if w.Code != 409 || !strings.Contains(w.Body.String(), `"unsupported":true`) {
			t.Fatalf("unsupported native read executed: %d %s", w.Code, w.Body)
		}
	}
	for _, path := range []string{"/api/node/env/check?target=http://u:p@example.invalid", "/api/node/env/check?target=bad"} {
		if w := nodeHarnessGet(mux, path, token); w.Code != 400 {
			t.Fatalf("invalid target accepted: %d", w.Code)
		}
	}
	for _, path := range []string{"/api/node/env/docker", "/api/node/env/check?target=example.invalid:81", "/api/node/harness/usage?harness=hermes&reading=quota"} {
		w := envTestRequest(func(w http.ResponseWriter, r *http.Request) {
			r.Header.Set("Authorization", "Bearer "+token)
			mux.ServeHTTP(w, r)
		}, "POST", path, "{}")
		if w.Code != 405 {
			t.Fatalf("method accepted: %d %s", w.Code, w.Body)
		}
	}
	observe.run = func(context.Context, string, []string, string) ([]byte, error) {
		return nil, errors.New("Docker permission denied")
	}
	w = envTestRequest(main.docker, "GET", "/api/env/docker?machine="+m.ID, "")
	if !strings.Contains(w.Body.String(), "permission denied") || !strings.Contains(w.Body.String(), `"ok":false`) {
		t.Fatalf("denied Docker shown working: %s", w.Body)
	}
	if err := putBool(bkState, nodeObserveDisabledKey, true); err != nil {
		t.Fatal(err)
	}
	w = envTestRequest(main.docker, "GET", "/api/env/docker?machine="+m.ID, "")
	if !strings.Contains(w.Body.String(), `"unsupported":true`) {
		t.Fatalf("runtime-disabled module shown working: %s", w.Body)
	}
	nodeClient.Transport = envRoundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("offline") })
	q := main.check(context.Background(), "example.invalid:81", m.ID)
	if q.OK || q.Error != "node machine unreachable" {
		t.Fatalf("offline shown working: %+v", q)
	}
}

func TestNodeHistorySourceDoesNotRequireSSHKeygen(t *testing.T) {
	testHome(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	m := RemoteMachine{ID: "node-history", NodeID: "paired", Modules: []string{"harness"}, Tools: []RemoteTool{{ID: "hermes", Path: "/fixture/hermes"}}}
	if err := putStoreJSON(bkState, remoteMachinesState, []RemoteMachine{m}); err != nil {
		t.Fatal(err)
	}
	if err := setHarnessManaged(m.ID, "hermes", true); err != nil {
		t.Fatal(err)
	}
	a, err := historySource("remote:" + m.ID + ":hermes")
	if err != nil || len(a.Args) != 4 || a.Args[0] != "node-bridge" {
		t.Fatalf("Node history requires SSH: %+v %v", a, err)
	}
}

func TestNodeUsageOutputAndCancellationBounds(t *testing.T) {
	_, mux, token := nodeHarnessFixture(t, 1)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	dir := t.TempDir()
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := os.WriteFile(filepath.Join(dir, "hermes"), []byte("#!/bin/sh\ncat "+shellQuote(filepath.Join(dir, "output"))+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "output"), []byte(strings.Repeat("x", (128<<10)+1)), 0600); err != nil {
		t.Fatal(err)
	}
	w := nodeHarnessGet(mux, "/api/node/harness/usage?harness=hermes&reading=stats&days=7", token)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"ok":false`) || !strings.Contains(w.Body.String(), "exceeds 128 KiB") || strings.Contains(w.Body.String(), strings.Repeat("x", 20)) {
		t.Fatalf("oversized native output leaked: %d %.200s", w.Code, w.Body)
	}
	r := httptest.NewRequest("GET", "/api/node/harness/usage?harness=hermes&reading=quota", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	ctx, cancel := context.WithCancel(r.Context())
	cancel()
	r = r.WithContext(ctx)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if strings.Contains(w.Body.String(), `"ok":true`) {
		t.Fatal("cancelled read reported success", w.Body)
	}
}

func TestNodeDegradedObservationDoesNotFabricateSuccess(t *testing.T) {
	testHome(t)
	m, err := savePairedMachine(RemoteMachine{Name: "degraded", Host: "127.0.0.1"}, &engineNode{URL: "http://127.0.0.1:2511", WebKey: "fixture-token", Role: "engine-node", NodeID: "degraded", Modules: []string{"observe"}, Handshake: 1})
	if err != nil {
		t.Fatal(err)
	}
	old := nodeClient
	nodeClient = &http.Client{Transport: envRoundTripFunc(func(*http.Request) (*http.Response, error) {
		w := httptest.NewRecorder()
		w.WriteString(`{"ok":true}`)
		return w.Result(), nil
	})}
	defer func() { nodeClient = old }()
	e := newEnvironment()
	q := e.check(context.Background(), "example.invalid:80", m.ID)
	if q.OK || q.Error == "" {
		t.Fatalf("missing check observation reported success: %+v", q)
	}
	rows, err := e.readDocker(context.Background(), &m)
	if err == nil {
		t.Fatalf("missing container observation reported empty success: %+v", rows)
	}
}

func TestHybridLifecycleUsesSSHWhenHarnessModuleIsAbsent(t *testing.T) {
	testHome(t)
	isolatePhase4b(t)
	m := RemoteMachine{ID: "hybrid-lifecycle", NodeID: "paired", User: "fixture", Host: "test.invalid", OS: "Linux", Modules: []string{"observe"}}
	if err := putStoreJSON(bkState, remoteMachinesState, []RemoteMachine{m}); err != nil {
		t.Fatal(err)
	}
	s := newHarnessLifecycleService()
	calls := 0
	updates := 0
	s.run = func(_ context.Context, target *RemoteMachine, argv []string) (string, error) {
		calls++
		if target == nil || target.ID != m.ID {
			t.Fatalf("wrong lifecycle transport target: %+v", target)
		}
		if argv[0] == "command" {
			return "/fixture/bin/" + argv[2], nil
		}
		if len(argv) > 1 && argv[1] == "view" {
			return "2.0.0", nil
		}
		if len(argv) > 1 && argv[1] == "install" {
			updates++
			return "updated", nil
		}
		return "1.0.0", nil
	}
	s.installation = func(_ context.Context, _ *RemoteMachine, _ inspectSpec, path string) (harnessInstallation, error) {
		return harnessInstallation{Channel: "npm", Path: path, Prefix: "/fixture"}, nil
	}
	s.refresh = func(context.Context, *RemoteMachine, string) error { return nil }
	s.active = func(string, string) bool { return false }
	for _, action := range []string{"check", "update"} {
		state, err := s.action(context.Background(), m.ID, "codex", action)
		if err != nil || !state.Installed {
			t.Fatalf("%s contradicts supported SSH operation: %+v %v", action, state, err)
		}
	}
	if calls == 0 || updates != 1 {
		t.Fatalf("lifecycle did not use saved SSH transport: calls=%d updates=%d", calls, updates)
	}
}
