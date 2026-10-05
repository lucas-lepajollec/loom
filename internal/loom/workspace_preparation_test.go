package loom

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestRemoteExecutorPreparationDoesNotBlockOtherDiscussionReads(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX SSH fixture")
	}
	home := testHome(t)
	m := newRuntimeSessions()
	machine := RemoteMachine{ID: "fixture-host", Name: "Fixture", Host: "fixture.invalid", User: "fixture", Home: "/srv/fixture", Port: 22}
	if err := putStoreJSON(bkState, remoteMachinesState, []RemoteMachine{machine}); err != nil {
		t.Fatal(err)
	}
	_ = os.MkdirAll(filepath.Join(home, "ssh"), 0700)
	for _, name := range []string{"loom_ed25519", "loom_ed25519.pub"} {
		_ = os.WriteFile(filepath.Join(home, "ssh", name), []byte("fixture"), 0600)
	}
	dir := t.TempDir()
	entered := filepath.Join(dir, "entered")
	release := filepath.Join(dir, "release")
	script := "#!/bin/sh\n: > " + shellQuote(entered) + "\nwhile [ ! -e " + shellQuote(release) + " ]; do /bin/sleep 0.01; done\n"
	if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	agent := acpAgent{ID: "fixture-remote", Name: "Fixture remote", Remote: true, Custom: true, Machine: machine.ID, Command: "ssh"}
	registeredRuntimes.upsert(&acpAdapter{agent: agent})
	t.Cleanup(func() { registeredRuntimes.remove(agent.ID) })
	s, err := m.create("", "", false)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := m.selectModelContext(ctx, s.ID, agent.ID+":default", true); done <- err }()
	deadline := time.Now().Add(time.Second)
	for {
		if _, err := os.Stat(entered); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("workspace preparation did not start")
		}
		time.Sleep(5 * time.Millisecond)
	}
	read := make(chan bool, 1)
	go func() { _, ok := m.get(s.ID); read <- ok }()
	select {
	case ok := <-read:
		if !ok {
			t.Fatal("discussion missing")
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("remote SSH preparation held the global discussion lock")
	}
	// A concurrent stored edit must never be overwritten by the old snapshot.
	m.mu.Lock()
	s.Title = "Concurrent edit"
	err = putStoreJSON(bkRuntimeSessions, s.ID, s)
	m.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(release, nil, 0600)
	if err := <-done; err == nil {
		t.Fatal("executor selection overwrote concurrent change")
	}
	final, _ := m.get(s.ID)
	if final.Title != "Concurrent edit" {
		t.Fatal("concurrent edit lost")
	}
}
