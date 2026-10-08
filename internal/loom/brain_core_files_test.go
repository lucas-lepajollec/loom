package loom

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/brain"
)

func TestCoreFilesMirrorBothWays(t *testing.T) {
	testHome(t)
	t.Cleanup(coreFilesJobs.Wait)
	s := theBrain()
	vault := primaryMemoryVault(t, s)
	profile, err := s.Remember(brain.RememberRequest{Class: "semantic", Scope: "global", Tags: []string{brain.ProfileTag}, Text: "Lucas, answers in French.", Provenance: brain.MemoryProvenance{Kind: "user"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.syncCoreFiles(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(vault, "Loom", "Toi.md")
	if _, err := os.Stat(path); err == nil {
		t.Fatal("written while disabled")
	}
	if err := s.storage.write("core_files.json", []byte(`{"enabled":true,"folder":"Loom"}`)); err != nil {
		t.Fatal(err)
	}
	if err := s.syncCoreFiles(); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); strings.TrimSpace(string(b)) != profile.Text {
		t.Fatalf("mirror %q", b)
	}
	// Edited in Obsidian: the file wins.
	if err := os.WriteFile(path, []byte("Lucas, prefers short answers.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.syncCoreFiles(); err != nil {
		t.Fatal(err)
	}
	list, _ := s.ListMemory(brain.MemoryFilter{Classes: []string{"semantic"}})
	if len(list.Items) != 1 || list.Items[0].Text != "Lucas, prefers short answers." {
		t.Fatalf("%+v", list.Items)
	}
	// Changed by an agent: the file follows.
	text := "Lucas, prefers short answers and sober UIs."
	if _, err := s.UpdateMemory(brain.UpdateMemoryRequest{ID: list.Items[0].ID, Patch: brain.MemoryPatch{Text: &text}}); err != nil {
		t.Fatal(err)
	}
	if err := s.syncCoreFiles(); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); strings.TrimSpace(string(b)) != text {
		t.Fatalf("file did not follow %q", b)
	}
	for _, bad := range []string{"/abs", "../out", ".loom/x", "."} {
		if validCoreFolder(bad) {
			t.Fatal("accepted", bad)
		}
	}
}

// Regression: building the Brain engine asks for index exclusions while
// holding the service lock; enabled core files must not re-enter it.
func TestCoreFilesExclusionsDoNotDeadlockEngineBuild(t *testing.T) {
	testHome(t)
	t.Cleanup(coreFilesJobs.Wait)
	s := theBrain()
	primaryMemoryVault(t, s)
	if err := s.storage.write("core_files.json", []byte(`{"enabled":true,"folder":"Loom"}`)); err != nil {
		t.Fatal(err)
	}
	fresh := newBrainService(LoomHome())
	done := make(chan error, 1)
	go func() { _, err := fresh.get(); done <- err }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("brain engine build deadlocked on core-file exclusions")
	}
}
