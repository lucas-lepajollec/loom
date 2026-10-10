package loom

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/capability"
	"github.com/lucas-lepajollec/loom/internal/loom/policy"
)

func TestNodeHarnessLifecycleControllerAndPolicy(t *testing.T) {
	server, mux, token := nodeHarnessFixture(t, 8)
	isolatePhase4b(t)
	s := newHarnessLifecycleService()
	server.lifecycle = newHarnessLifecycleService()
	server.lifecycle.installation = func(_ context.Context, _ *RemoteMachine, _ inspectSpec, path string) (harnessInstallation, error) {
		return harnessInstallation{Channel: "npm", Path: path, Prefix: "/node"}, nil
	}
	version := "codex-cli 1.0.0"
	updates := 0
	fail := false
	server.lifecycle.run = func(ctx context.Context, m *RemoteMachine, argv []string) (string, error) {
		if m != nil {
			t.Fatal("node lifecycle must execute locally as node user")
		}
		switch {
		case argv[0] == "command":
			return "/node/bin/" + argv[2], nil
		case argv[1] == "--version":
			return version, nil
		case argv[1] == "view":
			return "2.0.0", nil
		case argv[1] == "install":
			updates++
			if fail {
				return "npm error verbatim\n", errors.New("npm failed")
			}
			version = "codex-cli 2.0.0"
			return "node updated\n", nil
		}
		t.Fatalf("unexpected command %q", argv)
		return "", nil
	}
	server.lifecycle.refresh = func(context.Context, *RemoteMachine, string) error { return nil }
	refreshes := 0
	s.refresh = func(context.Context, *RemoteMachine, string) error { refreshes++; return nil }
	old := nodeClient
	requests := 0
	nodeClient = &http.Client{Transport: envRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.Header.Get("Authorization") != "Bearer "+token || strings.Contains(r.URL.String(), token) {
			t.Error("credential outside authorization header")
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w.Result(), nil
	})}
	t.Cleanup(func() { nodeClient = old })
	m, err := savePairedMachine(RemoteMachine{Name: "paired", Host: "127.0.0.1"}, &engineNode{URL: "http://127.0.0.1:2511", WebKey: token, Role: "engine-node", NodeID: "node-fixture", Modules: []string{"engine", "harness"}, Handshake: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"check", "install", "update"} {
		state, err := s.action(t.Context(), m.ID, "codex", action)
		if err != nil || state.Target != m.ID || state.Path != "/node/bin/codex" || !state.Installed || !state.CanUpdate {
			t.Fatalf("%s: %+v %v", action, state, err)
		}
	}
	if updates != 2 || refreshes != 2 {
		t.Fatal(updates, refreshes)
	}
	for _, action := range []string{"install", "update"} {
		savePolicyRules(t, policy.Rule{ID: "deny-" + action, Scope: "machine", ScopeID: m.ID, Subject: "node." + action, Decision: policy.Deny})
		before := requests
		if _, err := s.action(t.Context(), m.ID, "codex", action); err == nil || requests != before {
			t.Fatal("node contacted before controller policy allowed mutation", err)
		}
	}
	savePolicyRules(t)
	fail = true
	state, err := s.action(t.Context(), m.ID, "codex", "update")
	if err == nil || state.Log != "npm error verbatim\n" || !strings.Contains(err.Error(), "npm failed") || state.Version != version {
		t.Fatalf("lost node failure: %+v %v", state, err)
	}
	if !server.lifecycle.acquire("local", "codex") {
		t.Fatal("busy")
	}
	defer server.lifecycle.release("local", "codex")
	_, err = s.action(t.Context(), m.ID, "codex", "check")
	var actionErr runtimeActionError
	if !errors.As(err, &actionErr) || actionErr.status != 409 {
		t.Fatalf("lost contention: %v", err)
	}
}

func TestNodeHarnessLifecycleEndpointBoundaries(t *testing.T) {
	_, mux, token := nodeHarnessFixture(t, 8)
	for _, tc := range []struct {
		token, body string
		status      int
	}{
		{"", `{"id":"codex","action":"update"}`, 401},
		{"inference-key", `{"id":"codex","action":"update"}`, 401},
		{token, `{"id":"unknown","action":"install"}`, 404},
		{token, `{"id":"codex","action":"delete"}`, 400},
		{token, `{"id":"codex","action":"update","command":"arbitrary"}`, 400},
	} {
		r := httptest.NewRequest("POST", "/api/node/harness/lifecycle", strings.NewReader(tc.body))
		r.Header.Set("Authorization", "Bearer "+tc.token)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	_ = putBool(bkState, nodeHarnessDisabledKey, true)
	r := httptest.NewRequest("POST", "/api/node/harness/lifecycle", strings.NewReader(`{"id":"codex","action":"update"}`))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 409 || !strings.Contains(w.Body.String(), nodeHarnessDisabled) {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestNodeHarnessLifecycleShutdownCancellation(t *testing.T) {
	s, mux, token := nodeHarnessFixture(t, 8)
	started := make(chan struct{})
	s.lifecycle.run = func(ctx context.Context, _ *RemoteMachine, _ []string) (string, error) {
		close(started)
		<-ctx.Done()
		return "", ctx.Err()
	}
	done := make(chan struct{})
	go func() {
		r := httptest.NewRequest("POST", "/api/node/harness/lifecycle", strings.NewReader(`{"id":"codex","action":"check"}`))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		mux.ServeHTTP(httptest.NewRecorder(), r)
		close(done)
	}()
	<-started
	s.stop()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("node shutdown left lifecycle running")
	}
	if !s.lifecycle.acquire("local", "codex") {
		t.Fatal("node lifecycle lock retained")
	}
	s.lifecycle.release("local", "codex")
}

func TestHarnessAntigravityCheckAndUpdateResult(t *testing.T) {
	testHome(t)
	spec, _ := harnessInspectSpec("antigravity")
	argv, err := lifecycleActionCommand(spec, "unix", "update")
	if err != nil || !reflect.DeepEqual(argv, []string{"agy", "update"}) {
		t.Fatal(argv, err)
	}
	for _, changed := range []bool{false, true} {
		s := newHarnessLifecycleService()
		s.installation = func(_ context.Context, _ *RemoteMachine, _ inspectSpec, path string) (harnessInstallation, error) {
			return harnessInstallation{Channel: "native", Path: path}, nil
		}
		s.preserve = func(string) (func(bool) error, error) { return func(bool) error { return nil }, nil }
		version := "agy 1.0.0"
		s.run = func(_ context.Context, _ *RemoteMachine, argv []string) (string, error) {
			if argv[0] == "command" {
				return "/node/bin/" + argv[2], nil
			}
			if argv[1] == "update" {
				if changed {
					version = "agy 1.1.0"
				}
				return "native update log", nil
			}
			return version, nil
		}
		s.refresh = func(context.Context, *RemoteMachine, string) error { return nil }
		state, err := s.action(t.Context(), "local", "antigravity", "update")
		want := "unchanged"
		if changed {
			want = "updated"
		}
		if err != nil || state.Result != want || state.FromVersion != "agy 1.0.0" || state.Version != version || !state.CheckUpdate || !state.CanUpdate || state.UpdateAvailable || state.Latest != "" {
			t.Fatal(state, err)
		}
	}
}

func TestHarnessUpdateInvalidatesObservations(t *testing.T) {
	testHome(t)
	_, path, _ := fakeHarnessNPM(t, ".local")
	info, _ := os.Stat(path)
	a := acpAgent{ID: "codex", Command: "npx"}
	if nativeAgentProtocol(a) != "app-server" {
		t.Fatal("fixture protocol")
	}
	// Rewrite help support while retaining exactly the same stat identity.
	resolved, _ := filepath.EvalSymlinks(path)
	content, _ := os.ReadFile(resolved)
	content = []byte(strings.ReplaceAll(string(content), "app-server", "no-support"))
	if err := os.WriteFile(resolved, content, 0755); err != nil {
		t.Fatal(err)
	}
	_ = os.Chtimes(resolved, info.ModTime(), info.ModTime())
	if nativeAgentProtocol(a) != "app-server" {
		t.Fatal("cache fixture changed identity")
	}
	_ = putStoreJSON(bkState, acpProbeKey+a.ID, acpProbe{At: 1})
	_ = putStoreJSON(bkState, "agent_compat_"+a.ID, map[string]string{"protocol": "app-server"})
	if err := invalidateHarnessAgentObservations(a); err != nil {
		t.Fatal(err)
	}
	if nativeAgentProtocol(a) != "" {
		t.Fatal("stale protocol retained")
	}
	if p, ok := cachedAgentProbe(a.ID); ok {
		t.Fatal(p)
	}
	var record map[string]any
	if getStoreJSON(bkState, "agent_compat_"+a.ID, &record) {
		t.Fatal("stale compatibility retained")
	}
	for _, c := range []string{"models", "resume", "session-list", "history-import"} {
		degradedCapabilities.Observe("agent:"+a.ID, c, false, "handshake_failed", time.Now())
	}
	observeAgentProbe(a.ID, acpProbe{CapabilityChecks: []capability.Probe{{Capability: "models", OK: true}}})
	if len(degradedCapabilities.Snapshot("agent:"+a.ID)) != 3 {
		t.Fatal("models probe recovered unchecked history/resume")
	}
	for _, c := range []string{"resume", "session-list", "history-import"} {
		observeAgentProbe(a.ID, acpProbe{CapabilityChecks: []capability.Probe{{Capability: c, OK: true}}})
	}
	if len(degradedCapabilities.Snapshot("agent:"+a.ID)) != 0 {
		t.Fatal("targeted recovery failed")
	}
}

func TestNodeHarnessDetectsChannelsAndRepairsNative(t *testing.T) {
	server, mux, token := nodeHarnessFixture(t, 8)
	isolatePhase4b(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", "/usr/bin:/bin")
	oldClient := nodeClient
	nodeClient = &http.Client{Transport: envRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			t.Fatal("node credential missing")
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w.Result(), nil
	})}
	t.Cleanup(func() { nodeClient = oldClient })
	m, err := savePairedMachine(RemoteMachine{Name: "paired", Host: "127.0.0.1"}, &engineNode{URL: "http://127.0.0.1:2511", WebKey: token, Role: "engine-node", NodeID: "channels", Modules: []string{"engine", "harness"}, Handshake: 1})
	if err != nil {
		t.Fatal(err)
	}
	controller := newHarnessLifecycleService()
	controller.refresh = func(context.Context, *RemoteMachine, string) error { return nil }
	server.lifecycle.refresh = func(context.Context, *RemoteMachine, string) error { return nil }
	actualRun := server.lifecycle.run
	server.lifecycle.run = func(ctx context.Context, m *RemoteMachine, argv []string) (string, error) {
		if argv[0] == "npm" && argv[1] == "view" {
			return "2.0.0", nil
		}
		return actualRun(ctx, m, argv)
	}
	launcher := filepath.Join(home, ".local/bin/codex")
	for _, tc := range []struct{ channel, path string }{
		{"native", ".codex/packages/standalone/current/bin/codex"},
		{"npm", ".local/lib/node_modules/@openai/codex/cli"},
		{"homebrew", ".linuxbrew/Caskroom/codex/1.0/codex"},
		{"unknown", "manual/codex"},
	} {
		path := filepath.Join(home, tc.path)
		writeHarnessFixture(t, path, "#!/bin/sh\necho 1.0.0\n")
		_ = os.MkdirAll(filepath.Dir(launcher), 0755)
		_ = os.Remove(launcher)
		if err := os.Symlink(path, launcher); err != nil {
			t.Fatal(err)
		}
		state, err := controller.action(t.Context(), m.ID, "codex", "check")
		if err != nil || state.Channel != tc.channel || state.CanUpdate != (tc.channel != "unknown") || state.Path != launcher {
			t.Fatal(state, err)
		}
	}
	_ = os.Remove(launcher)
	// Only the known native install is recoverable; custom binaries do not turn
	// a missing installation into an arbitrary executable repair candidate.
	state, err := controller.action(t.Context(), m.ID, "codex", "check")
	if err != nil || state.Installed || !state.CanRepair || state.Channel != "native" {
		t.Fatal(state, err)
	}
	savePolicyRules(t, policy.Rule{ID: "deny-repair", Scope: "machine", ScopeID: m.ID, Subject: "node.install", Decision: policy.Deny})
	if _, err := controller.action(t.Context(), m.ID, "codex", "repair"); err == nil {
		t.Fatal("repair bypassed install policy")
	}
	if _, err := os.Lstat(launcher); !os.IsNotExist(err) {
		t.Fatal("denied repair created launcher", err)
	}
	savePolicyRules(t)
	state, err = controller.action(t.Context(), m.ID, "codex", "repair")
	if err != nil || !state.Installed || state.Result != "repaired" || state.Channel != "native" || state.Target != m.ID {
		t.Fatal(state, err)
	}
}
