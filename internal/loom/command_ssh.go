package loom

import (
	"encoding/base64"
	"encoding/binary"
	"strings"
	"unicode/utf16"
)

const remoteWindowsProbeScript = `$tools = @()
foreach ($name in @('ssh','git','python')) {
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
