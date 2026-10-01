package store

import "testing"

// Le cache de lecture ne doit jamais servir une valeur périmée après une
// écriture de ce process : un « loom edit » suivi d'un « loom restart » doit
// démarrer sur la NOUVELLE configuration.
func TestCacheConfigInvalideParEcriture(t *testing.T) {
	path := testDB(t)
	if err := ReplaceKV(path, BucketConfig, map[string]string{"CTX": "4096"}); err != nil {
		t.Fatal(err)
	}
	if got := CachedKV(path, BucketConfig)["CTX"]; got != "4096" { // remplit le cache
		t.Fatalf("CTX = %q, attendu 4096", got)
	}
	if err := PutStr(path, BucketConfig, "CTX", "8192"); err != nil {
		t.Fatal(err)
	}
	if got := CachedKV(path, BucketConfig)["CTX"]; got != "8192" {
		t.Fatalf("valeur périmée servie par le cache : CTX = %q, attendu 8192", got)
	}
	if err := ReplaceKV(path, BucketConfig, map[string]string{"MODEL": "m.gguf"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := CachedKV(path, BucketConfig)["CTX"]; ok {
		t.Error("ReplaceKV remplace tout : CTX ne doit plus exister")
	}
}

// La carte renvoyée appartient à l'appelant : la modifier ne doit pas polluer
// le cache, sans quoi un appelant distrait corromprait la configuration vue par
// tous les autres.
func TestCacheConfigRenvoieUneCopie(t *testing.T) {
	path := testDB(t)
	if err := ReplaceKV(path, BucketConfig, map[string]string{"CTX": "4096"}); err != nil {
		t.Fatal(err)
	}
	cfg := CachedKV(path, BucketConfig)
	cfg["CTX"] = "999"
	delete(cfg, "CTX")
	if got := CachedKV(path, BucketConfig)["CTX"]; got != "4096" {
		t.Fatalf("le cache a été altéré par l'appelant : CTX = %q", got)
	}
}
