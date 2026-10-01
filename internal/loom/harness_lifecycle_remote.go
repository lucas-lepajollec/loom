package loom

import (
	"encoding/base64"
	"encoding/binary"
	"strings"
	"unicode/utf16"
)

const remoteWindowsPathPreamble = `$env:PATH += ";$env:APPDATA\npm;$env:USERPROFILE\.local\bin;$env:LOCALAPPDATA\hermes\bin;$env:LOCALAPPDATA\agy\bin"
$ErrorActionPreference = 'Stop'
`
const remoteWindowsProbeScript = remoteWindowsPathPreamble + `$tools = @()
foreach ($name in @('hermes','claude','codex','pi','gemini','opencode','agy','npm','npx','node')) {
 $c = Get-Command $name -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
 if ($c) {
  $v = try { (& $c.Source --version 2>$null | Select-Object -First 1) } catch { '' }
  $tools += @{id=$name;path=$c.Source;version=[string]$v}
 }
}
$info = @{user=$env:USERNAME;host=$env:COMPUTERNAME;hostname=$env:COMPUTERNAME;home=$env:USERPROFILE;os='Windows';tools=$tools}
Write-Output ('LOOM-MACHINE ' + ($info | ConvertTo-Json -Compress -Depth 4))
`

func windowsRemoteSSHArgs(m RemoteMachine, key, script string) []string {
	words := utf16.Encode([]rune(script))
	encoded := make([]byte, 2*len(words))
	for i, w := range words {
		binary.LittleEndian.PutUint16(encoded[2*i:], w)
	}
	// Only a base64 payload crosses the SSH login shell. Host/user/port retain
	// the existing validation; no user value is inserted into executable code.
	return sshArgs(m, key, "powershell", "-NoProfile", "-NonInteractive", "-EncodedCommand", shellQuote(base64.StdEncoding.EncodeToString(encoded)))
}
func powershellLiteral(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
func windowsRemoteLifecycleCommand(m RemoteMachine, key string, argv []string) []string {
	script := remoteWindowsPathPreamble
	if len(argv) == 3 && argv[0] == "command" && argv[1] == "-v" {
		script += "$c = Get-Command " + powershellLiteral(argv[2]) + " -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1\nif (!$c) { exit 1 }; Write-Output $c.Source"
	} else {
		parts := make([]string, len(argv))
		for i, a := range argv {
			parts[i] = powershellLiteral(a)
		}
		if i := lifecycleScriptArg(argv); i >= 0 {
			script += "$loom_installer = Join-Path ([IO.Path]::GetTempPath()) ([IO.Path]::GetRandomFileName() + '.ps1')\ntry {\nInvoke-WebRequest -UseBasicParsing -Uri " + powershellLiteral(argv[i]) + " -OutFile $loom_installer\n"
			parts[i] = "$loom_installer"
			script += "& " + strings.Join(parts, " ") + "\n$loom_exit = $LASTEXITCODE\n} finally { Remove-Item -Force -ErrorAction SilentlyContinue $loom_installer }\nexit $loom_exit"
		} else {
			script += "& " + strings.Join(parts, " ") + "\nexit $LASTEXITCODE"
		}
	}
	return append([]string{"ssh"}, windowsRemoteSSHArgs(m, key, script)...)
}
