package store

import (
	"path/filepath"
	"testing"
)

func testDB(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "loom.db")
}

// La configuration écrite doit se relire à l'identique, et une clé vidée
// disparaître au lieu de rester à "".
func TestConfigAllerRetour(t *testing.T) {
	path := testDB(t)
	if err := ReplaceKV(path, BucketConfig, map[string]string{"MODEL": "m.gguf", "CTX": "4096"}); err != nil {
		t.Fatal(err)
	}
	if cfg := CachedKV(path, BucketConfig); cfg["MODEL"] != "m.gguf" || cfg["CTX"] != "4096" {
		t.Fatalf("configuration relue incorrecte : %v", cfg)
	}
	if err := PutStr(path, BucketConfig, "CTX", ""); err != nil {
		t.Fatal(err)
	}
	if _, ok := CachedKV(path, BucketConfig)["CTX"]; ok {
		t.Error("CTX vidée mais toujours présente")
	}
}

// ReplaceKV REMPLACE : aucune clé de l'ancienne configuration ne doit
// survivre, sans quoi une bascule de preset laisserait des réglages fantômes.
func TestReplaceKVRemplaceTout(t *testing.T) {
	path := testDB(t)
	if err := ReplaceKV(path, BucketConfig, map[string]string{"A": "1", "B": "2"}); err != nil {
		t.Fatal(err)
	}
	if err := ReplaceKV(path, BucketConfig, map[string]string{"B": "3"}); err != nil {
		t.Fatal(err)
	}
	cfg := CachedKV(path, BucketConfig)
	if _, ok := cfg["A"]; ok {
		t.Error("A a survécu au remplacement")
	}
	if cfg["B"] != "3" {
		t.Errorf("B = %q, attendu 3", cfg["B"])
	}
}
