//go:build !windows

package platform

// sys_disk_other.go — espace libre d'un système de fichiers via statfs(2).
// Bavail (et pas Bfree) : les blocs réservés à root ne sont pas utilisables pour
// poser un .gguf de 40 Go.

import "syscall"

// diskFreeAt renvoie les octets libres du système de fichiers contenant dir, ou
// -1 si la mesure échoue. dir doit exister (l'appelant remonte les parents au
// besoin).
func DiskFreeAt(dir string) int64 {
	available, _, _, err := diskSpaceAt(dir)
	if err != nil || available > 1<<62 {
		return -1
	}
	return int64(available)
}

// DiskUsageAt observes the filesystem containing dir, including reserved blocks.
func DiskUsageAt(dir string) (used, total uint64, err error) {
	_, total, free, err := diskSpaceAt(dir)
	if err != nil {
		return 0, 0, err
	}
	return total - free, total, nil
}

func diskSpaceAt(dir string) (available, total, free uint64, err error) {
	var st syscall.Statfs_t
	if err = syscall.Statfs(dir, &st); err != nil {
		return
	}
	// The native integer widths differ on Linux and macOS.
	size := uint64(st.Bsize)
	return uint64(st.Bavail) * size, uint64(st.Blocks) * size, uint64(st.Bfree) * size, nil
}
