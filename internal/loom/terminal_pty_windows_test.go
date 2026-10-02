//go:build windows

package loom

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func requireConPTY(t *testing.T) {
	t.Helper()
	if !ptySupported {
		t.Skip("ConPTY requires Windows 10 1809+")
	}
}

func closeTerminalWithin(t *testing.T, p termProcess) {
	t.Helper()
	done := make(chan struct{})
	go func() { _ = p.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("ConPTY close blocked")
	}
}

func TestTerminalConPTYOutputEnvironmentAndExit(t *testing.T) {
	requireConPTY(t)
	dir := t.TempDir()
	// Exercise cmd's nested quotes as well as cwd, UTF-16 environment and the
	// final frame (> pipe capacity). The output is consumed while Wait closes.
	command := `for /L %i in (1,1,1000) do @echo sortie-terminal`
	command += ` & echo %LOOM_TERM_TEST% & cd & echo "fin terminal" & exit /b 7`
	p, err := startPTY(windowsTerminalShellCommand(os.Getenv("ComSpec"), command), dir, []string{"LOOM_TERM_TEST=bonjour-été"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeTerminalWithin(t, p) })
	output := make(chan string, 1)
	go func() { b, _ := io.ReadAll(p); output <- string(b) }()
	var got string
	select {
	case got = <-output:
	case <-time.After(15 * time.Second):
		t.Fatal("ConPTY output did not finish")
	}
	code, err := p.Wait()
	if code != 7 || err == nil {
		t.Fatalf("exit = %d, %v", code, err)
	}
	for _, want := range []string{"sortie-terminal", "bonjour-été", dir, "fin terminal"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in output %q", want, got)
		}
	}
}

func TestTerminalConPTYInputResizeAndClose(t *testing.T) {
	requireConPTY(t)
	testHome(t)
	term, err := openTerminal("local", t.TempDir(), "$x = Read-Host; Write-Output ('reply:' + $x); Start-Sleep -Seconds 300", "Echo")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeTerminalWithin(t, term.proc) })
	for _, size := range [][2]uint16{{0, 1}, {1, 0}, {1001, 1}, {1, 501}} {
		if term.proc.Resize(size[0], size[1]) == nil {
			t.Fatalf("accepted invalid size %v", size)
		}
	}
	if err := term.proc.Resize(120, 40); err != nil {
		t.Fatal(err)
	}
	out, _ := term.attach()
	if _, err := term.proc.Write([]byte("salut\r\n")); err != nil {
		t.Fatal(err)
	}
	got := ""
	timeout := time.After(15 * time.Second)
	for !strings.Contains(got, "reply:salut") {
		select {
		case b, ok := <-out:
			if !ok {
				t.Fatalf("shell exited: %q", got)
			}
			got += string(b)
		case <-timeout:
			t.Fatalf("input not received: %q", got)
		}
	}
	closeTerminalWithin(t, term.proc)
	select {
	case <-term.done:
	case <-time.After(10 * time.Second):
		t.Fatal("terminal pump did not finish")
	}
	if term.proc.Resize(80, 25) == nil {
		t.Fatal("resize after close succeeded")
	}
}

func TestTerminalConPTYCloseWithoutReader(t *testing.T) {
	requireConPTY(t)
	p, err := startPTY(windowsTerminalShellCommand(os.Getenv("ComSpec"), `for /L %i in (1,1,100000) do @echo blocked-output`), t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeTerminalWithin(t, p) })
	// Concurrent/repeated close must remain safe even with a full output pipe.
	var wg sync.WaitGroup
	wg.Add(2)
	done := make(chan struct{})
	for range 2 {
		go func() { defer wg.Done(); _ = p.Close() }()
	}
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("close without reader blocked")
	}
	p.Wait()
}

func TestTerminalConPTYCloseKillsDescendants(t *testing.T) {
	requireConPTY(t)
	shell, err := exec.LookPath("powershell.exe")
	if err != nil {
		t.Fatal(err)
	}
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	script := "$p = Start-Process -PassThru -FilePath " + powershellLiteral(shell) + " -ArgumentList '-NoProfile','-Command','Start-Sleep -Seconds 300'; [IO.File]::WriteAllText(" + powershellLiteral(pidFile) + ", [string]$p.Id); Start-Sleep -Seconds 300"
	p, err := startPTY([]string{shell, "-NoProfile", "-Command", script}, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeTerminalWithin(t, p) })
	go io.Copy(io.Discard, p)
	deadline := time.Now().Add(15 * time.Second)
	var pid int
	for time.Now().Before(deadline) {
		b, _ := os.ReadFile(pidFile)
		pid, _ = strconv.Atoi(string(b))
		if pid != 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if pid == 0 {
		t.Fatal("child PID was not written")
	}
	child, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(child)
	t.Cleanup(func() { _ = windows.TerminateProcess(child, 1) })
	closeTerminalWithin(t, p)
	result, err := windows.WaitForSingleObject(child, 10000)
	if err != nil || result != windows.WAIT_OBJECT_0 {
		t.Fatalf("child survived close: %d, %v", result, err)
	}
	p.Wait()
}

func TestTerminalConPTYNativeQuoting(t *testing.T) {
	argv := []string{`C:\Program Files\PowerShell\7\pwsh.exe`, "-Command", `Write-Output 'a "b"'`, "", `C:\path with spaces\`, "été"}
	line := windowsTerminalCommandLine(argv, windows.ComposeCommandLine)
	got, err := windows.DecomposeCommandLine(line)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(argv) {
		t.Fatalf("%#v", got)
	}
	for i := range argv {
		if got[i] != argv[i] {
			t.Fatalf("arg %d: %q, want %q", i, got[i], argv[i])
		}
	}
}
