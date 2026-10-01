package loom

import (
	"os"
	"path/filepath"
	"testing"
)

func writeLlamaServer(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestEngineKindFullVsServer(t *testing.T) {
	testHome(t)
	root := t.TempDir()

	repo := filepath.Join(root, "llama.cpp")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	full := filepath.Join(repo, "build", "bin", "llama-server")
	writeLlamaServer(t, full)
	if got := engineKind(full); got != "full" {
		t.Fatalf("kind(full) = %q, attendu full", got)
	}
	if got := engineRepo(full); got != repo {
		t.Fatalf("repo = %q, attendu %s", got, repo)
	}

	lone := filepath.Join(root, "only", "llama-server")
	writeLlamaServer(t, lone)
	if got := engineKind(lone); got != "server" {
		t.Fatalf("kind(lone) = %q, attendu server (repo %q)", got, engineRepo(lone))
	}
	if got := engineRepo(lone); got != "" {
		t.Fatalf("repo(lone) = %q, attendu vide", got)
	}
	if engineKind("") != "" || engineKind(filepath.Join(root, "missing")) != "" {
		t.Fatal("un chemin vide ou absent ne doit pas être un moteur")
	}
}

func TestProbedLlamaServersIncludesConfigBIN(t *testing.T) {
	testHome(t)
	bin := filepath.Join(t.TempDir(), "llama-server")
	writeLlamaServer(t, bin)
	if err := WriteConfig(map[string]string{"BIN": bin}); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range probedLlamaServers() {
		if samePath(p, bin) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("probes = %v, attendu %s", probedLlamaServers(), bin)
	}
}

func TestResolveInstallDir(t *testing.T) {
	testHome(t)
	def := defaultRepoDir()
	got, err := resolveInstallDir("")
	if err != nil || got != def {
		t.Fatalf("vide = %q (%v), attendu %s", got, err, def)
	}
	if _, err := resolveInstallDir("relatif"); err == nil {
		t.Fatal("un chemin relatif devrait être refusé")
	}
	file := filepath.Join(t.TempDir(), "llama-server")
	writeLlamaServer(t, file)
	if _, err := resolveInstallDir(file); err == nil {
		t.Fatal("un binaire ne doit pas passer pour un dossier d'install")
	}
}
