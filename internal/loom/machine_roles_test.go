package loom

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// When the files brick lives on a paired machine, files requests go to its
// node with the node credential, and the roles view reports that machine.
func TestFilesBrickOnAnotherMachine(t *testing.T) {
	testHome(t)
	seen := ""
	node := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer node-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		seen = r.URL.Path + "?" + r.URL.RawQuery
		_, _ = w.Write([]byte(`{"ok":true,"root":"/srv/brain","exists":true,"tree":[]}`))
	}))
	defer node.Close()
	m := RemoteMachine{ID: "tower", Name: "Tower", Host: "127.0.0.1", NodeID: "n1", Modules: []string{"engine", "files"}}
	if err := saveRemoteMachine(m); err != nil {
		t.Fatal(err)
	}
	if err := putStoreJSON(bkState, machineNodePrefix+m.ID, engineNode{URL: node.URL, WebKey: "node-key", Role: "engine-node", Handshake: nodeHandshake}); err != nil {
		t.Fatal(err)
	}
	post := func(body string) int {
		w := httptest.NewRecorder()
		handleMachineRoles(w, httptest.NewRequest(http.MethodPost, "/api/machines/roles", strings.NewReader(body)))
		return w.Code
	}
	if post(`{"role":"engine","machine":"tower"}`) != 400 {
		t.Fatal("engine role set through the files endpoint")
	}
	if post(`{"role":"files","machine":"missing"}`) == 200 {
		t.Fatal("unknown machine accepted")
	}
	if post(`{"role":"files","machine":"tower"}`) != 200 {
		t.Fatal("files role refused")
	}
	w := httptest.NewRecorder()
	handleFilesTree(w, httptest.NewRequest(http.MethodGet, "/api/files/tree?scope=machine", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "/srv/brain") || seen != "/api/files/tree?scope=machine" {
		t.Fatalf("not proxied: %d %s (%s)", w.Code, w.Body.String(), seen)
	}
	w = httptest.NewRecorder()
	handleMachineRoles(w, httptest.NewRequest(http.MethodGet, "/api/machines/roles", nil))
	var out struct {
		Roles map[string]string `json:"roles"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if out.Roles["files"] != "tower" || out.Roles["engine"] != "local" {
		t.Fatalf("roles %v", out.Roles)
	}
	if post(`{"role":"files","machine":"local"}`) != 200 || filesMachine() != "local" {
		t.Fatal("files role not moved back")
	}
	// Another machine's files can be read without moving the brick.
	w = httptest.NewRecorder()
	handleFilesRoot(w, httptest.NewRequest(http.MethodGet, "/api/files/root?machine=tower", nil))
	if w.Code != 200 || !strings.HasPrefix(seen, "/api/files/root") {
		t.Fatalf("machine query not proxied: %d %s", w.Code, seen)
	}
}

// A node that gained modules after pairing (an update, voice enabled) is
// read again when machines are listed.
func TestNodeModulesFollowTheNode(t *testing.T) {
	testHome(t)
	node := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer node-key" || r.URL.Path != "/api/node/info" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"version":"0.2.11","handshake":1,"modules":["engine","voice","terminal","files"]}`))
	}))
	defer node.Close()
	m := RemoteMachine{ID: "tower", Name: "Tower", Host: "127.0.0.1", NodeID: "n1", Modules: []string{"engine", "terminal"}}
	if err := putStoreJSON(bkState, remoteMachinesState, []RemoteMachine{m}); err != nil {
		t.Fatal(err)
	}
	if err := putStoreJSON(bkState, machineNodePrefix+m.ID, engineNode{URL: node.URL, WebKey: "node-key", Role: "engine-node", Handshake: nodeHandshake}); err != nil {
		t.Fatal(err)
	}
	machineRefresh.Lock()
	machineRefresh.at = map[string]time.Time{}
	machineRefresh.Unlock()
	handleRemoteMachines(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/machines", nil))
	got := loadRemoteMachines()[0].Modules
	if strings.Join(got, ",") != "engine,voice,terminal,files" {
		t.Fatalf("modules %v", got)
	}
	if _, err := nodeMachineAccessForModule("tower", "files"); err != nil {
		t.Fatalf("files still unavailable: %v", err)
	}
}
