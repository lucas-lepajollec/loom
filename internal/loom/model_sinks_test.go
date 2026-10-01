package loom

import (
	"encoding/json"
	"os"
	"path/filepath"
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
