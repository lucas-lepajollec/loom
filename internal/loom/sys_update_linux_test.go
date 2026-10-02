//go:build linux

package loom

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSystemUpdaterRejectsCallerPathsAndArguments(t *testing.T) {
	if err := cmdSystemUpdate([]string{"/tmp/arbitrary"}); err == nil {
		t.Fatal("helper accepted caller arguments")
	}
	file := filepath.Join(t.TempDir(), "caller-binary")
	if err := os.WriteFile(file, []byte("untrusted"), 0o777); err != nil {
		t.Fatal(err)
	}
	if rootTrustedPath(file) == nil {
		t.Fatal("trusted user-writable executable")
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(file, link); err != nil {
		t.Fatal(err)
	}
	if rootTrustedPath(link) == nil || canUseSystemUpdater(link) {
		t.Fatal("symlink can gain root execution")
	}
	if err := installSystemUpdater("user ALL=(ALL) NOPASSWD: ALL"); err == nil {
		t.Fatal("sudoers user injection accepted")
	}
}
