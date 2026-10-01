package platform

import (
	"path/filepath"
	"strings"
)

// engineCmdlineOwned dit si ce cmdline est un moteur que Loom a le droit
// d'arrêter : llama-server, ou le binaire loom/loom encore en `serve`
// avant l'exec. Un PID recyclé vers autre chose est refusé.
func EngineCmdlineOwned(cmdline string) bool {
	s := strings.ToLower(strings.ReplaceAll(cmdline, "\x00", " "))
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	if strings.Contains(s, "llama-server") {
		return true
	}
	fields := strings.Fields(s)
	base := filepath.Base(fields[0])
	if base != "loom" && !strings.HasPrefix(base, "loom") {
		return false
	}
	for _, f := range fields[1:] {
		if f == "serve" {
			return true
		}
	}
	return false
}
