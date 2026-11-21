package loom

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lucas-lepajollec/loom/internal/loom/brain"
)

func TestPrimarySecondBrainWriteIsConfined(t *testing.T) {
	testHome(t)
	brainSvcMu.Lock()
	brainSvc = nil
	brainSvcMu.Unlock()
	s := theBrain()
	defer s.writeRefresh.Wait()
	dir := t.TempDir()
	e, err := theBrain().get()
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Update(brain.Source{ID: "primary", Label: "Primary", Path: dir, Kind: "context", Connector: "folder", Permission: "write", Primary: true}); err != nil {
		t.Fatal(err)
	}
	if err = secondBrainWrite("projects/loom.md", "# Loom\n\nCurrent decision.\n"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "projects", "loom.md"))
	if err != nil || !strings.Contains(string(b), "Current decision") {
		t.Fatalf("write missing: %q %v", b, err)
	}
	if err = secondBrainEdit("projects/loom.md", "Current decision", "Updated decision"); err != nil {
		t.Fatal(err)
	}
	secondary := t.TempDir()
	if err = e.Update(brain.Source{ID: "shared", Label: "Shared", Path: secondary, Kind: "context", Connector: "folder", Permission: "write"}); err != nil {
		t.Fatal(err)
	}
	if err = secondBrainWriteSource("shared", "requested.md", "# Explicit update\n"); err != nil {
		t.Fatal(err)
	}
	readOnly := t.TempDir()
	if err = e.Update(brain.Source{ID: "reference", Label: "Reference", Path: readOnly, Kind: "context", Connector: "folder", Permission: "read"}); err != nil {
		t.Fatal(err)
	}
	if err = secondBrainWriteSource("reference", "blocked.md", "no"); err == nil {
		t.Fatal("read-only second brain accepted a write")
	}
	if err = secondBrainWrite("../escape.md", "no"); err == nil {
		t.Fatal("path traversal accepted")
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dir, "escape")); err == nil {
		if err = secondBrainWrite("escape/new/blocked.md", "no"); err == nil {
			t.Fatal("symlink escape accepted")
		}
		if _, err = os.Stat(filepath.Join(outside, "new")); !os.IsNotExist(err) {
			t.Fatal("write created a directory outside the second brain")
		}
		if err = os.WriteFile(filepath.Join(outside, "existing.md"), []byte("private"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err = secondBrainEdit("escape/existing.md", "private", "changed"); err == nil {
			t.Fatal("edit followed a symlink outside the second brain")
		}
	}
	names := map[string]bool{}
	for _, tool := range EnabledTools(Caps{}) {
		names[tool.Function.Name] = true
	}
	if !names["brain_write"] || !names["brain_edit"] {
		t.Fatal("writable primary brain tools missing")
	}
}
