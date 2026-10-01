package loom

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProviderProtocols(t *testing.T) {
	cases := map[string][2]string{
		"https://openrouter.ai/api/v1": {"https://openrouter.ai/api/v1", "https://openrouter.ai/api"},
		"https://api.deepseek.com/v1":  {"", "https://api.deepseek.com/anthropic"},
		"https://api.openai.com/v1":    {"https://api.openai.com/v1", ""},
		"https://api.anthropic.com/v1": {"", "https://api.anthropic.com"},
		"https://api.mistral.ai/v1":    {"", ""},
		"http://192.168.1.10:8000/v1/": {"", ""},
	}
	for endpoint, want := range cases {
		p := providerProtocols(endpoint)
		if p["responses"] != want[0] || p["anthropic"] != want[1] || p["chat"] == "" {
			t.Fatalf("%s: %v", endpoint, p)
		}
	}
}

func TestHarnessEnvForLocalAndProvider(t *testing.T) {
	testHome(t)
	env := strings.Join(acpLoomModelEnv("claude-code", "loom:Gemma.gguf"), "\n")
	for _, want := range []string{"ANTHROPIC_MODEL=Gemma.gguf", "ANTHROPIC_DEFAULT_HAIKU_MODEL=Gemma.gguf", "ANTHROPIC_AUTH_TOKEN=", "ANTHROPIC_BASE_URL=http://127.0.0.1:"} {
		if !strings.Contains(env, want) {
			t.Fatalf("manque %q dans\n%s", want, env)
		}
	}
	if strings.Contains(env, "/v1\n") {
		t.Fatal("l'API Anthropic se joint sans /v1")
	}
	p, err := workspaceSessions.saveProvider(CloudProvider{Name: "OpenRouter", Endpoint: "https://openrouter.ai/api/v1", Model: "z/free", Models: []string{"z/free"}}, "sk-test-123")
	if err != nil {
		t.Fatal(err)
	}
	codex := strings.Join(acpLoomModelEnv("codex", "provider:"+p.ID+":z/free"), "\n")
	if !strings.Contains(codex, `"base_url":"https://openrouter.ai/api/v1"`) || !strings.Contains(codex, `"model":"z/free"`) || !strings.Contains(codex, "LOOM_API_KEY=sk-test-123") {
		t.Fatal(codex)
	}
	if acpLoomModelEnv("codex", "provider:inconnu:x") != nil || acpLoomModelEnv("gemini", "loom:x") != nil || acpLoomModelEnv("codex", "gpt-6") != nil {
		t.Fatal("source inconnue ou harness sans format : rien attendu")
	}
	_ = putStoreJSON(bkState, modelSinkState, map[string]bool{"claude-code": true})
	choices := harnessLoomChoices(RuntimeDescriptor{ID: "claude-code", Name: "Claude Code"}, true)
	found := false
	for _, c := range choices {
		if c.Model == "provider:"+p.ID+":z/free" && c.Via == "Loom · OpenRouter" {
			found = true
		}
	}
	if !found {
		t.Fatalf("OpenRouter absent des choix de Claude Code: %+v", choices)
	}
}

func TestPiAndOpenCodeListLoomSourcesWithoutWritingKeys(t *testing.T) {
	testHome(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	p, err := workspaceSessions.saveProvider(CloudProvider{Name: "OpenRouter", Endpoint: "https://openrouter.ai/api/v1", Model: "a/b", Models: []string{"a/b"}}, "sk-secret-123")
	if err != nil {
		t.Fatal(err)
	}
	_ = putStoreJSON(bkState, modelSinkState, map[string]bool{"pi": true, "opencode": true})
	// Pi: the provider is declared with a reference, the key travels in the environment.
	piFile := filepath.Join(home, "models.json")
	if err := writePiProvider(piFile, true, []string{"Local.gguf"}, "http://127.0.0.1:1/v1", "k", piCloudProviders()); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(piFile)
	if strings.Contains(string(b), "sk-secret-123") || !strings.Contains(string(b), `"$`+providerKeyEnv(p.ID)+`"`) || !strings.Contains(string(b), `"loom-openrouter"`) {
		t.Fatalf("models.json de Pi:\n%s", b)
	}
	env := strings.Join(acpLaunchEnv("pi", ""), "\n")
	if !strings.Contains(env, providerKeyEnv(p.ID)+"=sk-secret-123") {
		t.Fatalf("clé absente de l'environnement de Pi: %s", env)
	}
	// OpenCode: configuration in the environment, key as {env:…}.
	env = strings.Join(acpLaunchEnv("opencode", ""), "\n")
	var cfgLine string
	for _, l := range strings.Split(env, "\n") {
		if strings.HasPrefix(l, "OPENCODE_CONFIG_CONTENT=") {
			cfgLine = l
		}
	}
	if cfgLine == "" || strings.Contains(cfgLine, "sk-secret-123") || !strings.Contains(cfgLine, "{env:"+providerKeyEnv(p.ID)+"}") {
		t.Fatalf("config OpenCode: %s", cfgLine)
	}
	// Disabling removes every Loom provider from Pi's file.
	if err := writePiProvider(piFile, false, nil, "", ""); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(piFile)
	if strings.Contains(string(b), "loom") {
		t.Fatalf("fournisseurs Loom restants:\n%s", b)
	}
	// Harness not enabled: nothing passed.
	_ = putStoreJSON(bkState, modelSinkState, map[string]bool{})
	if len(acpLaunchEnv("opencode", "")) != 0 {
		t.Fatal("sources passées sans activation")
	}
}
