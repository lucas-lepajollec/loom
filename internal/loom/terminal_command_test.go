package loom

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
	"reflect"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestTerminalWindowsShellChoice(t *testing.T) {
	for _, tc := range []struct {
		available     []string
		comSpec, want string
	}{
		{[]string{"pwsh.exe", "powershell.exe"}, `C:\Windows\cmd.exe`, `C:\Tools\pwsh.exe`},
		{[]string{"powershell.exe"}, `C:\Windows\cmd.exe`, `C:\Tools\powershell.exe`},
		{nil, `C:\Windows\cmd.exe`, `C:\Windows\cmd.exe`},
		{nil, "", "cmd.exe"},
	} {
		look := func(name string) (string, error) {
			for _, found := range tc.available {
				if found == name {
					return `C:\Tools\` + name, nil
				}
			}
			return "", errors.New("absent")
		}
		if got := windowsTerminalShell(look, tc.comSpec); got != tc.want {
			t.Fatalf("shell = %q, want %q", got, tc.want)
		}
	}
}

func TestTerminalWindowsCommandComposition(t *testing.T) {
	command := `Write-Output 'bonjour "été"'; exit 7`
	for _, tc := range []struct {
		shell, command string
		want           []string
	}{
		{"pwsh.exe", "", []string{"pwsh.exe", "-NoLogo"}},
		{`C:\Program Files\PowerShell\7\pwsh.exe`, command, []string{`C:\Program Files\PowerShell\7\pwsh.exe`, "-NoLogo", "-Command", command}},
		{"powershell.exe", command, []string{"powershell.exe", "-NoLogo", "-Command", command}},
		{`C:\Windows\CMD.EXE`, "", []string{`C:\Windows\CMD.EXE`}},
		{"cmd.exe", `"C:\Program Files\app.exe" "a b"`, []string{"cmd.exe", "/D", "/S", "/C", `"C:\Program Files\app.exe" "a b"`}},
	} {
		if got := windowsTerminalShellCommand(tc.shell, tc.command); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("argv = %#v, want %#v", got, tc.want)
		}
	}
	// Verify the cmd-specific shell tail is raw shell syntax. The native
	// composer is injected so this logic is exercised on Linux as well.
	cmd := windowsTerminalShellCommand("cmd.exe", `"C:\Program Files\app.exe" "a b"`)
	var composed []string
	got := windowsTerminalCommandLine(cmd, func(argv []string) string {
		composed = append([]string(nil), argv...)
		return strings.Join(argv, " ")
	})
	if !reflect.DeepEqual(composed, cmd[:4]) || got != `cmd.exe /D /S /C ""C:\Program Files\app.exe" "a b""` {
		t.Fatalf("command line = %q (%#v)", got, composed)
	}
	ps := windowsTerminalShellCommand("powershell.exe", command)
	windowsTerminalCommandLine(ps, func(argv []string) string {
		if !reflect.DeepEqual(argv, ps) {
			t.Fatal("PowerShell argv altered")
		}
		return ""
	})
}

func TestTerminalWindowsEnvironment(t *testing.T) {
	block, err := windowsTerminalEnvironment([]string{"Path=old", "TERM=old", "path=new", "TERM=xterm-256color", "UNICODE=été 😀", `=C:=C:\work`})
	if err != nil {
		t.Fatal(err)
	}
	got := string(utf16.Decode(block))
	want := "=C:=C:\\work\x00path=new\x00TERM=xterm-256color\x00UNICODE=été 😀\x00\x00"
	if got != want {
		t.Fatalf("environment = %q, want %q", got, want)
	}
	empty, err := windowsTerminalEnvironment(nil)
	if err != nil || !reflect.DeepEqual(empty, []uint16{0, 0}) {
		t.Fatalf("empty environment: %v %v", empty, err)
	}
	for _, entry := range []string{"", "INVALID", "X=a\x00b"} {
		if _, err := windowsTerminalEnvironment([]string{entry}); err == nil {
			t.Fatalf("accepted invalid environment %q", entry)
		}
	}
}

func TestTerminalRemoteWindowsCommand(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := RemoteMachine{OS: "Windows_NT", Host: "win.example", User: "alice", Port: 2222}
	dir := `C:/work/été it's $literal [1]`
	for _, command := range []string{"", "Write-Output 'salut'; exit 7", "Write-Output 'a'\nWrite-Output 'b'"} {
		args := remoteTerminalArgs(m, "", dir, command)
		joined := strings.Join(args, " ")
		if !strings.Contains(joined, "-tt ") || strings.Contains(joined, "-T ") || strings.Contains(joined, "-NonInteractive") || !strings.Contains(joined, "-p 2222 alice@win.example powershell -NoProfile ") {
			t.Fatal(joined)
		}
		if strings.Contains(joined, "-NoExit") != (command == "") {
			t.Fatalf("interactive shell: %s", joined)
		}
		payload, err := base64.StdEncoding.DecodeString(args[len(args)-1])
		if err != nil {
			t.Fatal(err)
		}
		words := make([]uint16, len(payload)/2)
		for i := range words {
			words[i] = binary.LittleEndian.Uint16(payload[2*i:])
		}
		script := string(utf16.Decode(words))
		prefix := "$ErrorActionPreference = 'Stop'\ntry { Set-Location -LiteralPath 'C:/work/été it''s $literal [1]' } catch { [Console]::Error.WriteLine($_.Exception.Message); exit 1 }\n"
		if !strings.HasPrefix(script, prefix) || strings.Contains(script, "exec ") || strings.Contains(script, "cd ") {
			t.Fatal(script)
		}
		if command == "" {
			if script != prefix {
				t.Fatal(script)
			}
		} else if !strings.Contains(script, "[scriptblock]::Create("+powershellLiteral(command)+")") || !strings.Contains(script, "exit $LASTEXITCODE") {
			t.Fatal(script)
		}
	}
	// No folder means use the Windows OpenSSH user's default folder.
	if script := windowsRemoteTerminalScript("", ""); strings.Contains(script, "Set-Location") {
		t.Fatal(script)
	}
}
