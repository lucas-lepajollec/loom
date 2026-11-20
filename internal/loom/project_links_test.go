package loom

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectCandidatesAndRemoteCredentials(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "docs"), 0o755)
	for _, f := range []string{"README.md", "docs/plan.md", "docs/image.png", "main.go"} {
		os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o644)
	}
	got := []string{}
	for _, c := range projectCandidates(dir) {
		got = append(got, c.Path)
	}
	if strings.Join(got, ",") != "README.md,docs/plan.md" {
		t.Fatalf("%v", got)
	}
	if r := cleanRemote("https://moi:jeton@forge.example/lucas/loom.git"); strings.Contains(r, "jeton") || !strings.Contains(r, "forge.example/lucas/loom.git") {
		t.Fatal(r)
	}
}
