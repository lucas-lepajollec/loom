//go:build !windows

package loom

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestWorkspaceDownloadRejectsEscapingLinksAndSpecialFiles(t *testing.T) {
	testHome(t)
	root := agentWorkspace()
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("outside fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(root, "inside.txt")
	if err := os.WriteFile(inside, []byte("inside fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "external.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("inside.txt", filepath.Join(root, "internal.txt")); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(root, "pipe.txt"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"external.txt", "pipe.txt"} {
		w := httptest.NewRecorder()
		handleChatFile(w, httptest.NewRequest("GET", "/api/chat/file?path="+p, nil))
		if w.Code == 200 {
			t.Fatal("non-workspace or non-regular file served", p)
		}
	}
	r := httptest.NewRequest("GET", "/api/chat/file?path=internal.txt", nil)
	r.Header.Set("Range", "bytes=0-5")
	w := httptest.NewRecorder()
	handleChatFile(w, r)
	if w.Code != 206 || w.Body.String() != "inside" {
		t.Fatal("confined symlink or range stopped working", w.Code)
	}
}
