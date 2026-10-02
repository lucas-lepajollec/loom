package loom

import "fmt"

func requireInstallWritable(paths ...string) error {
	for _, path := range paths {
		if !installDirWritable(path) {
			return fmt.Errorf("install/update directory is not writable: %s; use a user-owned engine directory or ask an administrator to repair this installation (do not run the interface as root)", path)
		}
	}
	return nil
}
