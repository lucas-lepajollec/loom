package loom

import (
	"errors"
	"sort"
	"strings"
	"unicode/utf16"
)

// Kept platform independent so Windows shell selection and argv can be tested
// without starting Windows processes or connecting to a machine.
func windowsTerminalShell(lookPath func(string) (string, error), comSpec string) string {
	for _, name := range []string{"pwsh.exe", "powershell.exe"} {
		if p, err := lookPath(name); err == nil {
			return p
		}
	}
	if comSpec != "" {
		return comSpec
	}
	return "cmd.exe"
}

func windowsTerminalShellCommand(shell, command string) []string {
	if windowsTerminalIsCMD(shell) {
		if command == "" {
			return []string{shell}
		}
		return []string{shell, "/D", "/S", "/C", command}
	}
	args := []string{shell, "-NoLogo"}
	if command != "" {
		args = append(args, "-Command", command)
	}
	return args
}

func windowsTerminalIsCMD(shell string) bool {
	parts := strings.Split(strings.ReplaceAll(shell, `\`, "/"), "/")
	return strings.EqualFold(parts[len(parts)-1], "cmd.exe") || strings.EqualFold(parts[len(parts)-1], "cmd")
}

// cmd /S /C consumes shell syntax, rather than CommandLineToArgvW escaping.
// The executable/options still use the native Windows composer supplied here.
func windowsTerminalCommandLine(argv []string, compose func([]string) string) string {
	if len(argv) == 5 && windowsTerminalIsCMD(argv[0]) && argv[3] == "/C" {
		return compose(argv[:4]) + ` "` + argv[4] + `"`
	}
	return compose(argv)
}

func windowsRemoteTerminalScript(dir, command string) string {
	script := "$ErrorActionPreference = 'Stop'\n"
	if dir != "" {
		script += "try { Set-Location -LiteralPath " + powershellLiteral(dir) + " } catch { [Console]::Error.WriteLine($_.Exception.Message); exit 1 }\n"
	}
	if command != "" {
		// The requested command is shell code, just as with Unix -lc. A script
		// block prevents a multiline command from escaping the exit handling.
		script += "& ([scriptblock]::Create(" + powershellLiteral(command) + "))\n" +
			"$loom_ok = $?\nif ($null -ne $LASTEXITCODE -and $LASTEXITCODE -ne 0) { exit $LASTEXITCODE }; if (!$loom_ok) { exit 1 }; exit 0\n"
	}
	return script
}

func windowsTerminalEnvironment(env []string) ([]uint16, error) {
	values := make(map[string]string, len(env))
	for _, entry := range env {
		// Windows may include drive-current-directory entries such as =C:=C:\.
		if strings.ContainsRune(entry, 0) || len(entry) < 2 {
			return nil, errors.New("environnement invalide")
		}
		i := strings.IndexByte(entry[1:], '=') + 1
		if i == 0 {
			return nil, errors.New("environnement invalide")
		}
		values[strings.ToUpper(entry[:i])] = entry
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var block []uint16
	for _, key := range keys {
		block = append(block, utf16.Encode([]rune(values[key]))...)
		block = append(block, 0)
	}
	if len(block) == 0 {
		block = append(block, 0)
	}
	return append(block, 0), nil
}
