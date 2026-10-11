package loom

import (
	"os"

	"path/filepath"
)

// A write probe respects ACLs and mounted read-only filesystems. Probe the
// nearest existing ancestor without creating package directories or changing
// npm's global/user configuration.
func installDirWritable(path string) bool {
	for {
		st, err := os.Stat(path)
		if err == nil {
			if !st.IsDir() {
				return false
			}
			f, err := os.CreateTemp(path, ".loom-write-check-*")
			if err != nil {
				return false
			}
			name := f.Name()
			_ = f.Close()
			_ = os.Remove(name)
			return true
		}
		if !os.IsNotExist(err) {
			return false
		}
		parent := filepath.Dir(path)
		if parent == path {
			return false
		}
		path = parent
	}
}
