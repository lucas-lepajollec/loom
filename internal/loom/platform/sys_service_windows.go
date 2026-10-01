//go:build windows

package platform

import (
	"os"
	"strings"
	"syscall"
	"unsafe"
)

// On Windows there is no systemd, so "the service" is a background copy of
// `loom serve` that this process launches detached. We track it with a PID file
// and stream its output to a log file under LOOM_HOME. This needs no admin
// rights and no external tools beyond the always-present tasklist/taskkill.

// processAlive reports whether a PID is currently running. On utilise l'API
// Win32 (OpenProcess + GetExitCodeProcess) plutôt que `tasklist` : l'ancien
// appel externe faisait CLIGNOTER une fenêtre de console à CHAQUE vérification,
// or l'UI web poll le statut en boucle → rafale de fenêtres noires. L'API ne
// lance aucun process.
func ProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	const (
		queryLimitedInfo = 0x1000
		stillActive      = 259
	)
	k := syscall.NewLazyDLL("kernel32.dll")
	h, _, _ := k.NewProc("OpenProcess").Call(queryLimitedInfo, 0, uintptr(pid))
	if h == 0 {
		return false
	}
	defer k.NewProc("CloseHandle").Call(h)
	var code uint32
	r, _, _ := k.NewProc("GetExitCodeProcess").Call(h, uintptr(unsafe.Pointer(&code)))
	if r == 0 {
		return false
	}
	return code == stillActive
}

// tailFile returns the last n lines of the file at path (best-effort).
func TailFile(path string, n int) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n") + "\n"
}
