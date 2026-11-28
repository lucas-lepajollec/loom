package loom

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func registryRequest(t *testing.T, action map[string]any, expected int) []engineConnection {
	t.Helper()
	rec := httptest.NewRecorder()
	method := "GET"
	var body io.Reader
	if action != nil {
		method = "POST"
		raw, err := json.Marshal(action)
		if err != nil {
			t.Fatal(err)
		}
		body = strings.NewReader(string(raw))
	}
	req := httptest.NewRequest(method, "/api/engines/connections", body)
	req.Header.Set("Content-Type", "application/json")
	handleEngineConnections(rec, req)
	if rec.Code != expected {
		t.Fatalf("status %d want %d: %s", rec.Code, expected, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "native-fixture") || strings.Contains(rec.Body.String(), "sealed_key") || strings.Contains(rec.Body.String(), `"api_key"`) {
		t.Fatalf("registry response exposes a credential: %s", rec.Body)
	}
	var result struct{ Connections []engineConnection }
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result.Connections
}

func TestEngineRegistryPersistsAndSwitchesWithoutOverwriting(t *testing.T) {
	testHome(t)
	a := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer native-fixture" {
			w.WriteHeader(401)
			return
		}
		if r.URL.Path == "/v1/models" {
			io.WriteString(w, `{"data":[{"id":"one","owned_by":"vllm"},{"id":"two","owned_by":"vllm"}]}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer a.Close()
	b, _ := fakeInference(t, "llama")
	registryRequest(t, map[string]any{"action": "save", "name": "First", "url": a.URL + "/v1", "key": "native-fixture", "activate": true}, 200)
	profiles := registryRequest(t, map[string]any{"action": "save", "name": "Second", "url": b.URL}, 200)
	if len(profiles) != 2 || profiles[0].ID != connectionID(a.URL) || profiles[0].Name != "First" || !profiles[0].KeySet || !profiles[0].Active || profiles[1].Active {
		t.Fatalf("profiles = %+v", profiles)
	}
	raw := getBytes(bkState, engineConnectionsState)
	var sealed []storedEngineConnection
	if err := json.Unmarshal(raw, &sealed); err != nil || len(sealed) != 2 || sealed[0].SealedKey == "" || strings.Contains(string(raw), "native-fixture") {
		t.Fatalf("invalid persisted credentials: %s", raw)
	}
	// Simulate losing the in-process cache across a full restart.
	engineNodeMu.Lock()
	engineNodeRead, engineNodeCache = false, nil
	engineNodeMu.Unlock()
	if n := currentEngineNode(); n == nil || n.V1 != a.URL || n.APIKey != "native-fixture" {
		t.Fatal("active direct link did not survive restart")
	}

	registryRequest(t, map[string]any{"action": "activate", "id": connectionID(a.URL), "model": "invalid"}, 400)
	profiles = registryRequest(t, map[string]any{"action": "activate", "id": connectionID(b.URL), "model": "b.gguf"}, 200)
	if len(profiles) != 2 || !profiles[0].Active || currentEngineNode().Model != "b.gguf" {
		t.Fatalf("switch = %+v", profiles)
	}
	profiles = registryRequest(t, map[string]any{"action": "activate", "id": connectionID(a.URL), "model": "two"}, 200)
	if profiles[0].Model != "two" || currentEngineNode().APIKey != "native-fixture" {
		t.Fatalf("saved activation = %+v", profiles)
	}
	profiles = registryRequest(t, map[string]any{"action": "delete", "id": connectionID(b.URL)}, 200)
	if len(profiles) != 1 || currentEngineNode() == nil {
		t.Fatal("deleting an inactive profile affected the active engine")
	}
	profiles = registryRequest(t, map[string]any{"action": "delete", "id": connectionID(a.URL)}, 200)
	if len(profiles) != 0 || currentEngineNode() != nil {
		t.Fatal("deleting the active profile failed to unlink")
	}
	registryRequest(t, map[string]any{"action": "activate", "id": connectionID(a.URL)}, 404)
}

func TestEngineRegistryMigratesLegacyLinkAndKeepsConcurrentSaves(t *testing.T) {
	testHome(t)
	legacy, _ := fakeInference(t, "llama")
	node := &engineNode{Direct: true, Kind: "llama.cpp", V1: legacy.URL, Model: "a.gguf", Hostname: "legacy"}
	if err := putStoreJSON(bkState, engineNodeState, node); err != nil {
		t.Fatal(err)
	}
	profiles := registryRequest(t, nil, 200)
	if len(profiles) != 1 || !profiles[0].Active {
		t.Fatalf("legacy profile = %+v", profiles)
	}
	replacement, _ := fakeInference(t, "vllm")
	rec := httptest.NewRecorder()
	req := vllmJSONPost(strings.NewReader(`{"direct":true,"url":"` + replacement.URL + `"}`))
	handleEngineNode(rec, req)
	if rec.Code != 200 {
		t.Fatalf("old link route: %s", rec.Body)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 3)
	for range 3 {
		server, _ := fakeInference(t, "vllm")
		wg.Go(func() { _, err := linkDirectEngine(context.Background(), server.URL, "", ""); errs <- err })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	profiles = registryRequest(t, nil, 200)
	if len(profiles) != 5 || !profiles[0].Active {
		t.Fatalf("concurrent profile writes lost data: %+v", profiles)
	}
	registryRequest(t, map[string]any{"action": "unlink"}, 200)
	if currentEngineNode() != nil || len(registryRequest(t, nil, 200)) != 5 {
		t.Fatal("unlink removed saved profiles")
	}
}
