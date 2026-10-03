//go:build !windows

package loom

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestTerminalConcurrentRetriesStartOnlyOneProcess(t *testing.T) {
	testHome(t)
	t.Setenv("SHELL", "/bin/sh")
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	marker := filepath.Join(dir, "started.txt")
	id := "qa-" + randomID(8)
	command := "printf started >> " + shellQuote(marker) + "; exec cat"
	body, _ := json.Marshal(map[string]string{"target": "local", "dir": dir, "command": command, "request_id": id})
	call := func(body []byte) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/terminals", strings.NewReader(string(body)))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handleTerminals(w, r)
		return w
	}
	var wg sync.WaitGroup
	results := make(chan *httptest.ResponseRecorder, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- call(body) }()
	}
	wg.Wait()
	close(results)
	terminalID := ""
	for result := range results {
		var data struct {
			Terminal TerminalInfo `json:"terminal"`
		}
		if result.Code != 200 || json.Unmarshal(result.Body.Bytes(), &data) != nil {
			t.Fatal("terminal creation failed")
		}
		if terminalID != "" && data.Terminal.ID != terminalID {
			t.Fatal("one action started multiple terminals")
		}
		terminalID = data.Terminal.ID
	}
	term := terminalByID(terminalID)
	t.Cleanup(func() {
		_ = term.proc.Close()
		<-term.done
		terminals.Lock()
		delete(terminals.byID, terminalID)
		terminals.Unlock()
		terminalRequests.Lock()
		delete(terminalRequests.items, id)
		terminalRequests.Unlock()
	})
	deadline := time.Now().Add(3 * time.Second)
	for {
		data, _ := os.ReadFile(marker)
		if len(data) > 0 {
			if string(data) != "started" {
				t.Fatal("command ran more than once")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("command never ran")
		}
		time.Sleep(10 * time.Millisecond)
	}
	changed, _ := json.Marshal(map[string]string{"target": "local", "dir": dir, "command": "echo different", "request_id": id})
	if call(changed).Code != 409 {
		t.Fatal("request ID accepted a different command")
	}
	closeBody, _ := json.Marshal(map[string]string{"id": terminalID})
	r := httptest.NewRequest("POST", "/api/terminals/close", strings.NewReader(string(closeBody)))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handleTerminalClose(w, r)
	if w.Code != 200 || call(body).Code != 409 {
		t.Fatal("closed command was restarted by retry")
	}
}

func TestInteractiveRemoteWorkingDirectoryRejectsTerminalControls(t *testing.T) {
	if _, err := remoteTerminalInitialInput("fixture", "/app\x1b\r", ""); err == nil {
		t.Fatal("terminal editing characters accepted")
	}
}

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
	testHome(t)
	grant, err := controlOwner(httptest.NewRequest("GET", "/", nil))
	if err != nil {
		t.Fatal(err)
	}
	terminals.Lock()
	terminals.tickets[hashWebKey("abc")] = ticket{term: "t1", exp: time.Now().Add(time.Minute), grant: grant}
	terminals.tickets[hashWebKey("old")] = ticket{term: "t1", exp: time.Now().Add(-time.Second), grant: grant}
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
		!strings.Contains(joined, `root@10.0.0.2 `) || !strings.HasSuffix(joined, `cd '/srv/app it'"'"'s' && hermes`) {
		t.Fatal(joined)
	}
	if _, _, err := terminalCommand("box", "relatif", ""); err == nil {
		t.Fatal("dossier distant relatif accepté")
	}
	if _, _, err := terminalCommand("unknown", "", ""); err == nil {
		t.Fatal("machine inconnue acceptée")
	}
}
