package loom

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestSkillSinksWriteOnlyLoomFoldersAndCleanUp(t *testing.T) {
	testHome(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	foreign := filepath.Join(home, ".claude", "skills", "mine")
	if err := os.MkdirAll(foreign, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := saveCapability(Capability{Name: "Revue de code", Description: "Relire un diff", Instructions: "Cherche les bugs."}); err != nil {
		t.Fatal(err)
	}
	list := loadSkillSinks()
	list[0].Enabled = true
	if err := saveSkillSinks(list); err != nil {
		t.Fatal(err)
	}
	out := syncSkillSinks()
	link := filepath.Join(home, ".claude", "skills", "revue-de-code")
	if target, err := os.Readlink(link); err != nil || target != filepath.Join(loomSkillsDir(), "revue-de-code") {
		t.Fatalf("lien attendu vers le dossier de Loom: %q %v", target, err)
	}
	b, err := os.ReadFile(filepath.Join(link, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "name: revue-de-code") || !strings.Contains(string(b), "title: \"Revue de code\"") || !strings.Contains(string(b), "Cherche les bugs.") {
		t.Fatalf("SKILL.md inattendu:\n%s", b)
	}
	if len(out[0].Written) != 1 || len(out[1].Written) != 0 {
		t.Fatalf("manifest %+v", out)
	}
	if _, err := os.Stat(filepath.Join(home, ".agents", "skills")); !os.IsNotExist(err) {
		t.Fatal("cible désactivée écrite")
	}
	list = loadSkillSinks()
	list[0].Enabled = false
	_ = saveSkillSinks(list)
	syncSkillSinks()
	if _, err := os.Lstat(filepath.Join(home, ".claude", "skills", "revue-de-code")); !os.IsNotExist(err) {
		t.Fatal("lien Loom non retiré")
	}
	if _, err := os.Stat(filepath.Join(loomSkillsDir(), "revue-de-code", "SKILL.md")); err != nil {
		t.Fatal("la skill elle-même ne doit pas être touchée")
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Fatal("dossier étranger touché")
	}
}

func TestSkillFoldersLinkedSourcesAndMigration(t *testing.T) {
	testHome(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	// A skill stored in the database before folders existed keeps its id.
	if err := putStoreJSON(bkCapabilities, "1234", Capability{ID: "1234", Name: "Ancienne", Description: "d", Instructions: "Corps ancien"}); err != nil {
		t.Fatal(err)
	}
	skillMigrateOnce = sync.Once{}
	if c, ok := getCapability("1234"); !ok || c.Instructions != "Corps ancien" || c.Source != "loom" || c.ReadOnly {
		t.Fatalf("migration: %+v %v", c, ok)
	}
	// A linked folder (here the user's own Claude Code skills) is listed read-only.
	mine := filepath.Join(home, ".claude", "skills", "perso")
	_ = os.MkdirAll(mine, 0o755)
	_ = os.WriteFile(filepath.Join(mine, "SKILL.md"), []byte("---\nname: perso\ndescription: \"Mes règles\"\n---\nFais court."), 0o644)
	_ = os.WriteFile(filepath.Join(mine, "aide.sh"), []byte("echo"), 0o755)
	_ = putStoreJSON(bkState, skillSourcesState, []SkillSource{{ID: "dir-x", Path: filepath.Join(home, ".claude", "skills"), Label: "Claude Code"}})
	c, ok := getCapability("dir-x:perso")
	if !ok || !c.ReadOnly || c.Description != "Mes règles" || c.Instructions != "Fais court." || c.Files != 1 {
		t.Fatalf("source liée: %+v %v", c, ok)
	}
	if _, err := saveCapability(Capability{ID: c.ID, Name: "x", Instructions: "y"}); err == nil {
		t.Fatal("skill liée réécrite")
	}
	if err := deleteCapability(c.ID); err == nil {
		t.Fatal("skill liée supprimée")
	}
	// A user folder with the same name as a Loom skill is never replaced.
	_ = os.MkdirAll(filepath.Join(home, ".claude", "skills", "ancienne"), 0o755)
	// Distributed to the other harnesses' folder, not back into its own folder.
	list := loadSkillSinks()
	for i := range list {
		list[i].Enabled = true
	}
	_ = saveSkillSinks(list)
	out := syncSkillSinks()
	if _, err := os.Readlink(filepath.Join(home, ".agents", "skills", "perso")); err != nil {
		t.Fatalf("skill liée non distribuée: %+v", out)
	}
	if _, err := os.Readlink(mine); err == nil {
		t.Fatal("la skill a été remplacée par un lien vers elle-même")
	}
	if out[0].Error == "" {
		t.Fatal("collision non signalée")
	}
	if _, err := os.Readlink(filepath.Join(home, ".claude", "skills", "ancienne")); err == nil {
		t.Fatal("dossier de l'utilisateur remplacé")
	}
}
