package loom

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectContextFilesStayInsideFolder(t *testing.T) {
	testHome(t)
	dir := t.TempDir()
	outside := t.TempDir()
	os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("Règle du dépôt : tests d'abord."), 0o644)
	os.WriteFile(filepath.Join(outside, "secret.md"), []byte("non"), 0o644)
	os.Symlink(filepath.Join(outside, "secret.md"), filepath.Join(dir, "lien.md"))
	for _, bad := range []string{"../" + filepath.Base(outside) + "/secret.md", filepath.Join(outside, "secret.md"), "lien.md", "absent.md"} {
		if _, err := validProjectContextFiles(dir, []string{bad}); err == nil {
			t.Fatalf("accepté: %s", bad)
		}
	}
	p, err := saveProjectContext(ChatProject{Name: "Dépôt", Directory: dir, ContextFiles: []string{"AGENTS.md", "./AGENTS.md"}})
	if err != nil || len(p.ContextFiles) != 1 {
		t.Fatalf("%v %v", err, p.ContextFiles)
	}
	c := discussionContext(RuntimeSession{ID: "s", ProjectID: p.ID})
	if !strings.Contains(c.System, "Project file AGENTS.md:\nRègle du dépôt") {
		t.Fatalf("contexte: %q", c.System)
	}
	os.Remove(filepath.Join(dir, "AGENTS.md"))
	if c := discussionContext(RuntimeSession{ID: "s", ProjectID: p.ID}); c.Warning == "" || strings.Contains(c.System, "Règle") {
		t.Fatalf("fichier supprimé: %+v", c)
	}
	if projectWorkdir(p.ID) != dir {
		t.Fatal("dossier de travail par défaut absent")
	}
}

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
