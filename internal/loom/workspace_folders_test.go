package loom

import (
	"context"
	"github.com/lucas-lepajollec/loom/internal/loom/resources"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSavedWorkspaceDefaultsAndNonDestructiveRemoval(t *testing.T) {
	home := testHome(t)
	implicit := workspaceList("local")
	if len(implicit) != 1 || !implicit[0].Default || !implicit[0].Managed {
		t.Fatal("fresh installation has no managed default")
	}
	if _, err := os.Stat(filepath.Join(home, "workspaces", "loom")); !os.IsNotExist(err) {
		t.Fatal("listing created a directory")
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "keep.txt")
	_ = os.WriteFile(file, []byte("owned by source"), 0600)
	folder, err := saveWorkspace(context.Background(), resources.Workspace{Name: "Project", Path: dir, Target: "local"}, false)
	if err != nil {
		t.Fatal(err)
	}
	def, err := defaultWorkspace(acpAgent{})
	if err != nil || def.ID != implicit[0].ID {
		t.Fatal("saving another folder replaced the managed default")
	}
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces", strings.NewReader(`{"action":"default","id":"`+folder.ID+`"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handleWorkspaces(rr, req)
	if rr.Code != 200 {
		t.Fatal(rr.Body.String())
	}
	def, _ = defaultWorkspace(acpAgent{})
	if def.ID != folder.ID {
		t.Fatal("default not changed")
	}
	req = httptest.NewRequest(http.MethodPost, "/api/workspaces", strings.NewReader(`{"action":"remove","id":"`+folder.ID+`"}`))
	req.Header.Set("Content-Type", "application/json")
	rr = httptest.NewRecorder()
	handleWorkspaces(rr, req)
	if rr.Code != 200 {
		t.Fatal(rr.Body.String())
	}
	if b, err := os.ReadFile(file); err != nil || string(b) != "owned by source" {
		t.Fatal("removing a definition touched source files")
	}
}
func TestHarnessSelectionUsesDefaultAndPreservesExistingFolder(t *testing.T) {
	testHome(t)
	a := fakeACPAdapter(t)
	a.agent.Custom = true
	isolateRuntimeRegistry(t, a, llamaRuntimeAdapter{})
	m := newRuntimeSessions()
	s, _ := m.create("", "", false)
	selected, err := m.selectModel(s.ID, a.agent.ID+":default", true)
	if err != nil || selected.Workdir == "" || selected.WorkspaceTarget != "local" {
		t.Fatalf("default selection: %v", err)
	}
	if info, err := os.Stat(selected.Workdir); err != nil || !info.IsDir() {
		t.Fatal("managed workspace not prepared")
	}
	path := t.TempDir()
	w, err := saveWorkspace(context.Background(), resources.Workspace{Name: "Other", Path: path, Target: "local", Default: true}, false)
	if err != nil {
		t.Fatal(err)
	}
	selected, err = m.selectModel(s.ID, a.agent.ID+":default", true)
	if err != nil || selected.Workdir == w.Path {
		t.Fatal("new default rewrote an existing discussion")
	}
	selected, err = m.configureDiscussion(s.ID, selected.Title, selected.ProjectID, selected.Instructions, discussionContext(selected).Revision, false, acpConfiguration{WorkspaceID: &w.ID})
	if err != nil || selected.Workdir != w.Path || selected.WorkspaceID != w.ID {
		t.Fatalf("explicit switch: %v", err)
	}
}
func TestWorkspaceCannotCrossExecutionMachines(t *testing.T) {
	testHome(t)
	a := fakeACPAdapter(t)
	a.agent.Remote = true
	a.agent.Machine = "remote"
	isolateRuntimeRegistry(t, a)
	_ = saveRemoteMachine(RemoteMachine{ID: "remote", Name: "Remote", Host: "example.test", User: "agent", Home: "/home/agent"}, nil)
	local, err := saveWorkspace(context.Background(), resources.Workspace{Name: "Local", Path: t.TempDir(), Target: "local"}, false)
	if err != nil {
		t.Fatal(err)
	}
	s := RuntimeSession{RuntimeID: a.agent.ID}
	if err := newRuntimeSessions().configureACPLocked(&s, acpConfiguration{WorkspaceID: &local.ID}, false); err == nil {
		t.Fatal("local workspace accepted for a remote execution")
	}
	if s.Workdir != "" {
		t.Fatal("rejected switch changed the folder")
	}
}
