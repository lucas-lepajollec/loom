//go:build !windows

package loom

import (
	"strings"
	"testing"
	"time"
)

func TestTerminalRunsAndKeepsOutput(t *testing.T) {
	testHome(t)
	dir := t.TempDir()
	term, err := openTerminal("local", dir, "printf 'bonjour-terminal'; pwd", "")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-term.done:
	case <-time.After(10 * time.Second):
		t.Fatal("la commande ne s'est pas terminée")
	}
	_, scroll := term.attach()
	if !strings.Contains(string(scroll), "bonjour-terminal") || !strings.Contains(string(scroll), dir) {
		t.Fatalf("sortie: %q", scroll)
	}
	if s := term.snapshot(); s.Running || s.ExitCode != 0 || s.Title != "printf" {
		t.Fatalf("%+v", s)
	}
}

func TestTerminalInteractiveInputAndClose(t *testing.T) {
	testHome(t)
	term, err := openTerminal("local", t.TempDir(), "cat", "Echo")
	if err != nil {
		t.Fatal(err)
	}
	out, _ := term.attach()
	if _, err := term.proc.Write([]byte("salut\n")); err != nil {
		t.Fatal(err)
	}
	got := ""
	deadline := time.After(5 * time.Second)
	for !strings.Contains(got, "salut") {
		select {
		case b := <-out:
			got += string(b)
		case <-deadline:
			t.Fatalf("écho absent: %q", got)
		}
	}
	if err := term.proc.Resize(120, 40); err != nil {
		t.Fatal(err)
	}
	_ = term.proc.Close()
	select {
	case <-term.done:
	case <-time.After(5 * time.Second):
		t.Fatal("fermeture sans effet")
	}
}

func TestTerminalTicketsAreSingleUse(t *testing.T) {
	terminals.Lock()
	terminals.tickets["abc"] = ticket{term: "t1", exp: time.Now().Add(time.Minute)}
	terminals.tickets["old"] = ticket{term: "t1", exp: time.Now().Add(-time.Second)}
	terminals.Unlock()
	if useTicket("abc") != "t1" || useTicket("abc") != "" || useTicket("old") != "" || useTicket("") != "" {
		t.Fatal("ticket réutilisable ou expiré accepté")
	}
}

func TestRemoteTerminalCommand(t *testing.T) {
	testHome(t)
	t.Setenv("HOME", t.TempDir())
	m := RemoteMachine{ID: "box", Name: "box", Host: "10.0.0.2", User: "root", Port: 22, Home: "/root"}
	if err := saveRemoteMachine(m, nil); err != nil {
		t.Fatal(err)
	}
	argv, _, err := terminalCommand("box", "/srv/app it's", "hermes")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(argv, " ")
	if argv[0] != "ssh" || !strings.Contains(joined, " -tt ") || strings.Contains(joined, " -T ") ||
		!strings.HasSuffix(joined, `root@10.0.0.2 cd '/srv/app it'"'"'s' && exec "${SHELL:-/bin/sh}" -lc hermes`) {
		t.Fatal(joined)
	}
	if _, _, err := terminalCommand("box", "relatif", ""); err == nil {
		t.Fatal("dossier distant relatif accepté")
	}
	if _, _, err := terminalCommand("unknown", "", ""); err == nil {
		t.Fatal("machine inconnue acceptée")
	}
}
