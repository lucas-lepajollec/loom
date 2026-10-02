package resources

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSkillFrontMatterParsing(t *testing.T) {
	meta, body := ParseSkillMarkdown("\uFEFF---\nname: x\ndescription: 'a: b'\nmetadata:\n  title: Titre\n  loom-id: 7\n---\n\nCorps\n")
	if meta["name"] != "x" || meta["description"] != "a: b" || meta["metadata.title"] != "Titre" || meta["metadata.loom-id"] != "7" || body != "Corps" {
		t.Fatalf("%v %q", meta, body)
	}
	if _, body := ParseSkillMarkdown("Sans en-tête"); body != "Sans en-tête" {
		t.Fatal(body)
	}
}

func TestLibraryAndSinkKeepBindingsAndForeignFolders(t *testing.T) {
	root := t.TempDir()
	lib := Library{Root: root, NewID: func() string { return "1234" }}
	dir, err := lib.WriteSkill(Capability{Name: "Revue de code", Description: "Relire", Instructions: "Cherche les bugs."}, "")
	if err != nil {
		t.Fatal(err)
	}
	source := SkillSource{ID: "loom", Path: root, Label: "Loom", Writable: true}
	skills := lib.ScanSkills([]SkillSource{source})
	if len(skills) != 1 || skills[0].ID != "1234" || skills[0].Name != "Revue de code" || skills[0].Dir != dir {
		t.Fatalf("library: %+v", skills)
	}
	target := t.TempDir()
	foreign := filepath.Join(target, "mine")
	if err := os.MkdirAll(foreign, 0755); err != nil {
		t.Fatal(err)
	}
	list := []SkillSinkTarget{{ID: "claude", Dir: target, Enabled: true}}
	list = lib.SyncSkillSinks(list, skills, map[string]map[string]bool{}, func(s string) string { return s })
	if len(list[0].Written) != 1 {
		t.Fatalf("sink: %+v", list)
	}
	list = lib.SyncSkillSinks(list, skills, map[string]map[string]bool{"1234": {"claude": false}}, func(s string) string { return s })
	if len(list[0].Written) != 0 {
		t.Fatalf("excluded skill retained: %+v", list)
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Fatal("foreign folder changed", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "SKILL.md")); err != nil {
		t.Fatal("source changed", err)
	}
}
