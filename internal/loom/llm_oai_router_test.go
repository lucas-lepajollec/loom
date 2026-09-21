package loom

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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
