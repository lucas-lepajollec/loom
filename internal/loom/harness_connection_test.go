package loom

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHarnessConnectionRequiresOptInAndCanDisconnect(t *testing.T) {
	testHome(t)
	a := fakeACPAdapter(t)
	isolateRuntimeRegistry(t, a)
	if len(modelCatalog(nil)) != 0 {
		t.Fatal("installed CLI was exposed without a Loom connection")
	}
	if _, err := a.Connect(context.Background(), false); err == nil {
		t.Fatal("native catalog read without consent")
	}
	if _, err := a.Connect(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if !harnessConnected(a.agent) || len(modelCatalog(nil)) == 0 {
		t.Fatal("connected harness missing")
	}
	req := httptest.NewRequest(http.MethodPost, "/api/runtimes/test-acp/disconnect", strings.NewReader(`{}`))
	req.SetPathValue("id", a.agent.ID)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handleHarnessDisconnect(rr, req)
	if rr.Code != 200 || harnessConnected(a.agent) || len(modelCatalog(nil)) != 0 {
		t.Fatal("disconnect did not hide harness")
	}
	migrateHarnessConnections()
	if harnessConnected(a.agent) {
		t.Fatal("migration reconnected an explicitly disconnected harness")
	}
}
func TestExistingHarnessDiscussionsMigrateWithoutAccountRead(t *testing.T) {
	testHome(t)
	a := fakeACPAdapter(t)
	isolateRuntimeRegistry(t, a)
	_ = putStoreJSON(bkRuntimeSessions, "existing", RuntimeSession{ID: "existing", RuntimeID: a.agent.ID, ACPState: ACPState{Workdir: t.TempDir()}})
	migrateHarnessConnections()
	if !harnessConnected(a.agent) {
		t.Fatal("existing harness use lost in migration")
	}
	if _, ok := loadACPProbe(a.agent.ID); ok {
		t.Fatal("migration accessed native account")
	}
}
func TestNativeFilesystemPoliciesRejectUnsupportedConfinement(t *testing.T) {
	for _, a := range []acpAgent{{ID: "codex"}, {ID: "claude-code"}, {ID: "custom-codex", Custom: true}} {
		if _, err := harnessFilesystemMode(a, "workspace-only"); err == nil {
			t.Fatal("invented strict read confinement")
		}
	}
	if mode, err := harnessFilesystemMode(acpAgent{ID: "codex"}, "workspace-write"); err != nil || mode != "workspace-write" {
		t.Fatal("native Codex mode missing")
	}
	testHome(t)
	a := fakeACPAdapter(t)
	a.agent.ID = "codex"
	isolateRuntimeRegistry(t, a)
	s := RuntimeSession{RuntimeID: "codex"}
	policy := "full-access"
	if err := newRuntimeSessions().configureACPLocked(&s, acpConfiguration{FilesystemPolicy: &policy}, false); err == nil {
		t.Fatal("full filesystem access accepted without confirmation")
	}
}

func TestNativeFilesystemProtectionAppliesToFreshLiveAndRestoredSessions(t *testing.T) {
	testHome(t)
	t.Setenv("LOOM_TEST_FAKE_CODEX_MODES", "1")
	a := fakeACPAdapter(t)
	a.agent.ID = "codex"
	isolateRuntimeRegistry(t, a)
	m := newRuntimeSessions()
	s := createACPSession(t, m, a, "all")
	s.FilesystemPolicy = "workspace-write"
	if err := putStoreJSON(bkRuntimeSessions, s.ID, s); err != nil {
		t.Fatal(err)
	}
	run := func(request string) {
		t.Helper()
		if err := m.start(s.ID, request, "__loom_inspect_native_mode"); err != nil {
			t.Fatal(err)
		}
		state := waitACPTurn(t, m, s.ID)
		if state.Messages[len(state.Messages)-1].Content != "workspace-write" {
			t.Fatal("native protection was not applied before prompt")
		}
	}
	run("native-policy-1")
	m.acpMu.Lock()
	binding := m.acp[s.ID]
	m.acpMu.Unlock()
	if err := binding.configure(context.Background(), "agent-full-access", nil); err != nil {
		t.Fatal(err)
	}
	run("native-policy-2")
	m.closeACP(s.ID)
	run("native-policy-3")
}
