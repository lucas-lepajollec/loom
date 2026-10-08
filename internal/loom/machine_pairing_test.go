package loom

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func pairTestTransport(t *testing.T, handler http.Handler) {
	t.Helper()
	previous := nodeClient.Transport
	t.Cleanup(func() { nodeClient.Transport = previous })
	nodeClient.Transport = discussionTransport(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "" || r.Header.Get("Origin") != "" || r.Header.Get("Cookie") != "" {
			t.Error("browser secrets forwarded")
		}
		copy := r.Clone(r.Context())
		copy.RemoteAddr = "127.0.0.1:4567"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, copy)
		return w.Result(), nil
	})
}

func machinesPairRequest(address, code string, force bool) *http.Request {
	body, _ := json.Marshal(map[string]any{"address": address, "code": code, "force": force})
	r := httptest.NewRequest("POST", "/api/machines/pair", strings.NewReader(string(body)))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer browser-fixture")
	r.Header.Set("Cookie", "loom_session=browser-fixture")
	r.Header.Set("Origin", "http://localhost:2510")
	return r
}

func TestMachinesPairStorageReuseAndSecretIsolation(t *testing.T) {
	mux, token := pairTestNode(t)
	pairTestTransport(t, mux)
	active := &engineNode{URL: "https://active.example", WebKey: "active-key", Role: "engine-node"}
	if err := setEngineNode(active); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = setEngineNode(nil) })
	m := RemoteMachine{ID: "gpu", Name: "My GPU", Host: "192.168.1.20", User: "gpuuser", Port: 22, Folders: []string{"/workspace"}, Harnesses: []string{"existing-agent"}}
	if err := putStoreJSON(bkState, remoteMachinesState, []RemoteMachine{m}); err != nil {
		t.Fatal(err)
	}
	code := pairTestCode(t, false, time.Now())
	w := httptest.NewRecorder()
	handleMachinesPair(w, machinesPairRequest("https://192.168.1.20:2511", code, false))
	var result struct {
		OK      bool          `json:"ok"`
		Machine RemoteMachine `json:"machine"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || !result.OK {
		t.Fatalf("pair: %d %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), token) || strings.Contains(w.Body.String(), readAPIKey()) {
		t.Fatal("browser received secrets")
	}
	got := result.Machine
	if got.ID != m.ID || got.User != m.User || got.Port != m.Port || got.Folders[0] != m.Folders[0] || got.Harnesses[0] != m.Harnesses[0] || got.NodeID == "" || got.Handshake != 1 || !hasEngineModule(got.Modules) {
		t.Fatal("existing SSH registration lost or handshake missing")
	}
	n := savedMachineNode(got)
	if n == nil || n.WebKey != token || n.APIKey != readAPIKey() || n.URL != "https://192.168.1.20:2511" || n.V1 != n.URL {
		t.Fatal("existing secret storage not reused/TLS lost")
	}
	if currentEngineNode().URL != active.URL {
		t.Fatal("pairing switched active engine")
	}
	if _, err := os.Stat(filepath.Join(LoomHome(), "ssh")); !os.IsNotExist(err) {
		t.Fatal("pairing created SSH credentials")
	}
	code = pairTestCode(t, false, time.Now())
	w = httptest.NewRecorder()
	handleMachinesPair(w, machinesPairRequest(n.URL, code, false))
	if w.Code != 200 || len(loadRemoteMachines()) != 1 {
		t.Fatal("re-pair duplicated machine")
	}
	r := httptest.NewRequest("GET", "/api/machines/gpu/node", nil)
	r.SetPathValue("id", "gpu")
	w = httptest.NewRecorder()
	handleMachineNode(w, r)
	if w.Code != 200 || strings.Contains(w.Body.String(), token) || strings.Contains(w.Body.String(), readAPIKey()) {
		t.Fatal("maintenance observation leaked secrets")
	}
}

func TestMachinesPairCreatesMachineWithoutSSH(t *testing.T) {
	mux, _ := pairTestNode(t)
	_ = setEngineNode(nil)
	pairTestTransport(t, mux)
	code := pairTestCode(t, false, time.Now())
	w := httptest.NewRecorder()
	handleMachinesPair(w, machinesPairRequest("192.168.1.20:2511", code, false))
	machines := loadRemoteMachines()
	if w.Code != 200 || len(machines) != 1 || machines[0].Name == "" || machines[0].Host != "192.168.1.20" || machines[0].User != "" || machines[0].Port != 0 {
		t.Fatalf("machine creation: %d %s", w.Code, w.Body.String())
	}
	if savedMachineNode(machines[0]) == nil {
		t.Fatal("maintenance credential missing")
	}
}

func TestMachinesPairFreshCodeAllowsForcedRepair(t *testing.T) {
	mux, _ := pairTestNode(t)
	pairTestTransport(t, mux)
	if w := pairTestExchange(mux, pairTestCode(t, false, time.Now()), "another-main", "192.168.1.2:1", false); w.Code != 200 {
		t.Fatal("initial pair failed")
	}
	code := pairTestCode(t, false, time.Now())
	w := httptest.NewRecorder()
	handleMachinesPair(w, machinesPairRequest("192.168.1.20:2511", code, false))
	if w.Code != 409 || !strings.Contains(w.Body.String(), "Main another-main") {
		t.Fatal("main conflict missing")
	}
	w = httptest.NewRecorder()
	handleMachinesPair(w, machinesPairRequest("192.168.1.20:2511", code, true))
	if w.Code != 200 {
		t.Fatal("main force not forwarded", w.Body.String())
	}
}

func TestMachinePairStorageKeepsExplicitMachineIdentity(t *testing.T) {
	testHome(t)
	if err := putStoreJSON(bkState, remoteMachinesState, []RemoteMachine{{ID: "first", Host: "gpu"}, {ID: "second", Host: "gpu"}}); err != nil {
		t.Fatal(err)
	}
	m, err := savePairedMachine(RemoteMachine{ID: "second", Host: "gpu"}, &engineNode{URL: "https://gpu:2511", WebKey: "fixture", NodeID: "node", Modules: []string{"engine"}, Handshake: 1})
	if err != nil || m.ID != "second" || savedMachineNode(RemoteMachine{ID: "first"}) != nil || savedMachineNode(m) == nil {
		t.Fatal("maintenance link moved to wrong machine", err)
	}
}

func TestMachinesPairErrorsAndHandshake(t *testing.T) {
	testHome(t)
	for _, tc := range []struct {
		name            string
		upstream, want  int
		body, errorCode string
	}{
		{"invalid", 401, 401, `{"error":"SECRET"}`, "invalid_code"},
		{"expired", 400, 401, `{"error":"SECRET"}`, "invalid_code"},
		{"owner", 409, 409, `{"main":{"name":"Other Loom"},"error":"SECRET"}`, "already_paired"},
		{"limit", 429, 429, `{}`, "rate_limited"},
		{"old", 404, 409, `{}`, "pairing_unsupported"},
		{"mismatch", 200, 409, `{"machine_token":"SECRET","inference_key":"SECRET","node":{"handshake":2}}`, "handshake_mismatch"},
		{"missing handshake", 200, 409, `{"machine_token":"SECRET","node":{}}`, "handshake_mismatch"},
		{"bad response", 200, 502, `{"machine_token":"SECRET","node":{"handshake":1}}`, ""},
		{"bad json", 200, 502, `SECRET`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pairTestTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req nodePairRequest
				if r.URL.Scheme != "https" || r.URL.Path != "/api/node/pair" || json.NewDecoder(r.Body).Decode(&req) != nil || !req.Force || req.Main.ID == "" || req.Main.Name == "" || req.Main.Version != Version {
					t.Error("bad exchange request")
				}
				w.WriteHeader(tc.upstream)
				io.WriteString(w, tc.body)
			}))
			w := httptest.NewRecorder()
			handleMachinesPair(w, machinesPairRequest("https://gpu.example:2511", "K7QM-4XPA", true))
			if w.Code != tc.want || strings.Contains(w.Body.String(), "SECRET") || (tc.errorCode != "" && !strings.Contains(w.Body.String(), tc.errorCode)) {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			if tc.name == "owner" && !strings.Contains(w.Body.String(), "Other Loom") {
				t.Fatal("owner name missing")
			}
			if len(loadRemoteMachines()) != 0 {
				t.Fatal("failed pairing saved machine")
			}
		})
	}
	previous := nodeClient.Transport
	t.Cleanup(func() { nodeClient.Transport = previous })
	nodeClient.Transport = discussionTransport(func(*http.Request) (*http.Response, error) { return nil, errors.New("offline") })
	w := httptest.NewRecorder()
	handleMachinesPair(w, machinesPairRequest("192.168.1.20:2511", "K7QM-4XPA", false))
	if w.Code != 502 || !strings.Contains(w.Body.String(), "unreachable") {
		t.Fatal("missing unreachable error")
	}
}

func TestNodeInfoHandshakeChecksBothLinkFlows(t *testing.T) {
	testHome(t)
	previous := nodeClient.Transport
	t.Cleanup(func() { nodeClient.Transport = previous; _ = setEngineNode(nil) })
	nodeClient.Transport = discussionTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"ok":true,"role":"engine-node","engine":true,"v1_exposed":true,"v1_same_origin":true,"handshake":77,"modules":["engine"]}`))}, nil
	})
	if _, err := nodeMaintenanceProbe(t.Context(), "https://gpu.example", "fixture"); err == nil || !strings.Contains(err.Error(), "major 77") {
		t.Fatal("maintenance accepted unknown handshake", err)
	}
	if _, err := linkEngineNode(t.Context(), "https://gpu.example", "fixture"); err == nil || !strings.Contains(err.Error(), "major 77") {
		t.Fatal("engine accepted unknown handshake", err)
	}
	if checkNodeHandshake(0) != nil || checkNodeHandshake(1) != nil {
		t.Fatal("legacy/current handshake rejected")
	}
}

func TestMachinesPairEncryptedStorageAndVaultGate(t *testing.T) {
	mux, token := pairTestNode(t)
	pairTestTransport(t, mux)
	clearMemDEK()
	t.Cleanup(clearMemDEK)
	if _, err := EnableMemEncryption("pair-test-password"); err != nil {
		t.Fatal(err)
	}
	code := pairTestCode(t, false, time.Now())
	w := httptest.NewRecorder()
	handleMachinesPair(w, machinesPairRequest("192.168.1.20:2511", code, false))
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	machines := loadRemoteMachines()
	raw := getBytes(bkState, machineNodePrefix+machines[0].ID)
	if !looksEncrypted(raw) || strings.Contains(string(raw), token) || savedMachineNode(machines[0]).WebKey != token {
		t.Fatal("secret encryption/readback failed")
	}
	clearMemDEK()
	nodeClient.Transport = discussionTransport(func(*http.Request) (*http.Response, error) { t.Fatal("locked vault contacted node"); return nil, nil })
	w = httptest.NewRecorder()
	handleMachinesPair(w, machinesPairRequest("192.168.1.20:2511", "K7QM-4XPA", false))
	if w.Code != 423 {
		t.Fatal("locked pairing accepted")
	}
	w = httptest.NewRecorder()
	handleMachinesDiscover(w, httptest.NewRequest("GET", "/api/machines/discover", nil))
	if w.Code != 423 {
		t.Fatal("locked discovery accepted")
	}
}

func TestMachinesPairAndDiscoveryRequireMainAuthentication(t *testing.T) {
	testHome(t)
	if err := storeWebKey("fixture-key"); err != nil {
		t.Fatal(err)
	}
	mux := newWebMux()
	for _, tc := range []struct{ method, path string }{{"POST", "/api/machines/pair"}, {"GET", "/api/machines/discover"}} {
		r := httptest.NewRequest(tc.method, "http://192.168.1.1"+tc.path, strings.NewReader("{}"))
		r.RemoteAddr = "192.168.1.2:4567"
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatalf("anonymous %s: %d", tc.path, w.Code)
		}
	}
}
