package loom

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestACPAgentHelper(t *testing.T) {
	if os.Getenv("LOOM_TEST_FAKE_ACP") != "1" {
		return
	}
	if file := os.Getenv("LOOM_TEST_ACP_CHILD_PID_FILE"); file != "" {
		child := exec.Command(os.Args[0], "-test.run=^TestACPChildHelper$")
		if err := child.Start(); err != nil {
			os.Exit(2)
		}
		_ = os.WriteFile(file, []byte(strconv.Itoa(child.Process.Pid)), 0600)
	}
	var modes map[string]any
	if os.Getenv("LOOM_TEST_FAKE_CODEX_MODES") == "1" {
		modes = map[string]any{"currentModeId": "workspace-write", "availableModes": []any{map[string]any{"id": "workspace-write", "name": "Workspace"}, map[string]any{"id": "agent-full-access", "name": "Full"}}}
	}
	runFakeACPWithModes(os.Stdin, os.Stdout, os.Getenv("LOOM_TEST_FAKE_NO_LOAD") != "1", modes)
	os.Exit(0)
}
func fakeACPAdapter(t *testing.T) *acpAdapter {
	t.Helper()
	t.Setenv("LOOM_TEST_FAKE_ACP", "1")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return &acpAdapter{agent: acpAgent{ID: "test-acp", Name: "Fixture", Command: executable, Args: []string{"-test.run=^TestACPAgentHelper$"}}}
}
func createACPSession(t *testing.T, m *runtimeSessions, a *acpAdapter, policy string) RuntimeSession {
	t.Helper()
	s, err := m.create("", "", false)
	if err != nil {
		t.Fatal(err)
	}
	s.RuntimeID = a.agent.ID
	s.Model = ""
	s.Workdir = t.TempDir()
	s.Permission = policy
	if err = putStoreJSON(bkRuntimeSessions, s.ID, s); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.shutdownACP)
	return s
}
func waitACPTurn(t *testing.T, m *runtimeSessions, id string) RuntimeSession {
	t.Helper()
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		s, ok := m.get(id)
		if ok && s.Status != "running" {
			if s.Status != "complete" {
				t.Fatalf("ACP turn: %s / %s", s.Status, s.Error)
			}
			return s
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("ACP turn timeout")
	return RuntimeSession{}
}
func TestACPProcessTurnPersistenceAndLoad(t *testing.T) {
	testHome(t)
	a := fakeACPAdapter(t)
	isolateRuntimeRegistry(t, a)
	m := newRuntimeSessions()
	s := createACPSession(t, m, a, "edits")
	if err := m.start(s.ID, "request-001", "fixture"); err != nil {
		t.Fatal(err)
	}
	first := waitACPTurn(t, m, s.ID)
	if first.Messages[1].Content != "OK" || first.NativeSessionID == "" || first.NativeContext == "" {
		t.Fatalf("portable/native state: %+v", first)
	}
	if len(first.Files) != 1 || first.Files[0].Add != 1 || first.Files[0].Op != "create" {
		t.Fatalf("files: %+v", first.Files)
	}
	if _, err := os.Stat(filepath.Join(s.Workdir, "..", "loom-fake-acp-outside.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("outside write accepted")
	}
	kinds := map[any]bool{}
	for _, e := range first.Turns[0].ACPEvents {
		kinds[e["type"]] = true
		if e["type"] == "tool_end" {
			tool := e["tool"].(map[string]any)
			if tool["title"] != "Write fixture" || tool["output"] != "Fixture result" || len(tool["diffs"].([]any)) != 1 {
				t.Fatalf("partial tool update lost fields: %+v", tool)
			}
		}
		if e["type"] == "approval_resolved" && (e["option_id"] != "once" || e["auto"] != true) {
			t.Fatal("automatic permission did not select allow_once")
		}
	}
	for _, kind := range []string{"text_delta", "reasoning_delta", "tool_start", "tool_delta", "tool_end", "plan", "usage", "mode", "config", "commands", "files", "approval_resolved"} {
		if !kinds[kind] {
			t.Fatalf("missing %s", kind)
		}
	}
	replay := runtimeReplay(first)
	text := ""
	for _, e := range replay {
		if e["type"] == "text_delta" {
			text += e["text"].(string)
		}
	}
	if text != "OK" {
		t.Fatal("replay duplicated text")
	}
	m.acpMu.Lock()
	process := m.acp[s.ID]
	m.acpMu.Unlock()
	if err := m.start(s.ID, "request-002", "next"); err != nil {
		t.Fatal(err)
	}
	second := waitACPTurn(t, m, s.ID)
	m.acpMu.Lock()
	same := m.acp[s.ID] == process
	m.acpMu.Unlock()
	if !same || second.NativeSessionID != first.NativeSessionID {
		t.Fatal("one active discussion spawned another process/session")
	}
	m.closeACP(s.ID)
	if err := m.start(s.ID, "request-003", "resume"); err != nil {
		t.Fatal(err)
	}
	third := waitACPTurn(t, m, s.ID)
	if third.NativeSessionID != first.NativeSessionID {
		t.Fatal("loadSession did not restore native ID")
	}
	for _, e := range third.Turns[2].ACPEvents {
		if e["type"] == "text_delta" && strings.Contains(e["text"].(string), "OLD REPLAY") {
			t.Fatal("native load replay duplicated cards")
		}
	}
	m.acpMu.Lock()
	last := m.acp[s.ID]
	m.acpMu.Unlock()
	if err := m.remove(s.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-last.client.done:
	case <-time.After(time.Second):
		t.Fatal("delete left agent alive")
	}
}
func TestACPAskRoutingAndCancellation(t *testing.T) {
	for _, cancelTurn := range []bool{false, true} {
		t.Run(map[bool]string{false: "answer", true: "cancel"}[cancelTurn], func(t *testing.T) {
			testHome(t)
			a := fakeACPAdapter(t)
			isolateRuntimeRegistry(t, a)
			m := newRuntimeSessions()
			s := createACPSession(t, m, a, "ask")
			sub := &discussionSubscriber{events: make(chan DiscussionEvent, 128)}
			m.mu.Lock()
			m.subscribers[s.ID] = map[*discussionSubscriber]bool{sub: true}
			m.mu.Unlock()
			if err := m.start(s.ID, "request-ask", "fixture"); err != nil {
				t.Fatal(err)
			}
			var approval string
			deadline := time.After(5 * time.Second)
		loop:
			for {
				select {
				case e := <-sub.events:
					if e["type"] == "approval_request" {
						approval = e["approval"].(map[string]any)["id"].(string)
						break loop
					}
				case <-deadline:
					t.Fatal("permission request missing")
				}
			}
			if err := m.answerACP(s.ID, approval, "invented", false); err == nil {
				t.Fatal("unknown option accepted")
			}
			if cancelTurn {
				_ = m.stop(s.ID)
				deadline := time.Now().Add(4 * time.Second)
				for time.Now().Before(deadline) {
					got, _ := m.get(s.ID)
					if got.Status == "cancelled" {
						found := false
						for _, e := range got.Turns[0].ACPEvents {
							if e["type"] == "approval_resolved" && e["option_id"] == "" {
								found = true
							}
						}
						if !found {
							t.Fatal("cancel left unresolved approval in replay")
						}
						return
					}
					time.Sleep(5 * time.Millisecond)
				}
				t.Fatal("cancel did not complete")
			}
			if err := m.answerACP(s.ID, approval, "always", false); err != nil {
				t.Fatal(err)
			}
			got := waitACPTurn(t, m, s.ID)
			if len(got.Files) != 1 {
				t.Fatal("approved write not recorded")
			}
			if err := m.answerACP(s.ID, approval, "always", false); err == nil {
				t.Fatal("approval replayed twice")
			}
		})
	}
}
func TestACPPermissionPolicies(t *testing.T) {
	options := []map[string]any{{"optionId": "permanent", "kind": "allow_always"}, {"optionId": "once", "kind": "allow_once"}}
	for _, policy := range []string{"ask", "edits", "full"} {
		for _, kind := range []string{"read", "search", "edit", "think", "fetch", "execute", "delete", "move", "other"} {
			got := acpAutoOption(policy, kind, options)
			want := ""
			if policy == "full" || policy == "edits" && (kind == "read" || kind == "search" || kind == "edit" || kind == "think" || kind == "fetch") {
				want = "once"
			}
			if got != want {
				t.Fatalf("%s/%s: %s", policy, kind, got)
			}
		}
	}
	if acpAutoOption("full", "edit", options[:1]) != "" {
		t.Fatal("automatic allow_always")
	}
}
func TestACPFilesystemConfinementAndDiff(t *testing.T) {
	dir, outside := t.TempDir(), t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	path := filepath.Join(dir, "file.txt")
	_ = os.WriteFile(path, []byte("one\ntwo\n"), 0600)
	p := &acpBinding{roots: []*os.Root{root}, state: ACPState{Workdir: dir}, emit: func(StreamEvent) bool { return true }}
	if _, err = p.writeFile(path, "one\nnew\ntwo\n"); err != nil {
		t.Fatal(err)
	}
	if p.state.Files[0].Add != 1 || p.state.Files[0].Del != 0 {
		t.Fatal(p.state.Files)
	}
	if _, err = p.writeFile(path, "one\nnew\n"); err != nil {
		t.Fatal(err)
	}
	if p.state.Files[0].Add != 0 || p.state.Files[0].Del != 1 {
		t.Fatal(p.state.Files)
	}
	if !strings.Contains(acpUnifiedDiff(path, p.state.FileBaselines[path], "one\nnew\n"), "-two\n+new\n") {
		t.Fatal("line diff incorrect")
	}
	for _, path := range []string{filepath.Join(outside, "escape"), filepath.Join(dir, "..", "escape"), "relative"} {
		if _, err = p.writeFile(path, "x"); err == nil {
			t.Fatal("outside/relative path accepted")
		}
	}
	if err = os.Symlink(outside, filepath.Join(dir, "escape")); err == nil {
		if _, err = p.writeFile(filepath.Join(dir, "escape", "file"), "x"); err == nil {
			t.Fatal("symlink escape")
		}
		if _, err = p.readFile(filepath.Join(dir, "escape", "file"), nil, nil); err == nil {
			t.Fatal("symlink read escape")
		}
	}
	if err = os.Symlink(path, filepath.Join(dir, "inside-link")); err == nil {
		if _, err = p.readFile(filepath.Join(dir, "inside-link"), nil, nil); err != nil {
			t.Fatal("inside symlink rejected", err)
		}
	}
	line, limit := 2, 1
	content, err := p.readFile(path, &line, &limit)
	if err != nil || content.(map[string]any)["content"] != "new\n" {
		t.Fatal("read line/limit", content, err)
	}
	if _, err = acpDirectory(path); err == nil {
		t.Fatal("file used as workdir")
	}
	if _, err = acpDirectory("."); err == nil {
		t.Fatal("relative workdir")
	}
}
func TestACPHTTPConfigureFilesAndAuth(t *testing.T) {
	testHome(t)
	a := fakeACPAdapter(t)
	isolateRuntimeRegistry(t, a)
	old := workspaceSessions
	workspaceSessions = newRuntimeSessions()
	t.Cleanup(func() { workspaceSessions.shutdownACP(); workspaceSessions = old })
	s, _ := workspaceSessions.create("", "", false)
	key := "synthetic-acp-key"
	_ = storeWebKey(key)
	mux := http.NewServeMux()
	for path, h := range map[string]http.HandlerFunc{"/api/runtime/sessions/configure": handleRuntimeSessionConfigure, "/api/runtime/sessions/files": handleACPFiles, "/api/runtime/sessions/diff": handleACPDiff, "/api/runtime/sessions/approval": handleACPApproval, "/api/fs/dirs": handleACPDirs, "/api/runtimes": handleACPRuntimes} {
		mux.HandleFunc(path, requireWebAuth(h))
	}
	call := func(method, path, body string, auth bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if method == "POST" {
			r.Header.Set("Content-Type", "application/json")
		}
		if auth {
			r.Header.Set("Authorization", "Bearer "+key)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	dir := t.TempDir()
	body := acpCompact(map[string]any{"id": s.ID, "workdir": dir, "permission": "edits"}, 4096)
	if w := call("POST", "/api/runtime/sessions/configure", body, false); w.Code != 401 {
		t.Fatal("missing auth", w.Code)
	}
	if w := call("POST", "/api/runtime/sessions/configure", body, true); w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("configure", w.Code, w.Body.String())
	}
	got, _ := workspaceSessions.get(s.ID)
	if got.Workdir != dir || got.Permission != "edits" {
		t.Fatal(got)
	}
	for _, body := range []string{`{"id":"` + s.ID + `","permission":"full"}`, `{"id":"` + s.ID + `","workdir":"relative"}`, `{"id":"` + s.ID + `","permission":"invalid"}`, `{"id":"` + s.ID + `","config":{"x":12}}`, `{"id":"` + s.ID + `","unknown":true}`} {
		if w := call("POST", "/api/runtime/sessions/configure", body, true); w.Code < 400 {
			t.Fatal("invalid config", body)
		}
	}
	if w := call("POST", "/api/runtime/sessions/configure", `{"id":"`+s.ID+`","permission":"full","consent":true}`, true); w.Code != 200 {
		t.Fatal("full consent", w.Body.String())
	}
	for _, path := range []string{"/api/runtimes", "/api/runtime/sessions/files?id=" + s.ID, "/api/fs/dirs?path=" + dir} {
		if w := call("GET", path, "", false); w.Code != 401 {
			t.Fatal("read auth", path, w.Code)
		}
		if w := call("GET", path, "", true); w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("read route", path, w.Code)
		}
	}
}
func TestACPMCPWireDefinitions(t *testing.T) {
	testHome(t)
	executable, _ := os.Executable()
	servers := map[string]MCPServerConfig{"stdio": {Command: executable, Args: []string{"--fixture"}, Env: map[string]string{"FIXTURE_SECRET": "private-value"}, Enabled: true}, "http": {URL: "https://example.test/mcp", Headers: map[string]string{"Authorization": "private-header"}, Enabled: true}, "disabled": {Command: "missing", Enabled: false}}
	mcpConfigMu.Lock()
	err := saveMCPConfigLocked(servers)
	mcpConfigMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	wire, err := acpMCPServers(map[string]any{"mcpCapabilities": map[string]any{"http": true}})
	if err != nil || len(wire) != 2 {
		t.Fatal(wire, err)
	}
	stdio := wire[1].(map[string]any)
	env := stdio["env"].([]any)
	if !reflect.DeepEqual(env, []any{map[string]string{"name": "FIXTURE_SECRET", "value": "private-value"}}) || stdio["command"] != executable {
		t.Fatal("wrong MCP env shape", stdio)
	}
	if _, err = acpMCPServers(map[string]any{}); err == nil {
		t.Fatal("HTTP without negotiated capability")
	}
	state, _ := json.Marshal(ACPState{})
	if strings.Contains(string(state), "private-value") {
		t.Fatal("MCP credential persisted")
	}
}

func TestACPApprovalSubscriberLifetime(t *testing.T) {
	m := newRuntimeSessions()
	id := "grace-fixture"
	sub := &discussionSubscriber{events: make(chan DiscussionEvent, 1)}
	m.subscribers[id] = map[*discussionSubscriber]bool{sub: true}
	p := &acpBinding{manager: m, id: id, state: ACPState{Permission: "ask"}, client: &acpClient{done: make(chan struct{})}, tools: map[string]map[string]any{}, approvals: map[string]*acpApproval{}, approvalGrace: 25 * time.Millisecond, emit: func(StreamEvent) bool { return true }}
	result := make(chan any, 1)
	go func() {
		r, _ := p.permission(context.Background(), map[string]any{"toolCallId": "tool", "kind": "execute"}, []map[string]any{{"optionId": "once", "kind": "allow_once"}})
		result <- r
	}()
	select {
	case <-result:
		t.Fatal("subscribed approval expired")
	case <-time.After(100 * time.Millisecond):
	}
	m.mu.Lock()
	delete(m.subscribers, id)
	m.mu.Unlock()
	select {
	case raw := <-result:
		if raw.(map[string]any)["outcome"].(map[string]any)["outcome"] != "cancelled" {
			t.Fatal("absent UI not auto-rejected")
		}
	case <-time.After(time.Second):
		t.Fatal("absent UI approval did not expire")
	}
}
func TestACPConfigureNativeModeAndScope(t *testing.T) {
	testHome(t)
	a := fakeACPAdapter(t)
	isolateRuntimeRegistry(t, a)
	m := newRuntimeSessions()
	s := createACPSession(t, m, a, "full")
	if err := m.start(s.ID, "request-setup", "fixture"); err != nil {
		t.Fatal(err)
	}
	s = waitACPTurn(t, m, s.ID)
	mode := "plan"
	configured, err := m.configureDiscussion(s.ID, s.Title, s.ProjectID, s.Instructions, discussionContext(s).Revision, false, acpConfiguration{Mode: &mode, Config: map[string]any{"verbosity": "long"}})
	if err != nil || configured.Mode != "plan" || configured.ConfigOptions["verbosity"] != "long" {
		t.Fatal("mode/config not applied", configured, err)
	}
	dir := t.TempDir()
	m.acpMu.Lock()
	p := m.acp[s.ID]
	m.acpMu.Unlock()
	changed, err := m.configureDiscussion(s.ID, s.Title, s.ProjectID, s.Instructions, discussionContext(configured).Revision, false, acpConfiguration{Workdir: &dir})
	if err != nil || changed.NativeSessionID != "" || changed.Workdir != dir || len(changed.Files) != 0 {
		t.Fatal("changed scope did not clear native binding", err)
	}
	select {
	case <-p.client.done:
	default:
		t.Fatal("changed workdir left old process alive")
	}
	if !hasRuntimeCapability(a.Descriptor(), "resume") {
		t.Fatal("negotiated resume missing")
	}
}

func TestACPChildHelper(t *testing.T) {
	if os.Getenv("LOOM_TEST_ACP_CHILD_PID_FILE") == "" {
		return
	}
	for {
		time.Sleep(time.Hour)
	}
}
func TestACPNonResumablePortableHandoff(t *testing.T) {
	testHome(t)
	a := fakeACPAdapter(t)
	t.Setenv("LOOM_TEST_FAKE_NO_LOAD", "1")
	isolateRuntimeRegistry(t, a)
	m := newRuntimeSessions()
	s := createACPSession(t, m, a, "edits")
	if err := m.start(s.ID, "request-history1", "question-history-control"); err != nil {
		t.Fatal(err)
	}
	first := waitACPTurn(t, m, s.ID)
	m.closeACP(s.ID)
	if err := m.start(s.ID, "request-history2", "__loom_inspect_portable"); err != nil {
		t.Fatal(err)
	}
	second := waitACPTurn(t, m, s.ID)
	prompt, _ := second.Messages[3].Content.(string)
	if first.NativeSessionID == second.NativeSessionID || !strings.Contains(prompt, "question-history-control") || !strings.Contains(prompt, `"role":"assistant","content":"OK"`) {
		t.Fatal("new native session did not receive portable history", prompt)
	}
	if strings.Contains(prompt, "Reported thought") || strings.Contains(prompt, "Fixture result") {
		t.Fatal("runtime-private state entered handoff")
	}
	if hasRuntimeCapability(a.Descriptor(), "resume") {
		t.Fatal("unsupported resume advertised")
	}
}
func TestACPCancelNotificationRoundtrip(t *testing.T) {
	a := fakeACPAdapter(t)
	c, err := startACPClient(a.agent.Command, a.agent.Args, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ready := make(chan struct{}, 1)
	c.notify = func(f acpFrame) {
		if f.Method == "session/update" {
			select {
			case ready <- struct{}{}:
			default:
			}
		}
	}
	if err = c.start(); err != nil {
		t.Fatal(err)
	}
	defer c.close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err = c.call(ctx, "initialize", map[string]any{"protocolVersion": 1, "clientInfo": map[string]any{"name": "loom", "version": Version}, "clientCapabilities": map[string]any{"fs": map[string]any{"readTextFile": true, "writeTextFile": true}, "terminal": false}}, nil); err != nil {
		t.Fatal(err)
	}
	var response acpSessionResponse
	if err = c.call(ctx, "session/new", map[string]any{"cwd": t.TempDir(), "mcpServers": []any{}}, &response); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		var out struct {
			StopReason string `json:"stopReason"`
		}
		err := c.call(ctx, "session/prompt", map[string]any{"sessionId": response.SessionID, "prompt": []any{map[string]any{"type": "text", "text": "wait"}}}, &out)
		if err == nil && out.StopReason != "cancelled" {
			err = errors.New("prompt not cancelled")
		}
		result <- err
	}()
	select {
	case <-ready:
	case <-ctx.Done():
		t.Fatal("waiting prompt not observed")
	}
	if err = c.notification("session/cancel", map[string]any{"sessionId": response.SessionID}); err != nil {
		t.Fatal(err)
	}
	if err = <-result; err != nil {
		t.Fatal(err)
	}
}

func TestACPMCPProjectAndSessionSelection(t *testing.T) {
	testHome(t)
	executable, _ := os.Executable()
	mcpConfigMu.Lock()
	err := saveMCPConfigLocked(map[string]MCPServerConfig{"first": {Command: executable, Enabled: true}, "second": {Command: executable, Enabled: true}, "disabled": {Command: executable, Enabled: false}})
	mcpConfigMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	names := []string{"first"}
	project, err := saveProjectContext(ChatProject{Name: "Scoped MCP", MCPServers: &names})
	if err != nil {
		t.Fatal(err)
	}
	s := RuntimeSession{ProjectID: project.ID}
	definitions, err := acpSessionMCPDefinitions(s)
	if err != nil || len(definitions) != 1 || definitions["first"].Command == "" {
		t.Fatal("project scope", definitions, err)
	}
	explicit := []string{"second", "disabled"}
	s.MCPServers = &explicit
	definitions, err = acpSessionMCPDefinitions(s)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := acpMCPServersFromDefinitions(map[string]any{}, definitions)
	if err != nil || len(wire) != 1 || wire[0].(map[string]any)["name"] != "second" {
		t.Fatal("session scope/enabled filter", wire, err)
	}
	none := []string{}
	s.MCPServers = &none
	definitions, err = acpSessionMCPDefinitions(s)
	if err != nil || len(definitions) != 0 {
		t.Fatal("empty scope inherited all servers")
	}
	if err = validateACPMCPSelection([]string{"unknown"}); err == nil {
		t.Fatal("unknown MCP scope accepted")
	}
}
