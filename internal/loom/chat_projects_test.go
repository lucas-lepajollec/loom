package loom

import (
	"testing"
)

func TestChatProjectsAssignAndOpen(t *testing.T) {
	testHome(t)
	p, err := createProject("Docs")
	if err != nil {
		t.Fatal(err)
	}
	if p.ID == "" || p.Name != "Docs" {
		t.Fatalf("projet inattendu: %+v", p)
	}
	if n := len(listProjects()); n != 1 {
		t.Fatalf("listProjects = %d, attendu 1", n)
	}

	c := newHistTestConv()
	c.ActiveProject = p.ID
	c.appendDelta(c.epoch, map[string]any{"user": "bonjour projet"})
	c.appendDelta(c.epoch, map[string]any{"content": "ok"})
	c.upsertSession()

	list := listArchives()
	if len(list) != 1 || list[0].ProjectID != p.ID {
		t.Fatalf("archive project_id = %+v", list)
	}

	c.NewSession()
	if c.currentProject() != "" {
		t.Fatalf("nouvelle session devrait quitter le projet, got %q", c.currentProject())
	}
	if err := c.OpenSession(list[0].ID); err != nil {
		t.Fatal(err)
	}
	if c.currentProject() != p.ID {
		t.Fatalf("ouverture : project = %q, attendu %q", c.currentProject(), p.ID)
	}

	c.NewSession()
	c.setActiveProject(p.ID)
	if c.currentProject() != p.ID {
		t.Fatal("setActiveProject n'a pas tenu")
	}

	if err := renameProject(p.ID, "Notes"); err != nil {
		t.Fatal(err)
	}
	got, ok := getProject(p.ID)
	if !ok || got.Name != "Notes" {
		t.Fatalf("rename = %+v", got)
	}

	if err := deleteProject(p.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := getProject(p.ID); ok {
		t.Fatal("projet encore là après delete")
	}
	list = listArchives()
	if len(list) != 1 || list[0].ProjectID != "" {
		t.Fatalf("chats devraient être détachés, got %+v", list)
	}
}
