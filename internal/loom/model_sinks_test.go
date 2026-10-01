package loom

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPiProviderSinkOnlyTouchesLoomProvider(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models.json")
	orig := `{"providers":{"mine":{"baseUrl":"http://x/v1","apiKey":"secret","models":[{"id":"a"}]}},"other":1}`
	if err := os.WriteFile(path, []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writePiProvider(path, true, []string{"q.gguf"}, "http://127.0.0.1:2595/v1", ""); err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	b, _ := os.ReadFile(path)
	_ = json.Unmarshal(b, &doc)
	prov := doc["providers"].(map[string]any)
	loom := prov["loom"].(map[string]any)
	if prov["mine"].(map[string]any)["apiKey"] != "secret" || doc["other"].(float64) != 1 || loom["baseUrl"] != "http://127.0.0.1:2595/v1" {
		t.Fatalf("%s", b)
	}
	if m := loom["models"].([]any)[0].(map[string]any); m["id"] != "q.gguf" || m["name"] != "q" {
		t.Fatalf("%v", m)
	}
	if err := writePiProvider(path, false, nil, "", ""); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(path)
	_ = json.Unmarshal(b, &doc)
	if _, ok := doc["providers"].(map[string]any)["loom"]; ok {
		t.Fatal("loom provider not removed")
	}
	if err := os.WriteFile(path, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if writePiProvider(path, true, nil, "u", "") == nil {
		t.Fatal("unreadable file must not be overwritten")
	}
}

func TestCodexLoomModelEnv(t *testing.T) {
	testHome(t)
	if env := acpLoomModelEnv("codex", "gpt-6.1-sol[high]"); env != nil {
		t.Fatalf("modèle natif: aucun environnement attendu, reçu %v", env)
	}
	if env := acpLoomModelEnv("pi", "loom:x"); env != nil {
		t.Fatalf("Pi passe par son fichier, pas par l'environnement: %v", env)
	}
	env := acpLoomModelEnv("codex", "loom:Qwen3-8B.gguf")
	if len(env) != 3 || !strings.HasPrefix(env[0], "CODEX_CONFIG=") || env[1] != "MODEL_PROVIDER=loom" || !strings.HasPrefix(env[2], "LOOM_API_KEY=") {
		t.Fatalf("%v", env)
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(strings.TrimPrefix(env[0], "CODEX_CONFIG=")), &cfg); err != nil || cfg["model"] != "Qwen3-8B.gguf" || cfg["model_provider"] != "loom" {
		t.Fatalf("%v %v", err, cfg)
	}
	provider := cfg["model_providers"].(map[string]any)["loom"].(map[string]any)
	if provider["wire_api"] != "responses" || !strings.HasSuffix(provider["base_url"].(string), "/v1") || strings.Contains(env[0], "LOOM_API_KEY=") {
		t.Fatalf("%v", provider)
	}
}
