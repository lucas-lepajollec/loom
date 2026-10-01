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

func TestProjectFoldersFollowTheHarnessMachine(t *testing.T) {
	testHome(t)
	t.Setenv("HOME", t.TempDir())
	main, extra := t.TempDir(), t.TempDir()
	local, err := saveProjectContext(ChatProject{Name: "Ici", Directory: main, ExtraDirs: []string{extra, main, ""}})
	if err != nil || len(local.ExtraDirs) != 1 {
		t.Fatalf("%v %+v", err, local)
	}
	if dir, more := projectFolders(local.ID, acpAgent{}); dir != main || len(more) != 1 || more[0] != extra {
		t.Fatalf("harness local: %q %v", dir, more)
	}
	if dir, _ := projectFolders(local.ID, acpAgent{Remote: true, Machine: "box"}); dir != "" {
		t.Fatal("dossier local donné à un harness distant")
	}
	if err := saveRemoteMachine(RemoteMachine{ID: "box", Name: "box", Host: "10.0.0.2", User: "root", Port: 22}, nil); err != nil {
		t.Fatal(err)
	}
	remote, err := saveProjectContext(ChatProject{Name: "Là-bas", Machine: "box", Directory: "/srv/app"})
	if err != nil {
		t.Fatal(err)
	}
	if dir, _ := projectFolders(remote.ID, acpAgent{Remote: true, Machine: "box"}); dir != "/srv/app" {
		t.Fatalf("harness de la machine: %q", dir)
	}
	if dir, _ := projectFolders(remote.ID, acpAgent{Remote: true, Machine: "autre"}); dir != "" {
		t.Fatal("dossier donné à une autre machine")
	}
	if dir, _ := projectFolders(remote.ID, acpAgent{}); dir != "" {
		t.Fatal("dossier distant donné à un harness local")
	}
	if _, err := saveProjectContext(ChatProject{ID: remote.ID, Name: "Là-bas", Machine: "box", Directory: "/srv/app", ContextFiles: []string{"README.md"}}); err == nil {
		t.Fatal("fichiers de contexte distants acceptés")
	}
	if _, err := saveProjectContext(ChatProject{Name: "X", Machine: "inconnue", Directory: "/a"}); err == nil {
		t.Fatal("machine inconnue acceptée")
	}
}
