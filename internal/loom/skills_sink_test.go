package loom

import (
	"os"
	"path/filepath"
	"strings"
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
	b, err := os.ReadFile(filepath.Join(home, ".claude", "skills", "loom-revue-de-code", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "name: loom-revue-de-code") || !strings.Contains(string(b), "Cherche les bugs.") {
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
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills", "loom-revue-de-code")); !os.IsNotExist(err) {
		t.Fatal("dossier Loom non retiré")
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Fatal("dossier étranger touché")
	}
}
