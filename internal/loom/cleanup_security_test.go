package loom

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/lucas-lepajollec/loom/internal/loom/policy"
)

func TestCleanupControlBoundaries(t *testing.T) {
	testHome(t)
	const control = "fixture-control"
	const inference = "fixture-inference"
	if err := storeWebKey(control); err != nil {
		t.Fatal(err)
	}
	if err := writeAPIKey(inference); err != nil {
		t.Fatal(err)
	}
	mux := newWebMux()
	signIn := authCall(mux, "POST", "/api/auth/password", `{"password":"`+testAccessPassword+`"}`, nil, control, "")
	cookies := signIn.Result().Cookies()
	if signIn.Code != 200 || len(cookies) != 1 {
		t.Fatal("expected owner session", signIn.Code)
	}
	for _, route := range []string{"/api/files/write", "/api/files/root", "/api/files/op", "/api/machines", "/api/machines/folders", "/api/machines/tower/node/modules", "/api/market", "/api/market/settings", "/api/codeman/config", "/api/codeman/v1/sessions"} {
		if w := authCall(mux, "POST", route, "{}", nil, inference, ""); w.Code != 401 {
			t.Errorf("inference key at %s: %d", route, w.Code)
		}
		if w := authCall(mux, "POST", route, "{}", cookies[0], "", "https://other.example"); w.Code != 403 {
			t.Errorf("foreign origin at %s: %d", route, w.Code)
		}
	}
	for _, route := range []string{"/api/files/write", "/api/files/op", "/api/machines/tower/node/modules"} {
		for _, method := range []string{"GET", "HEAD"} {
			if w := authCall(mux, method, route, "{}", cookies[0], "", ""); w.Code != 405 {
				t.Errorf("%s %s: %d", method, route, w.Code)
			}
		}
	}
	for _, route := range []string{"/api/tasks", "/api/tasks/stream", "/api/env/services", "/api/projects/context", "/api/projects/info", "/api/context/preferences", "/api/brain/sources", "/api/brain/memory", "/api/brain/distill", "/api/bench/catalog"} {
		if w := authCall(mux, "GET", route, "", cookies[0], "", ""); w.Code != 404 {
			t.Errorf("retired route %s: %d", route, w.Code)
		}
	}
	if w := authCall(mux, "GET", "/api/apikey", "", cookies[0], "", ""); w.Code != 200 || strings.Contains(w.Body.String(), inference) {
		t.Error("browser key status")
	}
	if w := authCall(mux, "GET", "/api/node/info", "", cookies[0], "", ""); w.Code != 403 || strings.Contains(w.Body.String(), inference) {
		t.Error("browser node handshake")
	}
	if w := authCall(mux, "GET", "/api/node/info", "", nil, control, ""); w.Code != 200 || !strings.Contains(w.Body.String(), inference) {
		t.Error("paired peer handshake", w.Code)
	}
	if maskAPIKey("a") == "a" || strings.Contains(maskAPIKey("ab"), "ab") {
		t.Error("short API key disclosed")
	}
}

func TestCleanupNodeFilesAndModules(t *testing.T) {
	testHome(t)
	mux := newEngineWorkerMux("machine-key")
	for _, tc := range []struct {
		method, path, body, token, origin string
		status                            int
	}{
		{"POST", "/api/node/modules", `{"module":"terminal","enabled":false}`, "", "", 401},
		{"POST", "/api/node/modules", `{"module":"terminal","enabled":false}`, "machine-key", "https://loom.example", 403},
		{"POST", "/api/node/modules", `{"module":"terminal"}`, "machine-key", "", 400},
		{"POST", "/api/node/modules", `{"module":"terminal","enabled":false}`, "machine-key", "", 200},
		{"GET", "/api/node/folders", "", "machine-key", "", 409},
		{"POST", "/api/node/workspace", `{}`, "machine-key", "", 409},
		{"GET", "/api/files/list", "", "machine-key", "", 409},
	} {
		if w := authCall(mux, tc.method, tc.path, tc.body, nil, tc.token, tc.origin); w.Code != tc.status {
			t.Errorf("%s: got %d, want %d", tc.path, w.Code, tc.status)
		}
	}
}

func TestCleanupFilesPrivateAndBounded(t *testing.T) {
	dir := secondBrainFixture(t)
	for _, input := range []string{"a/../b", "folder/.hidden/name", "a/./b"} {
		if _, err := cleanRel(input); err == nil {
			t.Errorf("accepted %q", input)
		}
	}
	for _, body := range []string{`{"path":"private/note","content":"one","create":true}`, `{"path":"private/note","content":"two"}`} {
		if status, _ := filesCall(t, handleFilesWrite, "POST", "/api/files/write", body); status != 200 {
			t.Fatal(status)
		}
	}
	if runtime.GOOS != "windows" {
		for path, want := range map[string]os.FileMode{"private": 0700, "private/note": 0600} {
			info, err := os.Stat(filepath.Join(dir, path))
			if err != nil || info.Mode().Perm() != want {
				t.Errorf("private mode for %s", path)
			}
		}
	}
}

func TestCleanupFilesForwardToOneMachine(t *testing.T) {
	testHome(t)
	m := RemoteMachine{ID: "tower", NodeID: "node", Modules: []string{"files"}}
	if err := putStoreJSON(bkState, remoteMachinesState, []RemoteMachine{m}); err != nil {
		t.Fatal(err)
	}
	if err := putStoreJSON(bkState, machineNodePrefix+m.ID, engineNode{URL: "http://127.0.0.1:2511", WebKey: "machine-key", Role: "engine-node", Handshake: nodeHandshake}); err != nil {
		t.Fatal(err)
	}
	prev := nodeClient
	t.Cleanup(func() { nodeClient = prev })
	calls := 0
	nodeClient = &http.Client{CheckRedirect: prev.CheckRedirect, Transport: codemanTestTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "127.0.0.1:2511" || r.URL.Query().Get("machine") != "local" || r.Header.Get("Authorization") != "Bearer machine-key" || r.Header.Get("Cookie") != "" {
			t.Error("wrong files destination or credentials")
		}
		resp := httptest.NewRecorder()
		resp.Header().Set("Location", "https://elsewhere.example")
		resp.WriteHeader(302)
		return resp.Result(), nil
	})}
	req := httptest.NewRequest("GET", "/api/files/read?machine=tower&path=note", nil)
	req.Header.Set("Cookie", "owner=browser")
	w := httptest.NewRecorder()
	handleFilesRead(w, req)
	if w.Code != 502 || calls != 1 {
		t.Fatalf("redirect: %d, %d upstream calls", w.Code, calls)
	}
}

func TestCleanupCodemanDestinationAndMethods(t *testing.T) {
	testHome(t)
	a := codemanConfig{URL: "http://first.test", Username: "admin"}
	b := codemanConfig{URL: "http://second.test", Username: "admin"}
	if err := updateCodemanConnection(a, "fixture", false); err != nil {
		t.Fatal(err)
	}
	if err := updateCodemanConnection(b, "", false); err != nil {
		t.Fatal(err)
	}
	if codemanAuthHeader(a) != "" || codemanAuthHeader(b) != "" {
		t.Fatal("credential survived a destination change")
	}
	if err := sealProviderSecret(codemanSecretID, "orphan"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(codemanConfigPath()); err != nil {
		t.Fatal(err)
	}
	if codemanAuthHeader(loadCodemanConfig()) != "" {
		t.Fatal("missing config exposed an orphan credential")
	}
	old := codemanClient
	t.Cleanup(func() { codemanClient = old })
	calls := 0
	codemanClient = &http.Client{Transport: codemanTestTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Path != "/api/v1/settings" {
			t.Error("unexpected upstream route")
		}
		resp := httptest.NewRecorder()
		io.WriteString(resp, `{"success":true,"data":{"voiceSettings":{"apiKey":"fixture-voice"}}}`)
		return resp.Result(), nil
	})}
	for _, method := range []string{"GET", "PUT"} {
		w := httptest.NewRecorder()
		handleCodemanProxy(w, httptest.NewRequest(method, "/api/codeman/v1/%73ettings", nil))
		if w.Code != 200 || strings.Contains(w.Body.String(), "fixture-voice") || !strings.Contains(w.Body.String(), "apiKeySet") {
			t.Errorf("%s settings response", method)
		}
	}
	for _, tc := range []struct {
		method, path string
		want         int
	}{
		{"TRACE", "sessions", 405}, {"POST", "auth/revoke", 403}, {"GET", "a%2Fb", 400},
	} {
		w := httptest.NewRecorder()
		handleCodemanProxy(w, httptest.NewRequest(tc.method, "/api/codeman/v1/"+tc.path, nil))
		if w.Code != tc.want {
			t.Errorf("%s %s returned %d", tc.method, tc.path, w.Code)
		}
	}
	if calls != 2 {
		t.Error("unexpected upstream calls", calls)
	}
}

func TestCleanupMarketDialBoundary(t *testing.T) {
	client := http.Client{Transport: marketTransport}
	if _, err := client.Get("http://127.0.0.1/"); !errors.Is(err, errPrivateTarget) {
		t.Fatalf("market dial: %v", err)
	}
}

func TestCleanupLinkedEngineControl(t *testing.T) {
	isolatePhase4b(t)
	const control = "control-fixture"
	if err := storeWebKey(control); err != nil {
		t.Fatal(err)
	}
	n := &engineNode{URL: "http://127.0.0.1:2511", Role: "engine-node", WebKey: "node-fixture"}
	if err := setEngineNode(n); err != nil {
		t.Fatal(err)
	}
	savePolicyRules(t, policy.Rule{ID: "deny", Scope: "machine", ScopeID: engineCapabilityOwner(n), Subject: "node.install", Decision: policy.Deny})
	mux := newWebMux()
	old := nodeClient
	t.Cleanup(func() { nodeClient = old })
	calls := 0
	nodeClient = &http.Client{Transport: codemanTestTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		w := httptest.NewRecorder()
		w.Header().Set("Set-Cookie", "upstream=fixture")
		if r.URL.Path == "/api/status" {
			w.Header().Set("Location", "https://elsewhere.example")
			w.WriteHeader(http.StatusTemporaryRedirect)
		} else if r.URL.Path == "/api/config" {
			w.WriteString(`{"API_KEY":"legacy-secret","MODEL":"fixture"}`)
		} else {
			w.WriteString(`{"ok":true,"key":"fixture-secret","masked":"fixture-secret"}`)
		}
		return w.Result(), nil
	})}
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{"GET", "/api/llamacpp/install", 405}, {"POST", "/api/llamacpp/install", 403}, {"GET", "/api/engine/node/update/apply", 405},
	} {
		if w := authCall(mux, tc.method, tc.path, "{}", nil, control, ""); w.Code != tc.status {
			t.Errorf("%s %s = %d", tc.method, tc.path, w.Code)
		}
	}
	if calls != 0 {
		t.Fatal("denied request reached node")
	}
	for _, path := range []string{"/api/apikey", "/api/config"} {
		w := authCall(mux, "GET", path, "", nil, control, "")
		if w.Code != 200 || strings.Contains(w.Body.String(), "secret") || w.Header().Get("Set-Cookie") != "" {
			t.Errorf("legacy peer %s status %d", path, w.Code)
		}
	}
	if w := authCall(mux, "GET", "/api/status", "", nil, control, ""); w.Code != 502 || w.Header().Get("Location") != "" {
		t.Fatal("upstream control redirect", w.Code)
	}
	w := authCall(mux, "POST", "/api/apikey", `{"action":"generate"}`, nil, control, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"key":"fixture-secret"`) {
		t.Fatal("explicit peer key issuance", w.Code)
	}
}

func TestCleanupLegacyConfig(t *testing.T) {
	testHome(t)
	if err := WriteConfig(map[string]string{"API_KEY": "legacy-secret", "CRAWL4AI_KEY": "crawler-secret", "MODEL": "old"}); err != nil {
		t.Fatal(err)
	}
	for _, handler := range []http.HandlerFunc{handleConfigEnv, handleNodeExecutionConfig} {
		w := httptest.NewRecorder()
		handler(w, httptest.NewRequest("GET", "/api/config", nil))
		if w.Code != 200 || strings.Contains(w.Body.String(), "secret") || !strings.Contains(w.Body.String(), "old") {
			t.Fatal("engine config response", w.Code)
		}
	}
	if err := applyLiveConfig("MODEL=new\n", ""); err != nil {
		t.Fatal(err)
	}
	cfg := ReadConfig()
	if cfg["API_KEY"] != "legacy-secret" || cfg["CRAWL4AI_KEY"] != "crawler-secret" || cfg["MODEL"] != "new" {
		t.Fatal("settings round trip lost fields")
	}
	id, err := SavePreset("", "fixture", "export API_KEY=legacy-secret\nMODEL=old\n")
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	handlePreset(w, httptest.NewRequest("GET", "/api/preset?id="+id, nil))
	if w.Code != 200 || strings.Contains(w.Body.String(), "legacy-secret") {
		t.Fatal("preset response", w.Code)
	}
	if _, err := SavePreset(id, "fixture", "MODEL=new\n"); err != nil {
		t.Fatal(err)
	}
	content, err := ReadPreset(id)
	if err != nil || parseEnv(content)["API_KEY"] != "legacy-secret" {
		t.Fatal("preset round trip lost field", err)
	}
	for _, path := range []string{"C:relative", "1:/fixture"} {
		if _, err := remoteWorkdir(path); err == nil {
			t.Errorf("accepted %q", path)
		}
	}
}
