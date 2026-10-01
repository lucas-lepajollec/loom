package platform

import "os"

// isTerminal reports whether stdout is a TTY. Stdlib-only check via FileMode.
func IsTerminal() bool {
	fi, err := os.Stdout.Stat()
	if err != nil || fi == nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
