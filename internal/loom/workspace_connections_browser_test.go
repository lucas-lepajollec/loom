package loom

// Disposable UI acceptance surface. Not part of production, and never enabled
// in ordinary tests. All keys/models/chats below are synthetic.
import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestWorkspaceConnectionsBrowser(t *testing.T) {
	if os.Getenv("LOOM_TEST_CONNECTIONS_BROWSER") != "1" {
		t.Skip("disposable browser fixture")
	}
	home := testHome(t)
	selectionFixture := os.Getenv("LOOM_TEST_SELECTION_BROWSER") == "1"
	if selectionFixture {
		model := filepath.Join(home, "models", "synthetic.gguf")
		writeMiniGGUF(t, model, "llama", 131072)
		bin := filepath.Join(home, "llama-server")
		_ = os.WriteFile(bin, []byte("#!/bin/sh\ncat <<'EOF'\n"+fakeRouterHelp+"--fit [on|off]    fit model\n--temp N    temperature\nEOF\n"), 0755)
		_ = SetConfigKey("BIN", bin)
		_ = os.MkdirAll(presetsDir(), 0755)
		_ = os.WriteFile(filepath.Join(presetsDir(), "a.env"), []byte("# NAME=Alpha preset\nMODEL="+model+"\nCTX=8192\n"), 0644)
		_ = os.WriteFile(filepath.Join(presetsDir(), "z.env"), []byte("# NAME=Zulu preset\nMODEL="+model+"\nCTX=4096\n"), 0644)
	}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"fixture-chat-small"},{"id":"fixture-chat-large"},{"id":"fixture-new-model"}]}`))
	}))
	defer provider.Close()
	_, err := workspaceSessions.saveProvider(CloudProvider{Name: "Catalogue de test", Endpoint: provider.URL + "/v1", Model: "fixture-chat-small", Models: []string{"fixture-chat-small"}}, "synthetic-only")
	if err != nil {
		t.Fatal(err)
	}
	var model codexModel
	_ = json.Unmarshal([]byte(`{"model":"fixture-codex","displayName":"Codex de test","defaultReasoningEffort":"low","supportedReasoningEfforts":[{"reasoningEffort":"low"},{"reasoningEffort":"high"}]}`), &model)
	if err = putStoreJSON(bkHarnessConnections, "codex", codexConnection{Models: []codexModel{model}}); err != nil {
		t.Fatal(err)
	}
	// Opt-in registry acceptance: native catalogs/quotas are all synthetic. The
	// browser can exercise real generic endpoints without touching an account.
	if os.Getenv("LOOM_TEST_RUNTIME_REGISTRY_BROWSER") == "1" {
		agy, codex := newRegistryFixture("antigravity"), newRegistryFixture("codex")
		agy.descriptor = (antigravityAdapter{}).Descriptor()
		codex.models = []codexModel{model}
		remaining := 42.0
		for _, a := range []*registryFixtureAdapter{agy, codex} {
			a.quota = QuotaSnapshot{Name: a.descriptor.Name, FetchedAt: time.Now().Unix(), Source: "fixture",
				Windows: []QuotaWindow{{Group: "Synthétique", Remaining: &remaining}, {Group: "Inconnu"}}}
		}
		isolateRuntimeRegistry(t, llamaRuntimeAdapter{}, cloudRuntimeAdapter{}, agy, codex,
			plannedRuntimeAdapter{RuntimeDescriptor{ID: "claude-code", Name: "Claude Code", Kind: "harness", Description: "Agent d’Anthropic.", CLI: "claude"}},
			plannedRuntimeAdapter{RuntimeDescriptor{ID: "pi", Name: "Pi", Kind: "harness", Description: "Harness de code minimaliste.", CLI: "pi"}},
			plannedRuntimeAdapter{RuntimeDescriptor{ID: "hermes", Name: "Hermes", Kind: "harness", Description: "Agent persistant.", CLI: "hermes"}})
		agy.onConnect = func() {
			_ = putStoreJSON(bkHarnessConnections, "antigravity", antigravityConnection{Models: []string{"synthetic-model"}})
		}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:2596")
	if err != nil {
		t.Fatal(err)
	}
	mux := newWebMux()
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	if selectionFixture {
		var callsMu sync.Mutex
		calls := []string{}
		server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "POST" {
				callsMu.Lock()
				calls = append(calls, r.URL.Path)
				callsMu.Unlock()
			}
			switch r.URL.Path {
			case "/fixture/calls":
				callsMu.Lock()
				defer callsMu.Unlock()
				sendJSON(w, 200, calls)
			case "/api/status":
				sendJSON(w, 200, map[string]any{"active": false, "health": false, "model": "", "ctx_effective": nil})
			case "/api/model-caps":
				sendJSON(w, 200, map[string]any{"ok": true, "native_ctx": 131072, "n_layers": 80})
			case "/api/estimate":
				sendJSON(w, 200, map[string]any{"ok": true, "vram_total_mb": 24576, "gpu_mb": 32768, "kv_mb": 20480, "ctx": 131072, "ram_offload_mb": 8192, "max_ctx_fit": 32768})
			case "/api/load-model", "/api/apply", "/api/switch", "/api/start", "/api/restart", "/api/stop", "/api/unload":
				sendJSON(w, 200, map[string]any{"ok": true}) // recorded, no process/service actions
			default:
				if strings.HasPrefix(r.URL.Path, "/api/runtimes/") && r.Method == "POST" {
					sendJSON(w, 403, map[string]any{"ok": false, "error": "native CLI disabled in fixture"})
					return
				}
				mux.ServeHTTP(w, r)
			}
		})
	}
	mux.HandleFunc("/fixture/shutdown", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		w.WriteHeader(204)
		go server.Close()
	})
	t.Logf("disposable UI http://127.0.0.1:2596/#models ; fake provider %s/v1", provider.URL)
	if err = server.Serve(listener); err != http.ErrServerClosed {
		t.Fatal(err)
	}
}
