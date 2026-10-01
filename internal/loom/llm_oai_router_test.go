package loom

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

func TestLlamaBackendPortFor(t *testing.T) {
	if got := llamaBackendPortFor(8081); got != 18081 {
		t.Fatalf("8081 → %d, attendu 18081", got)
	}
	if got := llamaBackendPortFor(60000); got != 50000 {
		t.Fatalf("60000 → %d, attendu 50000", got)
	}
	if llamaBackendPortFor(8081) == 8081 {
		t.Fatal("le port interne ne doit pas coller au port public")
	}
}

func TestOAIKeepCurrent(t *testing.T) {
	for _, id := range []string{"", "loom", "loom", "local", "gpt-4o", "GPT-4"} {
		if !oaiKeepCurrent(id) {
			t.Errorf("%q devrait garder le modèle chargé", id)
		}
	}
	if oaiKeepCurrent("Qwen3.5-9B.gguf") {
		t.Fatal("un GGUF réel ne doit pas être traité comme alias")
	}
}

func TestOAICatalogListsUnloaded(t *testing.T) {
	home := testHome(t)
	t.Setenv("LOOM_HOME", home)
	models := filepath.Join(home, "models")
	if err := os.MkdirAll(models, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"alpha.gguf", "beta.gguf", "mmproj-F16.gguf"} {
		if err := os.WriteFile(filepath.Join(models, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	presets := filepath.Join(home, "presets")
	if err := os.MkdirAll(presets, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(presets, "rapide.env"), []byte("# NAME=Rapide\nMODEL=beta.gguf\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	setConfig(t, "BIN=/usr/bin/llama-server\nHOST=127.0.0.1\nPORT=8081\nMODEL="+filepath.Join(models, "alpha.gguf")+"\n")

	ids := map[string]oaiEntry{}
	for _, e := range oaiCatalog() {
		ids[e.ID] = e
	}
	if _, ok := ids["alpha.gguf"]; !ok {
		t.Fatalf("alpha chargé absent du catalogue : %v", ids)
	}
	if _, ok := ids["beta.gguf"]; !ok {
		t.Fatalf("beta non chargé absent du catalogue : %v", ids)
	}
	if _, ok := ids["mmproj-F16.gguf"]; ok {
		t.Fatal("un projecteur vision ne doit pas apparaître")
	}
	if _, ok := ids["Rapide"]; !ok {
		t.Fatalf("preset Rapide absent : %v", ids)
	}

	if !oaiAlreadyLoaded("alpha.gguf") {
		t.Fatal("alpha devrait compter comme chargé")
	}
	if oaiAlreadyLoaded("beta.gguf") {
		t.Fatal("beta ne doit pas compter comme chargé")
	}
	if oaiAlreadyLoaded("loom") {
		// loom = garder le courant : AlreadyLoaded est true
	} else {
		t.Fatal("loom = modèle courant")
	}
	if e, ok := resolveOAIModel("beta.gguf"); !ok || e.Kind != "gguf" {
		t.Fatalf("resolve beta : %+v %v", e, ok)
	}
	if e, ok := resolveOAIModel("Rapide"); !ok || e.Kind != "preset" {
		t.Fatalf("resolve Rapide : %+v %v", e, ok)
	}
}

func TestOAIModelsHTTP(t *testing.T) {
	home := testHome(t)
	t.Setenv("LOOM_HOME", home)
	models := filepath.Join(home, "models")
	if err := os.MkdirAll(models, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(models, "nu.gguf"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	setConfig(t, "BIN=/usr/bin/llama-server\nHOST=127.0.0.1\nPORT=8081\nMODEL="+filepath.Join(models, "nu.gguf")+"\n")

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()
	oaiPublicHandler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("GET /v1/models → %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Object string `json:"object"`
		Data   []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Object != "list" {
		t.Fatalf("object=%q", out.Object)
	}
	found := false
	for _, it := range out.Data {
		if it.ID == "nu.gguf" {
			found = true
		}
	}
	if !found {
		t.Fatalf("nu.gguf absent : %s", rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"inconnu.gguf","messages":[]}`))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	oaiPublicHandler().ServeHTTP(rec, req)
	if rec.Code != 404 {
		t.Fatalf("modèle inconnu → %d %s", rec.Code, rec.Body.String())
	}
}

func TestOAIModelsRequiresBearerWhenLANExposed(t *testing.T) {
	home := testHome(t)
	t.Setenv("LOOM_HOME", home)
	if err := SetConfigKey("HOST", "0.0.0.0"); err != nil {
		t.Fatal(err)
	}
	if err := writeAPIKey("test-only-key"); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name   string
		auth   string
		status int
	}{
		{"missing", "", http.StatusUnauthorized},
		{"raw token without scheme", "test-only-key", http.StatusUnauthorized},
		{"basic scheme", "Basic test-only-key", http.StatusUnauthorized},
		{"wrong token", "Bearer wrong-key", http.StatusUnauthorized},
		{"bearer token", "Bearer test-only-key", http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
			if tc.auth != "" {
				req.Header.Set("Authorization", tc.auth)
			}
			rec := httptest.NewRecorder()
			oaiPublicHandler().ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("Authorization=%q: got %d, want %d", tc.name, rec.Code, tc.status)
			}
		})
	}
	if err := putStr(bkState, "api_key", ""); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer test-only-key")
	rec := httptest.NewRecorder()
	oaiPublicHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("LAN without stored key: got %d, want 401", rec.Code)
	}
}

func TestOAIModelsLANAuthOverHTTP(t *testing.T) {
	testHome(t)
	if err := SetConfigKey("HOST", "0.0.0.0"); err != nil {
		t.Fatal(err)
	}
	if err := writeAPIKey("test-only-key"); err != nil {
		t.Fatal(err)
	}

	// httptest binds only to loopback: exercise the real HTTP stack without
	// opening a port on the developer's LAN or starting llama-server.
	srv := httptest.NewServer(oaiPublicHandler())
	defer srv.Close()
	check := func(name, authorization string, want int) {
		t.Helper()
		t.Run(name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, srv.URL+"/v1/models", nil)
			if err != nil {
				t.Fatal(err)
			}
			if authorization != "" {
				req.Header.Set("Authorization", authorization)
			}
			resp, err := srv.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != want {
				t.Fatalf("status = %d, want %d", resp.StatusCode, want)
			}
			if want == http.StatusUnauthorized && resp.Header.Get("WWW-Authenticate") == "" {
				t.Fatal("missing Bearer challenge")
			}
		})
	}

	check("missing key", "", http.StatusUnauthorized)
	check("raw token", "test-only-key", http.StatusUnauthorized)
	check("wrong Bearer token", "Bearer wrong-key", http.StatusUnauthorized)
	check("valid Bearer token", "Bearer test-only-key", http.StatusOK)

	if err := writeAPIKey("rotated-test-key"); err != nil {
		t.Fatal(err)
	}
	check("old key after rotation", "Bearer test-only-key", http.StatusUnauthorized)
	check("new key after rotation", "Bearer rotated-test-key", http.StatusOK)

	// Simulate a damaged configuration without using the normal setter, which
	// deliberately forbids removing a key while LAN exposure is enabled.
	if err := putStr(bkState, "api_key", ""); err != nil {
		t.Fatal(err)
	}
	check("exposed and keyless fails closed", "Bearer rotated-test-key", http.StatusUnauthorized)
}

func TestOAIPublicProxyRejectsUnauthorizedBeforeBackend(t *testing.T) {
	testHome(t)
	if err := SetConfigKey("HOST", "0.0.0.0"); err != nil {
		t.Fatal(err)
	}
	if err := writeAPIKey("test-only-key"); err != nil {
		t.Fatal(err)
	}

	var backendCalls atomic.Int32
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backendCalls.Add(1)
		if r.URL.Path != "/health" || r.Header.Get("Authorization") != "Bearer test-only-key" {
			http.Error(w, "unexpected proxy request", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()
	backendURL, err := url.Parse(backend.URL)
	if err != nil {
		t.Fatal(err)
	}
	backendPort, err := strconv.Atoi(backendURL.Port())
	if err != nil || backendPort <= 10000 {
		t.Fatalf("unexpected test backend port %d: %v", backendPort, err)
	}
	if err := SetConfigKey("PORT", strconv.Itoa(backendPort-10000)); err != nil {
		t.Fatal(err)
	}
	front := httptest.NewServer(oaiPublicHandler())
	defer front.Close()

	request := func(auth string) int {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, front.URL+"/health", nil)
		if err != nil {
			t.Fatal(err)
		}
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		resp, err := front.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}

	if got := request(""); got != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", got)
	}
	if got := backendCalls.Load(); got != 0 {
		t.Fatalf("unauthorized request reached backend %d times", got)
	}
	if got := request("Bearer test-only-key"); got != http.StatusOK {
		t.Fatalf("authorized proxy status = %d", got)
	}
	if got := backendCalls.Load(); got != 1 {
		t.Fatalf("authorized request reached backend %d times, want 1", got)
	}
}
