package loom

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestRetiredIntegrationRoutesRemainAuthenticatedAndInert(t *testing.T) {
	testHome(t)
	if err := storeWebKey("synthetic-control"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	registerRetiredIntegrations(mux)
	for _, path := range []string{"/api/agents/installations", "/api/harness/lifecycle", "/api/runtimes/codex/account", "/api/harness-history/transfer", "/api/brain/agents", "/api/usage/native", "/api/skills/targets"} {
		if w := authCall(mux, "POST", path, "{}", nil, "", ""); w.Code != 401 {
			t.Fatalf("%s unauthenticated: %d", path, w.Code)
		}
		for _, method := range []string{"GET", "POST"} {
			w := authCall(mux, method, path, "{}", nil, "synthetic-control", "")
			if w.Code != 410 || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("%s %s: %d", method, path, w.Code)
			}
		}
	}
}

func TestRetirementCatalogAndLegacyDiscussion(t *testing.T) {
	continuityBase(t)
	catalog := runtimeCatalog()
	if len(catalog) != 2 {
		t.Fatal(catalog)
	}
	for _, d := range catalog {
		if d.Kind != "local" && d.Kind != "cloud" {
			t.Fatal(d.ID, d.Kind)
		}
	}
	raw := `{"id":"retired-thread","runtime_id":"pi","model":"old-model","status":"idle","messages":[{"role":"user","content":"retained question"},{"role":"assistant","content":"retained answer"}],"acp":{"workdir":"/workspace/old"}}`
	var legacy RuntimeSession
	if err := json.Unmarshal([]byte(raw), &legacy); err != nil {
		t.Fatal(err)
	}
	if err := putStoreJSON(bkRuntimeSessions, legacy.ID, legacy); err != nil {
		t.Fatal(err)
	}
	m := newRuntimeSessions()
	err := m.startPrepared(legacy.ID, "retired-request", "new message", func(s RuntimeSession, text string) DiscussionPreview { return DiscussionPreview{} })
	if err == nil || !strings.Contains(err.Error(), "retired") {
		t.Fatal(err)
	}
	saved, ok := m.get(legacy.ID)
	if !ok || len(saved.Messages) != 2 || saved.Messages[1].Content != "retained answer" || saved.RuntimeID != "pi" {
		t.Fatal("legacy transcript changed")
	}
}

func TestRetiredDiscussionSelectsLocalCloudWithoutLosingHistory(t *testing.T) {
	continuityBase(t)
	if err := SetConfigKey("MODEL", "fixture.gguf"); err != nil {
		t.Fatal(err)
	}
	m := newRuntimeSessions()
	legacy := RuntimeSession{
		ID: "retired-history", RuntimeID: "pi", Model: "old-model", Status: "idle",
		Messages: []Message{{Role: "user", Content: "retained question"}, {Role: "assistant", Content: "retained answer"}},
		Turns:    []RuntimeTurnRecord{{RuntimeID: "pi", ACPEvents: []DiscussionEvent{{"type": "plan", "text": "historical plan"}}}},
		ACPState: ACPState{NativeSessionID: "old-session", NativeRuntimeID: "pi", NativeContext: "private runtime state", FilesystemPolicy: "full", Mode: "native-mode"},
	}
	if err := putStoreJSON(bkRuntimeSessions, legacy.ID, legacy); err != nil {
		t.Fatal(err)
	}
	localID := ""
	for _, choice := range modelCatalog(nil) {
		if choice.Kind == "local" {
			localID = choice.ID
			break
		}
	}
	local, err := m.selectModel(legacy.ID, localID, false)
	if err != nil {
		t.Fatal(err)
	}
	if local.RuntimeID != "llama.cpp" || local.NativeSessionID != "" || local.NativeRuntimeID != "" || local.NativeContext != "" || local.FilesystemPolicy != "" || local.Mode != "" {
		t.Fatal("retired runtime state transferred to Local")
	}
	provider, err := m.saveProvider(CloudProvider{Name: "Fixture", Endpoint: "https://provider.example/v1", Model: "fixture-cloud"}, "synthetic-key")
	if err != nil {
		t.Fatal(err)
	}
	cloudID := cloudChoiceID(provider.ID, provider.Model)
	if _, err := m.selectModel(legacy.ID, cloudID, false); err == nil {
		t.Fatal("external sharing accepted without consent")
	}
	cloud, err := m.selectModel(legacy.ID, cloudID, true)
	if err != nil {
		t.Fatal(err)
	}
	if cloud.RuntimeID != "openai-compatible" || cloud.ID != legacy.ID || !reflect.DeepEqual(cloud.Messages, legacy.Messages) || !reflect.DeepEqual(cloud.Turns, legacy.Turns) || len(m.list()) != 1 {
		t.Fatal("Local/Cloud selection lost or forked legacy history")
	}
}

func TestRuntimeRegistrationDoesNotReadEngineState(t *testing.T) {
	testHome(t)
	registry := newRuntimeRegistry()
	if err := registry.register(llamaRuntimeAdapter{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dbPath()); !os.IsNotExist(err) {
		t.Fatal("runtime registration accessed the data store", err)
	}
}

func TestEngineNodeCacheFollowsDataHome(t *testing.T) {
	first := testHome(t)
	second := t.TempDir()
	node := &engineNode{URL: "https://node.example", V1: "https://node.example", WebKey: "synthetic-machine-key"}
	if err := setEngineNode(node); err != nil {
		t.Fatal(err)
	}
	engineModelsCache.Lock()
	engineModelsCache.body = []byte(`{"data":[{"id":"old-model"}]}`)
	engineModelsCache.Unlock()
	t.Setenv("LOOM_HOME", second)
	if currentEngineNode() != nil {
		t.Fatal("engine from another data home was reused")
	}
	engineModelsCache.Lock()
	stale := engineModelsCache.body != nil
	engineModelsCache.Unlock()
	if stale {
		t.Fatal("engine model catalog from another data home was reused")
	}
	t.Setenv("LOOM_HOME", first)
	if restored := currentEngineNode(); restored == nil || restored.URL != node.URL {
		t.Fatal("original engine link was lost")
	}
}

func TestGatewayRotationRetainsMetadataAndOnlyStoresHash(t *testing.T) {
	testHome(t)
	state := gatewayState{Entries: map[string]gatewayOwnership{"legacy": {File: "/unused/config", Hash: "ownership"}}}
	if err := putJSON(bkState, gatewayStateKey, state); err != nil {
		t.Fatal(err)
	}
	first, err := rotateGatewayToken()
	if err != nil {
		t.Fatal(err)
	}
	second, err := rotateGatewayToken()
	if err != nil {
		t.Fatal(err)
	}
	stored, err := loadGatewayState()
	if err != nil {
		t.Fatal(err)
	}
	if first == second || stored.TokenHash != hashWebKey(second) || stored.Entries["legacy"] != state.Entries["legacy"] {
		t.Fatal("rotation lost ownership or credential identity")
	}
	raw, err := getBytesErr(bkState, gatewayStateKey)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), first) || strings.Contains(string(raw), second) {
		t.Fatal("plaintext persisted")
	}
	recorder := httptest.NewRecorder()
	handleMCPGateway(recorder, httptest.NewRequest("GET", "/api/mcp/gateway", nil))
	if recorder.Code != 200 || strings.Contains(recorder.Body.String(), second) {
		t.Fatal("observation exposed token")
	}
}

func TestHistoricalMachineCapabilitiesCannotAdvertiseHarnesses(t *testing.T) {
	m := projectMachineCapabilities(RemoteMachine{ID: "retired", Modules: []string{"engine", "harness", "terminal", "unknown"}})
	if strings.Join(m.Modules, ",") != "engine,terminal" {
		t.Fatal(m.Modules)
	}
}

func TestRemovedGatewaySessionTokensCannotAuthorizeExternalClients(t *testing.T) {
	testHome(t)
	if err := storeWebKey("synthetic-control"); err != nil {
		t.Fatal(err)
	}
	token, err := rotateGatewayToken()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp/loom", requireGatewayAuth(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	for _, credential := range []string{"", "session.forged.retired", "inference-only"} {
		if w := authCall(mux, "POST", "/mcp/loom", "{}", nil, credential, ""); w.Code != 401 {
			t.Fatal("unsupported credential authorized MCP", w.Code)
		}
	}
	if w := authCall(mux, "POST", "/mcp/loom", "{}", nil, token, ""); w.Code != 204 {
		t.Fatal("dedicated token rejected", w.Code)
	}
}

func TestNodeInventoryAndFoldersSurviveRetirementWithMachineAuth(t *testing.T) {
	testHome(t)
	if err := initEngineWorker("", ""); err != nil {
		t.Fatal(err)
	}
	token, err := readNodeToken()
	if err != nil {
		t.Fatal(err)
	}
	mux := newEngineWorkerMux(token)
	for _, path := range []string{"/api/node/inventory", "/api/node/folders"} {
		for _, credential := range []string{"", readAPIKey()} {
			if w := authCall(mux, "GET", path, "", nil, credential, ""); w.Code != 401 {
				t.Fatal(path, "accepted non-machine credential", w.Code)
			}
		}
		if w := authCall(mux, "GET", path, "", nil, token, ""); w.Code != 200 {
			t.Fatal(path, w.Code)
		}
		if w := authCall(mux, "GET", path, "", nil, token, "https://other.example"); w.Code != 403 {
			t.Fatal(path, "accepted browser origin", w.Code)
		}
	}
}
