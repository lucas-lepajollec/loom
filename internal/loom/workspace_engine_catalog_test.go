package loom

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestRemoteDiscussionChoicesUseEngineLibrary(t *testing.T) {
	testHome(t)
	isolateRuntimeRegistry(t, llamaRuntimeAdapter{})
	SetConfigKey("MODEL", "control-plane-only.gguf")
	var reads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-control" {
			t.Error("missing node authentication")
		}
		switch r.URL.Path {
		case "/api/models":
			reads.Add(1)
			w.Write([]byte(`[{"name":"gemma.gguf","path":"/engine/models/gemma.gguf","value":"gemma.gguf"},{"name":"gemma.gguf","path":"/other/gemma.gguf","value":"/other/gemma.gguf"},{"name":"mmproj.gguf","path":"/engine/models/mmproj.gguf","mmproj":true}]`))
		case "/api/status":
			w.Write([]byte(`{"model":"gemma.gguf"}`))
		default:
			t.Error("unexpected endpoint", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	if err := setEngineNode(&engineNode{URL: server.URL, WebKey: "fixture-control"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = setEngineNode(nil) })
	choices := modelCatalog(nil)
	if len(choices) != 2 || choices[0].Model != "/engine/models/gemma.gguf" || choices[0].EngineValue != "gemma.gguf" || !choices[0].Ready || choices[1].Ready {
		t.Fatalf("remote catalog: %+v", choices)
	}
	encoded, _ := json.Marshal(choices)
	if strings.Contains(string(encoded), "fixture-control") || strings.Contains(string(encoded), "control-plane-only") {
		t.Fatal("control plane state leaked into engine catalog")
	}
	modelCatalog(nil)
	if reads.Load() != 1 {
		t.Fatal("repeated catalog polls fetched the library again")
	}
	putStr(bkModelChoices, choices[0].ID, "hidden")
	if modelCatalog(nil)[0].Enabled {
		t.Fatal("cache ignored selector visibility")
	}
	_ = setEngineNode(&engineNode{URL: server.URL, WebKey: "fixture-control"})
	modelCatalog(nil)
	if reads.Load() != 2 {
		t.Fatal("new engine link reused old library")
	}
	_ = setEngineNode(nil)
	local := modelCatalog(nil)
	if len(local) != 1 || local[0].Model != "control-plane-only.gguf" {
		t.Fatal("unlink retained remote paths")
	}
}

func TestDirectEngineDiscussionCatalogKeepsFullModelIDs(t *testing.T) {
	testHome(t)
	isolateRuntimeRegistry(t, llamaRuntimeAdapter{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer fixture-inference" {
			t.Error("wrong inference request")
		}
		w.Write([]byte(`{"data":[{"id":"vendor-a/gemma"},{"id":"vendor-b/gemma"}]}`))
	}))
	defer server.Close()
	_ = setEngineNode(&engineNode{URL: server.URL, V1: server.URL, APIKey: "fixture-inference", Direct: true, Kind: "vllm", Model: "vendor-b/gemma"})
	t.Cleanup(func() { _ = setEngineNode(nil) })
	choices := modelCatalog(nil)
	if len(choices) != 2 || choices[0].ID == choices[1].ID || choices[0].Ready || !choices[1].Ready || choices[1].Model != "vendor-b/gemma" {
		t.Fatalf("direct models: %+v", choices)
	}
	m := newRuntimeSessions()
	s, _ := m.create("", "", false)
	selected, err := m.selectModel(s.ID, choices[1].ID, false)
	if err != nil || selected.RuntimeID != "llama.cpp" || selected.Model != "vendor-b/gemma" {
		t.Fatalf("selection: %+v %v", selected, err)
	}
}
