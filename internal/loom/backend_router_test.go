package loom

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

const fakeRouterHelp = `----- common params -----

-c,    --ctx-size N                     size of the prompt context
-fa,   --flash-attn [on|off|auto]       set Flash Attention use
-ngl,  --gpu-layers, --n-gpu-layers N   max. number of layers to store in VRAM
-ctk,  --cache-type-k TYPE              KV cache data type for K
--mmap, --no-mmap                       whether to memory-map model
--host, --no-host                       bypass host buffer allowing extra buffers to be used
--reasoning-budget N                    token budget for thinking

----- example-specific params -----

-np,   --parallel N                     number of server slots
-m,    --model FNAME                    model path to load
--host HOST                             ip address to listen
--port PORT                             port to listen
--api-key KEY                           API key to use for authentication
--slots, --no-slots                     expose slots monitoring endpoint
--models-preset PATH                    path to INI file containing model presets for the router server
--models-max N                          for router server, maximum number of models to load simultaneously
`

// fakeLlamaBin écrit un exécutable qui imprime une aide llama-server.
func fakeLlamaBin(t *testing.T, dir string) string {
	t.Helper()
	bin := filepath.Join(dir, "llama-server")
	script := "#!/bin/sh\ncat <<'EOF'\n" + fakeRouterHelp + "EOF\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

// fakeRouter simule l'API de gestion du router llama.cpp.
type fakeRouter struct {
	mu         sync.Mutex
	status     map[string]string
	loads      []string
	iniDir     string
	props      string
	propsReads int
	onProps    func()
}

func (f *fakeRouter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case r.URL.Path == "/props" && r.URL.Query().Get("model") == "":
		_, _ = io.WriteString(w, `{"role":"router"}`)
	case r.URL.Path == "/props":
		f.propsReads++
		if r.URL.Query().Get("autoload") != "false" {
			http.Error(w, "autoload must be disabled", 400)
			return
		}
		if f.onProps != nil {
			f.onProps()
		}
		_, _ = io.WriteString(w, f.props)
	case r.URL.Path == "/models":
		if r.URL.Query().Get("reload") != "" {
			raw, _ := os.ReadFile(filepath.Join(f.iniDir, "router-models.ini"))
			for _, line := range strings.Split(string(raw), "\n") {
				if strings.HasPrefix(line, "[") {
					name := strings.Trim(line, "[]")
					if _, ok := f.status[name]; !ok {
						f.status[name] = "unloaded"
					}
				}
			}
		}
		type st struct {
			Value string `json:"value"`
		}
		type item struct {
			ID     string `json:"id"`
			Status st     `json:"status"`
		}
		var data []item
		for id, s := range f.status {
			data = append(data, item{ID: id, Status: st{s}})
			if s == "loading" {
				f.status[id] = "loaded"
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	case r.URL.Path == "/models/load":
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.loads = append(f.loads, body.Model)
		for id, s := range f.status {
			if s == "loaded" && id != body.Model {
				f.status[id] = "unloaded" // models-max 1 : LRU
			}
		}
		f.status[body.Model] = "loading"
		_, _ = io.WriteString(w, `{"success":true}`)
	case r.URL.Path == "/models/unload":
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.status[body.Model] = "unloaded"
		_, _ = io.WriteString(w, `{"success":true}`)
	case r.URL.Path == "/v1/chat/completions":
		raw, _ := io.ReadAll(r.Body)
		_, _ = w.Write(raw) // renvoie le corps reçu pour vérifier la réécriture
	default:
		http.NotFound(w, r)
	}
}

// startFakeRouter place le faux router sur le port interne que Loom dérive
// de PORT (PORT + 10000).
func startFakeRouter(t *testing.T, home string) *fakeRouter {
	t.Helper()
	var ln net.Listener
	var err error
	for i := 0; i < 50; i++ {
		ln, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		if ln.Addr().(*net.TCPAddr).Port > 11000 {
			break
		}
		ln.Close()
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if err := SetConfigKey("PORT", strconv.Itoa(port-10000)); err != nil {
		t.Fatal(err)
	}
	fr := &fakeRouter{status: map[string]string{}, iniDir: home}
	srv := httptest.NewUnstartedServer(fr)
	srv.Listener.Close()
	srv.Listener = ln
	srv.Start()
	t.Cleanup(srv.Close)
	return fr
}

func setupRouterHome(t *testing.T) (string, *fakeRouter) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("LOOM_HOME", home)
	bin := fakeLlamaBin(t, home)
	model := filepath.Join(home, "qwen.gguf")
	if err := os.WriteFile(model, []byte("GGUF"), 0o644); err != nil {
		t.Fatal(err)
	}
	for k, v := range map[string]string{"BIN": bin, "MODEL": model, "NP": "4"} {
		if err := SetConfigKey(k, v); err != nil {
			t.Fatal(err)
		}
	}
	return home, startFakeRouter(t, home)
}

func TestRouterActivateLoadsWithoutRestartingEngine(t *testing.T) {
	home, fr := setupRouterHome(t)
	if !routerReachable() {
		t.Fatal("le faux router doit être détecté")
	}
	if err := restartLlamaEngine(); err != nil {
		t.Fatal(err)
	}
	active := getStr(bkState, routerStateActive)
	if active == "" || routerCurrentName() != active {
		t.Fatalf("active=%q current=%q", active, routerCurrentName())
	}
	ini, _ := os.ReadFile(filepath.Join(home, "router-models.ini"))
	for _, want := range []string{"[" + active + "]", "np = 4", "m = " + filepath.Join(home, "qwen.gguf")} {
		if !strings.Contains(string(ini), want) {
			t.Fatalf("INI sans %q :\n%s", want, ini)
		}
	}
	if strings.Contains(string(ini), "port") || strings.Contains(string(ini), "host") {
		t.Fatalf("host/port appartiennent au router :\n%s", ini)
	}
	if len(fr.loads) != 1 || fr.loads[0] != active {
		t.Fatalf("chargements = %v", fr.loads)
	}
	// Même configuration : rien à recharger.
	if err := restartLlamaEngine(); err != nil {
		t.Fatal(err)
	}
	if len(fr.loads) != 1 {
		t.Fatalf("une configuration déjà chargée ne doit pas se recharger : %v", fr.loads)
	}
}

func TestRouterFrontRewritesModelAndHealth(t *testing.T) {
	_, fr := setupRouterHome(t)
	h := newOAIRouter("")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("sans modèle, /health = %d", rec.Code)
	}
	if err := restartLlamaEngine(); err != nil {
		t.Fatal(err)
	}
	routerModeSeen = routerModeSeen.AddDate(-1, 0, 0) // invalide le cache
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("modèle chargé, /health = %d %s", rec.Code, rec.Body)
	}
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"local","messages":[]}`))
	h.ServeHTTP(rec, req)
	var echoed struct {
		Model string `json:"model"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &echoed)
	if echoed.Model != routerCurrentName() {
		t.Fatalf("le router doit recevoir la section Loom, reçu %q (%s)", echoed.Model, rec.Body)
	}
	if len(fr.loads) != 1 {
		t.Fatalf("une requête sur le modèle chargé ne doit rien recharger : %v", fr.loads)
	}
}

func TestRouterAPIOverridesBecomeVariantWithoutTouchingConfig(t *testing.T) {
	_, fr := setupRouterHome(t)
	if err := restartLlamaEngine(); err != nil {
		t.Fatal(err)
	}
	active := getStr(bkState, routerStateActive)
	t.Cleanup(clearOAIRuntime)
	if err := oaiRuntimeApply(map[string]string{"NP": "8"}, nil); err != nil {
		t.Fatal(err)
	}
	cur := routerCurrentName()
	if cur == active || getStr(bkState, routerStateActive) != active {
		t.Fatalf("la variante doit servir sans remplacer le choix de l'utilisateur (active=%s current=%s)", active, cur)
	}
	if ReadConfig()["NP"] != "4" {
		t.Fatal("une requête API ne doit jamais modifier la configuration enregistrée")
	}
	if len(fr.loads) != 2 || fr.loads[1] != cur {
		t.Fatalf("chargements = %v", fr.loads)
	}
}

func TestRouterUnloadKeepsEngineAndClearsSelection(t *testing.T) {
	_, fr := setupRouterHome(t)
	if err := restartLlamaEngine(); err != nil {
		t.Fatal(err)
	}
	active := getStr(bkState, routerStateActive)
	if err := unloadEngine(); err != nil {
		t.Fatal(err)
	}
	if routerCurrentName() != "" || fr.status[active] != "unloaded" {
		t.Fatalf("current=%q status=%v", routerCurrentName(), fr.status)
	}
	if strings.TrimSpace(ReadConfig()["MODEL"]) != "" {
		t.Fatal("décharger doit oublier le modèle choisi")
	}
}
