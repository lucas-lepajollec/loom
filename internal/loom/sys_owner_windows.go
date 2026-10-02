//go:build windows

package loom

// Windows installs use per-machine folders with their own ACLs.
func adoptUserLoomHome() {}
