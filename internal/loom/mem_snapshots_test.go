package loom

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRestoreSnapshotRejectsInvalidIDsWithoutChangingMemory(t *testing.T) {
	testHome(t)
	clearMemDEK()
	if err := MemAdd("keep.md", "Keep this page"); err != nil {
		t.Fatal(err)
	}
	want := MemContent("keep.md")

	for _, id := range []string{"", ".", "..", "../memory", "20260923-123456-../../outside", "not-a-snapshot"} {
		if err := restoreSnapshot(id); err == nil {
			t.Errorf("restoreSnapshot(%q) unexpectedly succeeded", id)
		}
		if got := MemContent("keep.md"); got != want {
			t.Fatalf("restoreSnapshot(%q) changed memory: %q", id, got)
		}
	}
	if got := listSnapshots(); len(got) != 0 {
		t.Fatalf("invalid restore IDs created safety snapshots: %+v", got)
	}
}

func TestRestoreSnapshotRejectsSymbolicLinkBeforeChangingMemory(t *testing.T) {
	testHome(t)
	clearMemDEK()
	if err := MemAdd("keep.md", "current"); err != nil {
		t.Fatal(err)
	}
	id, err := snapshotMemory("test")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(memoryDir(), "keep.md"), filepath.Join(snapshotsRoot(), id, "linked.md")); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}
	if err := restoreSnapshot(id); err == nil {
		t.Fatal("snapshot with a symbolic-link entry was accepted")
	}
	if got := MemContent("keep.md"); got != "current\n" {
		t.Fatalf("invalid snapshot changed memory: %q", got)
	}
}

func TestRestoreSnapshotRejectsEmptyDirectoryWithoutChangingMemory(t *testing.T) {
	testHome(t)
	clearMemDEK()
	if err := MemAdd("keep.md", "current"); err != nil {
		t.Fatal(err)
	}
	id := "20260923-123456-empty"
	if err := os.MkdirAll(filepath.Join(snapshotsRoot(), id), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := restoreSnapshot(id); err == nil {
		t.Fatal("empty snapshot directory was accepted")
	}
	if got := MemContent("keep.md"); got != "current\n" {
		t.Fatalf("empty snapshot changed memory: %q", got)
	}
}
