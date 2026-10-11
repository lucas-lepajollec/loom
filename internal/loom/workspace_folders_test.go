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
	def, err := defaultWorkspaceForTarget("local")
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
	def, _ = defaultWorkspaceForTarget("local")
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
