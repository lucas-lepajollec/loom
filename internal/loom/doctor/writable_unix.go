//go:build !windows

package doctor

import "golang.org/x/sys/unix"

// Writable observes effective access without creating a diagnostic file.
func Writable(path string) bool { return unix.Access(path, unix.W_OK) == nil }
