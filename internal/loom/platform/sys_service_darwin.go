//go:build darwin

package platform

import (
	"os"
	"strings"
	"syscall"
)

// processAlive : le signal 0 ne tue rien, il teste juste l'existence du process.
func ProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}

// tailFile renvoie les n dernières lignes d'un fichier (best-effort).
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
