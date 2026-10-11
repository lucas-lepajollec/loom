//go:build windows

package loom

import (
	"os"
)

func confinedOpenRead(root *os.Root, rel string) (*os.File, error) { return root.Open(rel) }
