package tools

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var lockedTestMemory = errors.New("locked")

type testMemoryStore struct {
	dir    string
	locked bool
}

func (s *testMemoryStore) Directory() string { return s.dir }
func (s *testMemoryStore) ReadPage(name string) ([]byte, error) {
	if s.locked {
		return nil, lockedTestMemory
	}
	fn, err := MemFileName(name)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(filepath.Join(s.dir, fn))
}
func (s *testMemoryStore) PageText(name string) (string, bool) {
	data, err := s.ReadPage(name)
	return string(data), err == nil
}
func (s *testMemoryStore) WriteFile(name string, data []byte) error {
	fn, err := MemFileName(name)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.dir, fn), data, 0600)
}
func (*testMemoryStore) LockedError() error { return lockedTestMemory }
func testMemory(t *testing.T) Memory        { return Memory{Store: &testMemoryStore{dir: t.TempDir()}} }

// Reproduit le bug rapporté : une petite page dont le NOM et le TITRE contiennent
// « copine » ne remontait pas pour la requête « copine Nathan », noyée par de
// grosses pages qui répètent « Nathan » des dizaines de fois. Le classement doit
// mettre en tête la page qui matche le PLUS de termes distincts (couverture),
// puis pondérer par la rareté.
func TestMemSearchCoverageBeatsFrequency(t *testing.T) {
	m := testMemory(t)

	// Deux pages « bruyantes » bourrées de « Nathan », sans « copine ».
	if err := m.MemAdd("business-nathan.md", "# Business Nathan\n"+strings.Repeat("Nathan veut un business. ", 40)); err != nil {
		t.Fatal(err)
	}
	if err := m.MemAdd("guide-loom.md", "# Guide Loom\n"+strings.Repeat("projet de Nathan. ", 30)); err != nil {
		t.Fatal(err)
	}
	// La petite page cible : matche les DEUX termes de la requête.
	if err := m.MemAdd("profil-copine-nathan.md", "# Profil : Copine de Nathan\n- Prénom : Bao\n"); err != nil {
		t.Fatal(err)
	}

	hits := m.MemSearch("copine Nathan", 8)
	if len(hits) == 0 {
		t.Fatal("aucun résultat pour « copine Nathan »")
	}
	if hits[0].File != "profil-copine-nathan.md" {
		var names []string
		for _, h := range hits {
			names = append(names, h.File)
		}
		t.Fatalf("la page copine devrait être 1re (matche les 2 termes), obtenu ordre : %v", names)
	}
}

// Une requête mono-terme rare doit privilégier un match dans le nom/titre.
func TestMemSearchNameTitleBoost(t *testing.T) {
	m := testMemory(t)
	_ = m.MemAdd("notes-diverses.md", "# Notes\nUn jour Bao est passée dire bonjour, puis repartie.\n")
	_ = m.MemAdd("profil-copine.md", "# Profil : Copine\n- Prénom : Bao\n")
	hits := m.MemSearch("copine", 8)
	if len(hits) == 0 || hits[0].File != "profil-copine.md" {
		t.Fatalf("la page dont le nom/titre porte « copine » devrait être 1re, obtenu : %+v", hits)
	}
}

func TestMemoryStorageBoundaryAndExactEdit(t *testing.T) {
	m := testMemory(t)
	if err := m.MemAdd("../escape", "x"); err == nil {
		t.Fatal("invalid name accepted")
	}
	if err := m.MemAdd("notes", "# Notes\nfirst\nsecond"); err != nil {
		t.Fatal(err)
	}
	if got, err := m.MemRead("notes", 2, 1); err != nil || got != "2\tfirst" {
		t.Fatalf("read: %q %v", got, err)
	}
	if err := m.MemEdit("notes", "first", "changed"); err != nil {
		t.Fatal(err)
	}
	if err := m.MemEdit("notes", "first", "changed"); err != ErrAlreadyApplied {
		t.Fatalf("repeated edit: %v", err)
	}
	if err := m.MemAdd("notes", "replacement"); err == nil {
		t.Fatal("existing page replaced")
	}
	m.Store.(*testMemoryStore).locked = true
	if _, err := m.MemRead("notes", 1, 1); err == nil || !strings.Contains(err.Error(), "mémoire verrouillée") {
		t.Fatalf("locked: %v", err)
	}
	if pages := m.MemList(); len(pages) != 1 || pages[0].Title != "🔒 (chiffré — mémoire verrouillée)" {
		t.Fatalf("locked listing: %+v", pages)
	}
}
