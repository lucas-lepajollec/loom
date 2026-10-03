package loom

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMachineNodeMaintenanceDoesNotSwitchActiveEngine(t *testing.T) {
	testHome(t)
	t.Cleanup(func() { _ = setEngineNode(nil) })
	active := &engineNode{URL: "https://active.example", WebKey: "active-fixture-key", Role: "engine-node"}
	if err := setEngineNode(active); err != nil {
		t.Fatal(err)
	}
	machines := []RemoteMachine{{ID: "gpu-a", Host: "gpu-a.example"}, {ID: "gpu-b", Host: "gpu-b.example"}}
	if err := putStoreJSON(bkState, remoteMachinesState, machines); err != nil {
		t.Fatal(err)
	}
	counts := []int{0, 0}
	for i, m := range machines {
		key := "machine-fixture-key-" + m.ID
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer "+key || r.Header.Get("Cookie") != "" || r.Header.Get("Origin") != "" {
				t.Error("maintenance credential isolation failed")
				w.WriteHeader(401)
				return
			}
			switch r.URL.Path {
			case "/api/node/info":
				json.NewEncoder(w).Encode(map[string]any{"ok": true, "role": "engine-node", "version": "fixture", "api_key": "private-v1-fixture"})
			case "/api/update", "/api/update/apply", "/api/ping":
				counts[i]++
				json.NewEncoder(w).Encode(map[string]any{"ok": true, "version": "fixture"})
			default:
				t.Error("unexpected maintenance route")
				w.WriteHeader(404)
			}
		}))
		defer server.Close()
		request := func(method, suffix, body string, handler http.HandlerFunc) *httptest.ResponseRecorder {
			r := httptest.NewRequest(method, "/api/machines/"+m.ID+"/node"+suffix, strings.NewReader(body))
			r.SetPathValue("id", m.ID)
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Authorization", "Bearer browser-fixture-key")
			r.Header.Set("Cookie", "loom_session=browser-fixture")
			r.Header.Set("Origin", "http://localhost:2510")
			w := httptest.NewRecorder()
			handler(w, r)
			return w
		}
		body, _ := json.Marshal(map[string]string{"url": server.URL, "key": key})
		w := request("POST", "", string(body), handleMachineNode)
		if w.Code != 200 || strings.Contains(w.Body.String(), key) || strings.Contains(w.Body.String(), "private-v1-fixture") {
			t.Fatalf("link failed/leaked: %d %s", w.Code, w.Body.String())
		}
		for _, tc := range []struct{ method, suffix string }{{"GET", "/update"}, {"POST", "/update/apply"}, {"GET", "/update/ping"}} {
			w = request(tc.method, tc.suffix, "{}", handleMachineNodeUpdate)
			if w.Code != 200 {
				t.Fatalf("maintenance failed: %d %s", w.Code, w.Body.String())
			}
		}
		if w := request("POST", "/update", "{}", handleMachineNodeUpdate); w.Code != 405 {
			t.Fatal("wrong method accepted")
		}
	}
	if counts[0] != 3 || counts[1] != 3 || currentEngineNode().URL != active.URL {
		t.Fatalf("maintenance switched/mixed nodes: %v", counts)
	}
}

func TestMachineNodeAuthRedirectAndMissingMachine(t *testing.T) {
	testHome(t)
	if err := putStoreJSON(bkState, remoteMachinesState, []RemoteMachine{{ID: "gpu"}}); err != nil {
		t.Fatal(err)
	}
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("credential-bearing redirect followed") }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer redirect.Close()
	if _, err := nodeMaintenanceProbe(t.Context(), redirect.URL, "fixture"); err == nil {
		t.Fatal("redirect accepted")
	}
	if err := storeWebKey("auth-fixture"); err != nil {
		t.Fatal(err)
	}
	mux := newWebMux()
	r := httptest.NewRequest("GET", "http://192.0.2.1/api/machines/gpu/node", nil)
	r.RemoteAddr = "192.0.2.2:4567"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("anonymous maintenance = %d", w.Code)
	}
	r = localTestRequest("GET", "/api/machines/missing/node", nil)
	r.Header.Set("Authorization", "Bearer auth-fixture")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 404 {
		t.Fatalf("unknown machine = %d", w.Code)
	}
}
