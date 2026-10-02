package loom

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEngineWorkerBoundaryAndInitialization(t *testing.T) {
	home := testHome(t)
	_ = setEngineNode(nil)
	t.Cleanup(func() { _ = setEngineNode(nil) })
	if err := initEngineWorker("", ""); err != nil {
		t.Fatal(err)
	}
	token, err := readNodeToken()
	if err != nil {
		t.Fatal(err)
	}
	if token == readAPIKey() || readAPIKey() == "" {
		t.Fatal("credentials must be separate")
	}
	if err := initEngineWorker("", ""); err != nil {
		t.Fatal(err)
	}
	again, _ := readNodeToken()
	if again != token {
		t.Fatal("init rotated the credential")
	}
	mux := newEngineWorkerMux(token)
	for _, path := range []string{"/", "/next/", "/api/workspace", "/api/chat/send", "/api/providers", "/api/harness/lifecycle", "/api/terminals", "/mcp/brain", "/api/auth/login"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		mux.ServeHTTP(rec, req)
		if rec.Code != 404 {
			t.Fatalf("unexpected exposed path %s: %d", path, rec.Code)
		}
	}
	for _, test := range []struct {
		path, key string
		status    int
	}{{"/api/config", "", 401}, {"/api/config", readAPIKey(), 401}, {"/api/config", token, 200}, {"/v1/models", token, 401}, {"/v1/models", readAPIKey(), 200}, {"/v1/models", "", 401}} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", test.path, nil)
		req.Header.Set("Authorization", "Bearer "+test.key)
		req.AddCookie(&http.Cookie{Name: "loom_session", Value: token})
		mux.ServeHTTP(rec, req)
		if rec.Code != test.status {
			t.Fatalf("%s: %d != %d", test.path, rec.Code, test.status)
		}
	}
	req := httptest.NewRequest("POST", "/api/stop", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Origin", "http://localhost:2510")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatal("browser origin accepted")
	}
	for _, dir := range []string{"memory", "workspace", "scripts"} {
		if _, err := os.Stat(filepath.Join(home, dir)); !os.IsNotExist(err) {
			t.Fatalf("created non-engine directory %s", dir)
		}
	}
	if getStr(bkState, "web_key_hash") != "" {
		t.Fatal("node changed human control key")
	}
	if err := os.Chmod(nodeTokenPath(), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := readNodeToken(); err == nil {
		t.Fatal("public-readable credential accepted")
	}
}

func TestEngineWorkerRefusesMainData(t *testing.T) {
	testHome(t)
	if err := WriteConfig(defaultConfig()); err != nil {
		t.Fatal(err)
	}
	if err := initEngineWorker("", ""); err == nil {
		t.Fatal("adopted full Loom data")
	}
	if _, err := os.Stat(nodeTokenPath()); !os.IsNotExist(err) {
		t.Fatal("created a token in main data")
	}
}

func TestEngineWorkerSameOriginLink(t *testing.T) {
	testHome(t)
	_ = writeAPIKey("inference-fixture")
	_ = setEngineNode(nil)
	srv := httptest.NewServer(newEngineWorkerMux("management-fixture"))
	defer srv.Close()
	// /v1/models stays available in idle mode, so linking starts no inference.
	n, err := linkEngineNode(context.Background(), srv.URL, "management-fixture")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = setEngineNode(nil) })
	if n.V1 != srv.URL || n.Role != "engine-node" || n.APIKey != "inference-fixture" {
		t.Fatal("incorrect node negotiation")
	}
}

func TestEngineWorkerInferenceProxy(t *testing.T) {
	testHome(t)
	_ = writeAPIKey("node-inference-fixture")
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer backend-fixture" || r.Header.Get("Cookie") != "" {
			t.Error("credential leak")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: fixture\n\ndata: [DONE]\n\n"))
	}))
	defer backend.Close()
	_ = setEngineNode(&engineNode{Direct: true, Kind: "vllm", V1: backend.URL, APIKey: "backend-fixture"})
	t.Cleanup(func() { _ = setEngineNode(nil) })
	mux := newEngineWorkerMux("management-fixture")
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"stream":true}`))
	req.Header.Set("Authorization", "Bearer node-inference-fixture")
	req.Header.Set("Cookie", "private-cookie")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "[DONE]") {
		t.Fatalf("stream lost: %d", rec.Code)
	}
}

func TestEngineWorkerUnitEscaping(t *testing.T) {
	unit, err := engineWorkerUnit("/opt/engine box/loom", "/data/test %n", "127.0.0.1:2511")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(unit, `ExecStart="/opt/engine box/loom" node serve --home "/data/test %%n"`) || !strings.Contains(unit, "KillMode=control-group") || strings.Contains(unit, "sudo") {
		t.Fatal(unit)
	}
	if _, err := engineWorkerUnit("/bin/loom\nExecStart=/evil", "/data", "localhost:2511"); err == nil {
		t.Fatal("unit injection")
	}
}

func TestEngineRoutesAvailableInWeb(t *testing.T) {
	// Prevent future registration/forwarding drift without starting a full web mux.
	registerEngineControlRoutes(func(path string, _ http.HandlerFunc) {
		if !engineRoutes[path] {
			t.Errorf("engine route missing from forwarding: %s", path)
		}
	})
}

func TestEngineWorkerInfoDoesNotExposeControlToken(t *testing.T) {
	testHome(t)
	_ = writeAPIKey("inference-fixture")
	mux := newEngineWorkerMux("management-fixture")
	req := httptest.NewRequest("GET", "/api/node/info", nil)
	req.Header.Set("Authorization", "Bearer management-fixture")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var info map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rec.Body.String(), "management-fixture") {
		t.Fatal("management credential returned")
	}
}

func TestEngineWorkerMissingKeysFailClosed(t *testing.T) {
	testHome(t)
	mux := newEngineWorkerMux("")
	for _, path := range []string{"/api/ping", "/v1/models"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("Authorization", "Bearer ")
		mux.ServeHTTP(rec, req)
		if rec.Code != 401 && rec.Code != 503 {
			t.Fatal("empty credentials opened a node route")
		}
	}
}

func TestEngineWorkerLocalBenchBoundary(t *testing.T) {
	testHome(t)
	t.Setenv("LOOM_UI_SERVICE", "loom-node")
	if _, err := benchRowsFromPicks([]benchPick{{ChoiceID: "cloud:fixture", Model: "fixture"}}); err == nil {
		t.Fatal("node accepted cloud benchmark")
	}
	_ = setEngineNode(&engineNode{Direct: true, Kind: "vllm", Model: "fixture", V1: "http://127.0.0.1:1"})
	t.Cleanup(func() { _ = setEngineNode(nil) })
	rec := httptest.NewRecorder()
	handleNodeBenchQueue(rec, httptest.NewRequest("POST", "/api/bench/queue", strings.NewReader(`{}`)))
	if rec.Code != 409 {
		t.Fatal("vLLM sweep not rejected")
	}
}

func TestEngineWorkerVLLMAliasAndNativeHealth(t *testing.T) {
	testHome(t)
	_ = writeAPIKey("inference-fixture")
	calls := 0
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer native-key" {
			t.Error("wrong native credential")
		}
		if r.URL.Path == "/health" {
			w.Write([]byte(`{"status":"ok"}`))
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["model"] != "fixture/native" || body["temperature"] != float64(0.2) || body["stream"] != true {
			t.Errorf("wrong request: %+v", body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer backend.Close()
	_ = setEngineNode(&engineNode{Direct: true, Kind: "vllm", Model: "fixture/native", V1: backend.URL, APIKey: "native-key"})
	t.Cleanup(func() { _ = setEngineNode(nil) })
	for _, path := range []string{"/health", "/v1/chat/completions"} {
		method := "GET"
		if strings.HasPrefix(path, "/v1") {
			method = "POST"
		}
		req := httptest.NewRequest(method, path, strings.NewReader(`{"model":"loom","temperature":0.2,"stream":true}`))
		req.Header.Set("Authorization", "Bearer inference-fixture")
		rec := httptest.NewRecorder()
		newEngineWorkerMux("management-fixture").ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("%s: %d", path, rec.Code)
		}
	}
	if calls != 2 {
		t.Fatal("requests did not reach native engine")
	}
	rec := httptest.NewRecorder()
	workerEngineRoute("/api/stop", func(http.ResponseWriter, *http.Request) { t.Fatal("stopped llama.cpp instead of vLLM") })(rec, httptest.NewRequest("POST", "/api/stop", nil))
	if rec.Code != 200 || currentEngineNode() != nil {
		t.Fatal("vLLM stop failed")
	}
}

func TestEngineWorkerCatalogAndPathsStayEngineOnly(t *testing.T) {
	home := testHome(t)
	_ = setEngineNode(&engineNode{Direct: true, Kind: "vllm", Model: "fixture/native", V1: "http://127.0.0.1:1"})
	t.Cleanup(func() { _ = setEngineNode(nil) })
	called := false
	workerEngineRoute("/api/models", func(w http.ResponseWriter, r *http.Request) { called = true; sendJSON(w, 200, []string{"local.gguf"}) })(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/models", nil))
	if !called {
		t.Fatal("vLLM hid the GGUF library")
	}
	rec := httptest.NewRecorder()
	workerEngineRoute("/api/paths", handlePaths)(rec, httptest.NewRequest("GET", "/api/paths", nil))
	var p loomPaths
	_ = json.Unmarshal(rec.Body.Bytes(), &p)
	if p.Home != home || p.Memory != "" || p.Workspace != "" || p.Scripts != "" {
		t.Fatal("advertised full-app paths")
	}
	for _, dir := range []string{"workspace", "memory", "scripts"} {
		if _, err := os.Stat(filepath.Join(home, dir)); !os.IsNotExist(err) {
			t.Fatal("paths provisioned full application data")
		}
	}
}

func TestEngineNodeUpdateProxyTargetsNodeOnly(t *testing.T) {
	testHome(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/update/apply" || r.Header.Get("Authorization") != "Bearer management-fixture" || r.Header.Get("Cookie") != "" || r.Header.Get("Origin") != "" {
			t.Error("wrong target or credential")
		}
		sendJSON(w, 200, map[string]any{"ok": true, "version": "fixture"})
	}))
	defer srv.Close()
	_ = setEngineNode(&engineNode{Role: "engine-node", URL: srv.URL, WebKey: "management-fixture"})
	t.Cleanup(func() { _ = setEngineNode(nil) })
	req := httptest.NewRequest("POST", "/api/engine/node/update/apply", strings.NewReader(`{"version":"fixture"}`))
	req.Header.Set("Cookie", "human-session")
	req.Header.Set("Origin", "http://main")
	rec := httptest.NewRecorder()
	handleEngineNodeUpdate(rec, req)
	if rec.Code != 200 {
		t.Fatal("node update not forwarded")
	}
}

func TestEngineWorkerPresetsKeepPrivateFrontAcrossLoadAndUnload(t *testing.T) {
	testHome(t)
	t.Setenv("LOOM_UI_SERVICE", "loom-node")
	t.Setenv("LOOM_SERVICE", "loom-node-engine-fixture")
	_ = setEngineNode(nil)
	if err := WriteConfig(map[string]string{"PORT": "2622", "HOST": "127.0.0.1"}); err != nil {
		t.Fatal(err)
	}
	if err := applyLiveConfig("MODEL=fixture.gguf\nCTX=1024\nHOST=0.0.0.0\nPORT=8081\n", ""); err != nil {
		t.Fatal(err)
	}
	if LLMPort() != 2622 || engineHost() != "127.0.0.1" {
		t.Fatal("preset relocated/exposed node front")
	}
	if err := unloadEngine(); err != nil {
		t.Fatal(err)
	}
	if LLMPort() != 2622 || engineHost() != "127.0.0.1" {
		t.Fatal("unload relocated/exposed node front")
	}
}

func TestLinkedEngineSamplingUsesNodeConfiguration(t *testing.T) {
	testHome(t)
	_ = SetConfigKey("TEMP", "0.9")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/engine/execution-config":
			if r.Header.Get("Authorization") != "Bearer management-fixture" {
				t.Error("wrong management credential")
			}
			sendJSON(w, 200, map[string]string{"TEMP": "0.23", "TOP_K": "17"})
		case "/v1/chat/completions":
			if r.Header.Get("Authorization") != "Bearer inference-fixture" {
				t.Error("wrong inference credential")
			}
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["temperature"] != 0.23 || body["top_k"] != float64(17) {
				t.Errorf("stale VM sampling: %+v", body)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hello\"},\"finish_reason\":null}]}\n\ndata: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	_ = setEngineNode(&engineNode{Role: "engine-node", URL: srv.URL, V1: srv.URL, WebKey: "management-fixture", APIKey: "inference-fixture"})
	t.Cleanup(func() { _ = setEngineNode(nil) })
	if _, err := runChat(context.Background(), []Message{{Role: "user", Content: "hello"}}, 0.7, Caps{}, func(StreamEvent) bool { return true }); err != nil {
		t.Fatal(err)
	}
	if ReadConfig()["TEMP"] != "0.9" {
		t.Fatal("changed VM configuration")
	}
}

func TestEngineNodeServerDisplaysNegotiatedEndpoint(t *testing.T) {
	testHome(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sendJSON(w, 200, map[string]any{"ok": true, "url": "http://127.0.0.1:2512/v1", "node_managed": true})
	}))
	defer srv.Close()
	n := &engineNode{Role: "engine-node", URL: srv.URL, V1: "https://engine.example", WebKey: "fixture"}
	rec := httptest.NewRecorder()
	proxyEngineNode(rec, httptest.NewRequest("GET", "/api/server", nil), n)
	var status map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &status)
	if rec.Code != 200 || status["url"] != "https://engine.example/v1" {
		t.Fatal("displayed internal or wrong-scheme endpoint")
	}
}
