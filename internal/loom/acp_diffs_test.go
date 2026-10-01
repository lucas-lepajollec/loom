package loom

import (
	"os"
	"path/filepath"
	"testing"
)

func TestACPToolDiffsBecomeChangedFilesInsideRootsOnly(t *testing.T) {
	dir := t.TempDir()
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	p := &acpBinding{roots: []*os.Root{root}}
	inside := filepath.Join(dir, "main.go")
	ev := p.noteToolDiffsLocked(map[string]any{"diffs": []any{
		map[string]any{"path": inside, "old": "a\nb\n", "new": "a\nc\n"},
		map[string]any{"path": "/etc/passwd", "old": "x", "new": "y"},
	}})
	if ev == nil || len(p.state.Files) != 1 || p.state.Files[0].Path != inside || p.state.Files[0].Add != 1 || p.state.Files[0].Del != 1 {
		t.Fatalf("files = %+v", p.state.Files)
	}
	if p.state.FileBaselines[inside] != "a\nb\n" {
		t.Fatal("baseline not kept")
	}
}
