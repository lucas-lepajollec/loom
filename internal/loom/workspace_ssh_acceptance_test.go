//go:build !windows

package loom

import (
	"context"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/resources"
)

// Explicit acceptance only. The disposable image has an OpenSSH server and a
// synthetic fixture user whose bash profile prints LOOM_SINGLE_STARTUP once.
// Ordinary tests never create containers or contact an SSH machine.
func TestRemoteWorkspaceSSHAcceptance(t *testing.T) {
	if os.Getenv("LOOM_TEST_SSH_ACCEPTANCE") != "1" {
		t.Skip("disposable SSH fixture")
	}
	testHome(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	name := "loom-workspace-ssh-qa-" + randomID(4)
	run := exec.Command("docker", "run", "-d", "--rm", "--name", name, "-p", "127.0.0.1::22", "loom-qa-ssh:local")
	if err := run.Run(); err != nil {
		t.Fatal(err)
	}
	defer exec.Command("docker", "rm", "-f", name).Run()
	published, err := exec.Command("docker", "port", name, "22/tcp").Output()
	if err != nil {
		t.Fatal(err)
	}
	_, portText, err := net.SplitHostPort(strings.TrimSpace(string(published)))
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	sshPath, err := exec.LookPath("ssh")
	if err != nil {
		t.Fatal(err)
	}
	// OpenSSH's default known_hosts follows passwd, not HOME. Keep this fixture
	// host key in its own file instead of modifying the real user's SSH state.
	bin := t.TempDir()
	shim := filepath.Join(bin, "ssh")
	script := "#!/bin/sh\nexec " + shellQuote(sshPath) + " -o UserKnownHostsFile=" + shellQuote(filepath.Join(home, "known_hosts")) + " \"$@\"\n"
	if err := os.WriteFile(shim, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	_, pub, err := loomSSHKey()
	if err != nil {
		t.Fatal(err)
	}
	setup := exec.Command("docker", "exec", "-i", name, "sh", "-c", "mkdir -p /home/fixture/.ssh; cat > /home/fixture/.ssh/authorized_keys; chmod 700 /home/fixture/.ssh; chmod 600 /home/fixture/.ssh/authorized_keys; chown -R fixture:fixture /home/fixture/.ssh")
	setup.Stdin = strings.NewReader(pub + "\n")
	if err := setup.Run(); err != nil {
		t.Fatal(err)
	}
	machine := RemoteMachine{ID: "fixture-box", Name: "Synthetic SSH", Host: "127.0.0.1", Port: port, User: "fixture", Home: "/home/fixture", OS: "linux"}
	if err := saveRemoteMachine(machine, nil); err != nil {
		t.Fatal(err)
	}
	folder, err := saveWorkspace(context.Background(), resources.Workspace{Name: "SSH project", Target: machine.ID, Path: "/home/fixture/app/new", Default: true}, true)
	if err != nil {
		t.Fatal(err)
	}
	if folder.Path != "/home/fixture/app/new" {
		t.Fatal("remote path changed")
	}
	terminal, err := openTerminal(machine.ID, folder.Path, "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = terminal.proc.Close()
		<-terminal.done
		terminals.Lock()
		delete(terminals.byID, terminal.ID)
		terminals.Unlock()
	}()
	if _, err := terminal.proc.Write([]byte("pwd\r")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		terminal.mu.Lock()
		output := string(terminal.scroll)
		terminal.mu.Unlock()
		if strings.Contains(output, "fixture-box") {
			t.Fatal("unexpected remote shell")
		}
		if strings.Contains(output, folder.Path) && strings.Contains(output, "LOOM_SINGLE_STARTUP") && strings.Contains(output, "fixture@") {
			// Echoed cd input alone is insufficient: require the pwd result too.
			if strings.Count(output, folder.Path) >= 2 {
				if strings.Count(output, "LOOM_SINGLE_STARTUP") != 1 {
					t.Fatal("nested login startup")
				}
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("remote shell did not initialize its workspace")
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Exercise the actual SSH -W stream used by remote application previews.
	app := exec.Command("docker", "exec", name, "sh", "-c", "python3 -c \"from pathlib import Path; Path('/home/fixture/app/index.html').write_text('synthetic-remote-asset\\n'*65536)\"; exec python3 -m http.server 5173 --bind 127.0.0.1 --directory /home/fixture/app")
	if err := app.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = app.Process.Kill(); _ = app.Wait() }()
	key, _, _ := loomSSHKey()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var conn net.Conn
	for time.Now().Before(deadline.Add(5 * time.Second)) {
		conn, err = previewSSHDial(ctx, ctx, machine, key, 5173)
		if err == nil {
			_, err = io.WriteString(conn, "GET / HTTP/1.0\r\nHost: localhost\r\n\r\n")
			if err == nil {
				data, readErr := io.ReadAll(conn)
				_ = conn.Close()
				if readErr == nil && strings.HasSuffix(string(data), strings.Repeat("synthetic-remote-asset\n", 65536)) {
					return
				}
			}
			_ = conn.Close()
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("SSH forwarding did not return the application asset")
}
