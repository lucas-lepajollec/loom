//go:build linux

package loom

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestACPDeleteKillsOwnedDescendants(t *testing.T) {
	testHome(t)
	a := fakeACPAdapter(t)
	isolateRuntimeRegistry(t, a)
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	t.Setenv("LOOM_TEST_ACP_CHILD_PID_FILE", pidFile)
	m := newRuntimeSessions()
	s := createACPSession(t, m, a, "edits")
	if err := m.start(s.ID, "request-process", "fixture"); err != nil {
		t.Fatal(err)
	}
	waitACPTurn(t, m, s.ID)
	b, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(b))
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Kill(pid, syscall.SIGKILL)
	if err = m.remove(s.ID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
			return
		}
		// An orphan zombie has terminated and may wait briefly for init to reap it.
		if stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat"); err == nil && strings.Contains(string(stat), ") Z ") {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("owned ACP descendant survived session delete")
}

func TestACPFIFOAccessDoesNotBlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	p := &acpBinding{roots: []*os.Root{root}}
	done := make(chan error, 1)
	go func() { _, err := p.readFile(path, nil, nil); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("FIFO accepted")
		}
	case <-time.After(time.Second):
		t.Fatal("FIFO read blocked")
	}
	if _, err := p.writeFile(path, "content"); err == nil {
		t.Fatal("FIFO write accepted")
	}
}
