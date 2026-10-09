package loom

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func migrationFixture(t *testing.T) (RemoteMachine, *http.ServeMux) {
	t.Helper()
	mux, _ := pairTestNode(t)
	pairTestTransport(t, mux)
	m := RemoteMachine{ID: "gpu-old", Name: "My GPU", Host: "192.168.1.20", User: "gpuuser", Port: 2222, Home: "/home/gpuuser", OS: "Linux", Folders: []string{"/workspace"}, Harnesses: []string{"custom-gpu-old-codex"}}
	if err := putStoreJSON(bkState, remoteMachinesState, []RemoteMachine{m}); err != nil {
		t.Fatal(err)
	}
	agents := []acpAgent{{ID: m.Harnesses[0], Machine: m.ID, Command: "ssh", Name: "Personal Codex", Args: []string{"old-ssh"}, Remote: true, Custom: true, Docs: "keep docs"}, {ID: "unrelated", Command: "custom-command"}}
	if err := putStoreJSON(bkState, acpCustomState, agents); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(LoomHome(), "ssh")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "loom_ed25519"), []byte("fake key"), 0600)
	_ = os.WriteFile(filepath.Join(dir, "loom_ed25519.pub"), []byte("fake public key"), 0600)
	previous := runNodeMigrationSSH
	t.Cleanup(func() { runNodeMigrationSSH = previous; registeredRuntimes.remove(m.Harnesses[0]) })
	return m, mux
}
func migrationRequest(m RemoteMachine) *http.Request {
	r := httptest.NewRequest("POST", "/api/machines/"+m.ID+"/node/migrate", strings.NewReader("{}"))
	r.SetPathValue("id", m.ID)
	return r
}
func TestSSHNodeMigrationMergesIdentityAndAgentTransport(t *testing.T) {
	m, _ := migrationFixture(t)
	if err := setHarnessManaged(m.ID, "codex", true); err != nil {
		t.Fatal(err)
	}
	if err := setHarnessManaged(m.ID, "claude-code", false); err != nil {
		t.Fatal(err)
	}
	installation, err := getBytesErr(bkState, harnessScopeKey)
	if err != nil {
		t.Fatal(err)
	}
	history := &Terminal{TerminalInfo: TerminalInfo{ID: "migration-history", Target: m.ID, Title: "Existing SSH terminal"}, scroll: []byte("retained terminal output")}
	terminals.Lock()
	terminals.byID[history.ID] = history
	terminals.Unlock()
	t.Cleanup(func() { terminals.Lock(); delete(terminals.byID, history.ID); terminals.Unlock() })
	code := pairTestCode(t, false, time.Now())
	runNodeMigrationSSH = func(ctx context.Context, got RemoteMachine, key string) (string, error) {
		if got.ID != m.ID || got.Port != 2222 || key == "" {
			t.Fatal("saved SSH connection not reused")
		}
		r := migrationRequest(m)
		r.Method = http.MethodGet
		w := httptest.NewRecorder()
		handleMachineNodeMigrate(w, r)
		if !strings.Contains(w.Body.String(), `"phase":"installing"`) {
			t.Fatal(w.Body.String())
		}
		return `installer banners` + "\nLOOM-NODE-PAIR " + `{"address":"http://192.168.1.20:2511","code":"` + code + `"}` + "\n", nil
	}
	w := httptest.NewRecorder()
	handleMachineNodeMigrate(w, migrationRequest(m))
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	var result struct {
		Machine RemoteMachine `json:"machine"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	got := result.Machine
	if got.ID != m.ID || got.Name != m.Name || got.User != "" || got.Port != 0 || got.NodeID == "" || !reflect.DeepEqual(got.Folders, m.Folders) || !reflect.DeepEqual(got.Harnesses, m.Harnesses) || got.Home != m.Home {
		t.Fatalf("merge: %+v", got)
	}
	agents := loadCustomACPAgents()
	if len(agents) != 2 || agents[0].ID != m.Harnesses[0] || agents[0].Name != "Personal Codex" || agents[0].Docs != "keep docs" || agents[0].Command == "ssh" || !reflect.DeepEqual(agents[0].Args, []string{"node-bridge", m.ID, "codex", nodeBridgeCwd}) || agents[1].Command != "custom-command" {
		t.Fatalf("agents: %+v", agents)
	}
	if savedMachineNode(got) == nil || len(loadRemoteMachines()) != 1 {
		t.Fatal("missing link or duplicate machine")
	}
	saved, _ := getBytesErr(bkState, harnessScopeKey)
	if string(saved) != string(installation) {
		t.Fatal("installation choices changed")
	}
	terminals.Lock()
	retained := terminals.byID[history.ID]
	terminals.Unlock()
	if retained != history || retained.Target != m.ID || string(retained.scroll) != "retained terminal output" {
		t.Fatal("terminal history was replaced or reassigned")
	}
	if strings.Contains(w.Body.String(), code) || strings.Contains(w.Body.String(), readAPIKey()) {
		t.Fatal("pairing secret returned to browser")
	}
}
func TestSSHNodeMigrationFailuresKeepOriginalRecord(t *testing.T) {
	for _, stage := range []string{"ssh", "output", "pair", "merge"} {
		t.Run(stage, func(t *testing.T) {
			m, _ := migrationFixture(t)
			if stage == "merge" {
				agents := loadCustomACPAgents()
				agents[0].ID = "unsupported-custom-agent"
				_ = putStoreJSON(bkState, acpCustomState, agents)
			}
			before, _ := getBytesErr(bkState, remoteMachinesState)
			beforeAgents, _ := getBytesErr(bkState, acpCustomState)
			code := pairTestCode(t, false, time.Now())
			if stage == "pair" {
				code = "0000-0000"
			}
			runNodeMigrationSSH = func(context.Context, RemoteMachine, string) (string, error) {
				if stage == "ssh" {
					return "", errors.New("ssh: connection refused exactly")
				}
				if stage == "output" {
					return "no pair details", nil
				}
				return "LOOM-NODE-PAIR " + `{"address":"http://192.168.1.20:2511","code":"` + code + `"}`, nil
			}
			w := httptest.NewRecorder()
			handleMachineNodeMigrate(w, migrationRequest(m))
			if w.Code == 200 {
				t.Fatal("failure accepted")
			}
			if stage == "ssh" && !strings.Contains(w.Body.String(), "ssh: connection refused exactly") {
				t.Fatal("lost exact error")
			}
			after, _ := getBytesErr(bkState, remoteMachinesState)
			afterAgents, _ := getBytesErr(bkState, acpCustomState)
			if string(before) != string(after) || string(beforeAgents) != string(afterAgents) || savedMachineNode(m) != nil {
				t.Fatal("half-migrated record")
			}
			machineMigrations.Lock()
			phase := machineMigrations.phases[m.ID]
			machineMigrations.Unlock()
			if phase != "" {
				t.Fatal("migration lock not released")
			}
		})
	}
}
func TestManualMigrationUsesSelectedMachineIDAndRejectsDuplicateNode(t *testing.T) {
	m, _ := migrationFixture(t)
	code := pairTestCode(t, false, time.Now())
	req := machinesPairRequest("https://192.168.1.21:2511", code, false)
	body, _ := json.Marshal(machinePairInput{Address: "https://192.168.1.21:2511", Code: code, MachineID: m.ID})
	req.Body = io.NopCloser(strings.NewReader(string(body)))
	w := httptest.NewRecorder()
	handleMachinesPair(w, req)
	got := loadRemoteMachines()
	if w.Code != 200 || len(got) != 1 || got[0].ID != m.ID || got[0].Host != "192.168.1.21" || got[0].User != "" {
		t.Fatalf("manual: %d %s", w.Code, w.Body.String())
	}
	other := RemoteMachine{ID: "other-ssh", Host: "192.168.1.22", User: "other"}
	_ = putStoreJSON(bkState, remoteMachinesState, append(got, other))
	before, _ := getBytesErr(bkState, remoteMachinesState)
	_, err := savePairedMachineMode(other, savedMachineNode(got[0]), true)
	after, _ := getBytesErr(bkState, remoteMachinesState)
	if err == nil || string(before) != string(after) {
		t.Fatal("duplicate node migration was accepted")
	}
}

func TestNodeMigrationRequiresAuthenticationVaultAndSerializesRequests(t *testing.T) {
	m, _ := migrationFixture(t)
	if err := storeWebKey("fixture-main-key"); err != nil {
		t.Fatal(err)
	}
	mux := newWebMux()
	for _, method := range []string{"GET", "POST"} {
		r := httptest.NewRequest(method, "http://192.168.1.1/api/machines/"+m.ID+"/node/migrate", strings.NewReader("{}"))
		r.RemoteAddr = "192.168.1.2:4567"
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatalf("anonymous migration %s: %d", method, w.Code)
		}
	}
	code := pairTestCode(t, false, time.Now())
	runNodeMigrationSSH = func(context.Context, RemoteMachine, string) (string, error) {
		w := httptest.NewRecorder()
		handleMachineNodeMigrate(w, migrationRequest(m))
		if w.Code != 409 || !strings.Contains(w.Body.String(), "migration is already running") {
			t.Fatal("concurrent migration accepted", w.Body.String())
		}
		return "LOOM-NODE-PAIR " + `{"address":"http://192.168.1.20:2511","code":"` + code + `"}`, nil
	}
	w := httptest.NewRecorder()
	handleMachineNodeMigrate(w, migrationRequest(m))
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	runNodeMigrationSSH = func(context.Context, RemoteMachine, string) (string, error) {
		t.Fatal("locked vault invoked SSH")
		return "", nil
	}
	clearMemDEK()
	t.Cleanup(clearMemDEK)
	if _, err := EnableMemEncryption("migration-fixture-password"); err != nil {
		t.Fatal(err)
	}
	clearMemDEK()
	w = httptest.NewRecorder()
	handleMachineNodeMigrate(w, migrationRequest(m))
	if w.Code != 423 {
		t.Fatal("locked vault accepted migration")
	}
}
