package loom

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lucas-lepajollec/loom/internal/loom/brain"
)

func TestProjectBrainContextReachesTheDiscussion(t *testing.T) {
	testHome(t)
	brainSvcMu.Lock()
	brainSvc = nil
	brainSvcMu.Unlock()
	notes := t.TempDir()
	_ = os.WriteFile(filepath.Join(notes, "infra.md"), []byte("# Réseau\n\n## Proxmox\n\nLa VM de dev s'appelle forge-dev et tourne sur le nœud pve1.\n"), 0o644)
	_ = os.WriteFile(filepath.Join(notes, "perso.md"), []byte("# Secret\n\nMon code de cadenas est 4242.\n"), 0o644)
	e, err := theBrain().get()
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Update(brain.Source{ID: "notes", Label: "Notes", Path: notes, Kind: "context"}); err != nil {
		t.Fatal(err)
	}
	if err := e.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := saveProjectContext(ChatProject{Name: "X", BrainSources: []string{"unknown"}, BrainBudget: 500}); err == nil {
		t.Fatal("source inconnue acceptée")
	}
	p, err := saveProjectContext(ChatProject{Name: "Infra", BrainSources: []string{"notes"}, BrainBudget: 800})
	if err != nil {
		t.Fatal(err)
	}
	s := RuntimeSession{ID: "s", RuntimeID: "llama.cpp", ProjectID: p.ID, Messages: []Message{{Role: "user", Content: "Comment s'appelle la VM de dev sur Proxmox ?"}}}
	c := discussionContextFor(s, lastUserText(s.Messages))
	if !strings.Contains(c.Extras, "forge-dev") || len(c.BrainCitations) == 0 || !strings.Contains(c.BrainCitations[0], "infra.md") {
		t.Fatalf("contexte: %q %v", c.System, c.BrainCitations)
	}
	inherited, _ := saveProjectContext(ChatProject{ID: p.ID, Name: "Infra", BrainBudget: 0})
	if c := discussionContextFor(RuntimeSession{ID: "s", RuntimeID: "llama.cpp", ProjectID: inherited.ID, Messages: s.Messages}, lastUserText(s.Messages)); !strings.Contains(c.Extras, "forge-dev") {
		t.Fatal("a project no longer inherited its connected second brains")
	}
}

// A secondary brain is announced to the model but never injected on its own.
func TestSecondaryBrainIsConsultedOnlyOnDemand(t *testing.T) {
	testHome(t)
	brainSvcMu.Lock()
	brainSvc = nil
	brainSvcMu.Unlock()
	archive := t.TempDir()
	_ = os.WriteFile(filepath.Join(archive, "infra.md"), []byte("# Réseau\n\nLa VM de dev s'appelle forge-dev.\n"), 0o644)
	e, err := theBrain().get()
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Update(brain.Source{ID: "archive", Label: "Archives", Path: archive, Kind: "context", Secondary: true}); err != nil {
		t.Fatal(err)
	}
	if err := e.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	p, err := saveProjectContext(ChatProject{Name: "Infra", BrainBudget: 800})
	if err != nil {
		t.Fatal(err)
	}
	c := discussionContext(RuntimeSession{ID: "s", RuntimeID: "llama.cpp", ProjectID: p.ID, Messages: []Message{{Role: "user", Content: "Comment s'appelle la VM de dev ?"}}})
	if strings.Contains(c.Extras, "forge-dev") || len(c.BrainCitations) > 0 {
		t.Fatalf("a secondary brain was injected: %q", c.System)
	}
	if !strings.Contains(c.System, "Archives (source id archive, read-only)") {
		t.Fatalf("the model is not told about the secondary brain: %q", c.System)
	}
	if !hasName(effectiveProjectBrainSources(p), "archive") {
		t.Fatal("a secondary brain must stay searchable on demand")
	}
}
