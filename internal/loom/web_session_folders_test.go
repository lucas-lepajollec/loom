package loom

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func postAgentFolder(t *testing.T, body string) (int, map[string]any) {
	t.Helper()
	w := httptest.NewRecorder()
	handleAgentFolders(w, httptest.NewRequest(http.MethodPost, "/api/agent-folders", strings.NewReader(body)))
	out := map[string]any{}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

// An agent session is filed under a Loom folder, unfiled on request, and
// loses its folder when that folder is deleted.
func TestAgentSessionFolders(t *testing.T) {
	testHome(t)
	p, err := createProject("Work")
	if err != nil {
		t.Fatal(err)
	}
	if code, _ := postAgentFolder(t, `{"id":"s1","folder":"missing"}`); code != 404 {
		t.Fatalf("unknown folder accepted: %d", code)
	}
	if code, _ := postAgentFolder(t, `{"id":"","folder":"`+p.ID+`"}`); code != 400 {
		t.Fatalf("empty session id accepted: %d", code)
	}
	if code, _ := postAgentFolder(t, `{"id":"s1","folder":"`+p.ID+`"}`); code != 200 {
		t.Fatalf("filing failed: %d", code)
	}
	postAgentFolder(t, `{"id":"s2","folder":"`+p.ID+`"}`)
	postAgentFolder(t, `{"id":"s2","folder":""}`)
	w := httptest.NewRecorder()
	handleAgentFolders(w, httptest.NewRequest(http.MethodGet, "/api/agent-folders", nil))
	var got struct {
		Folders map[string]string `json:"folders"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got.Folders["s1"] != p.ID || got.Folders["s2"] != "" {
		t.Fatalf("folders = %v", got.Folders)
	}
	if err := deleteProject(p.ID); err != nil {
		t.Fatal(err)
	}
	if left := loadAgentFolders(); len(left) != 0 {
		t.Fatalf("deleted folder kept sessions: %v", left)
	}
}
