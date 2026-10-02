//go:build !windows

package loom

import (
	"os"
	"syscall"
)

func acpOpenRead(root *os.Root, rel string) (*os.File, error) {
	return root.OpenFile(rel, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}
