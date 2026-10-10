package loom

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
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

// Pi hides a provider whose "$LOOM_KEY_…" reference is unset, so the catalog
// probe must receive every projected key, not only the chat-time one.
func TestPiProbeReceivesProjectedProviderKeys(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell stand-in")
	}
	jarvisTestSetup(t)
	home, _ := os.UserHomeDir()
	bin := filepath.Join(home, ".local", "bin")
	out := filepath.Join(t.TempDir(), "env")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "pi"), []byte("#!/bin/sh\nenv > "+out+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	p := CloudProvider{ID: "p1", Name: "Fixture", Model: "m", Endpoint: "https://fixture.invalid/v1"}
	if err := putStoreJSON(bkProviders, p.ID, p); err != nil {
		t.Fatal(err)
	}
	workspaceSessions.keys[p.ID] = "fixture-key"
	if err := putStoreJSON(bkState, modelSinkState, map[string]bool{"pi": true}); err != nil {
		t.Fatal(err)
	}
	c, err := startNativeAgent(acpAgent{ID: "pi"}, RuntimeSession{ACPState: ACPState{Workdir: t.TempDir()}}, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var env []byte
	for i := 0; i < 100 && len(env) == 0; i++ {
		time.Sleep(20 * time.Millisecond)
		env, _ = os.ReadFile(out)
	}
	if !strings.Contains(string(env), providerKeyEnv(p.ID)+"=fixture-key") || !strings.Contains(string(env), "LOOM_API_KEY=") {
		t.Fatalf("probe env lacks projected keys:\n%s", env)
	}
}

func TestHermesProvidersKeepUserConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	orig := "# my settings\nmodel:\n  provider: openrouter\nproviders:\n  mine:\n    base_url: https://x.invalid/v1\n    key_env: MINE_KEY\n  loom-old:\n    base_url: http://stale\n"
	if err := os.WriteFile(path, []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}
	loom := map[string]map[string]any{"loom-fake": {"name": "Fake (Loom)", "base_url": "http://127.0.0.1:2790/v1", "key_env": "LOOM_KEY_X", "models": []string{"m"}}}
	if err := writeHermesProviders(path, true, loom); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	s := string(b)
	if !strings.Contains(s, "# my settings") || !strings.Contains(s, "provider: openrouter") || !strings.Contains(s, "MINE_KEY") || strings.Contains(s, "loom-old") || !strings.Contains(s, "loom-fake:") || !strings.Contains(s, "LOOM_KEY_X") {
		t.Fatalf("unexpected config:\n%s", s)
	}
	if err := writeHermesProviders(path, false, nil); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(path)
	if strings.Contains(string(b), "loom-fake") || !strings.Contains(string(b), "MINE_KEY") {
		t.Fatalf("disable did not remove only Loom entries:\n%s", b)
	}
	if err := os.WriteFile(path, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if writeHermesProviders(path, true, loom) == nil {
		t.Fatal("broken config rewritten")
	}
}

func TestDshPatchArgsInsertAfterProfile(t *testing.T) {
	testHome(t)
	if err := putStoreJSON(bkState, modelSinkState, map[string]bool{"deepseek-harness": true}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(dshPatchPath()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dshPatchPath(), []byte("[]"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := acpAgent{ID: "deepseek-harness", Args: []string{"-y", "@deepseek-ai/dsh@0.2.0-rc.2", "--profile", "acp"}}
	got := strings.Join(dshPatchArgs(a, a.Args), " ")
	if got != "-y @deepseek-ai/dsh@0.2.0-rc.2 --profile acp --patch "+dshPatchPath() {
		t.Fatal(got)
	}
	a.Remote = true
	if len(dshPatchArgs(a, a.Args)) != 4 {
		t.Fatal("remote agent got a local patch")
	}
}
