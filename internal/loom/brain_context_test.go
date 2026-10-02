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
	if _, err := saveProjectContext(ChatProject{Name: "X", BrainSources: []string{"inconnue"}, BrainBudget: 500}); err == nil {
		t.Fatal("source inconnue acceptée")
	}
	p, err := saveProjectContext(ChatProject{Name: "Infra", BrainSources: []string{"notes"}, BrainBudget: 800})
	if err != nil {
		t.Fatal(err)
	}
	s := RuntimeSession{ID: "s", ProjectID: p.ID, Messages: []Message{{Role: "user", Content: "Comment s'appelle la VM de dev sur Proxmox ?"}}}
	c := discussionContext(s)
	if !strings.Contains(c.System, "forge-dev") || len(c.BrainCitations) == 0 || !strings.Contains(c.BrainCitations[0], "infra.md") {
		t.Fatalf("contexte: %q %v", c.System, c.BrainCitations)
	}
	off, _ := saveProjectContext(ChatProject{ID: p.ID, Name: "Infra", BrainSources: []string{"notes"}, BrainBudget: 0})
	if c := discussionContext(RuntimeSession{ID: "s", ProjectID: off.ID, Messages: s.Messages}); strings.Contains(c.System, "forge-dev") {
		t.Fatal("Brain utilisé avec un budget nul")
	}
}
