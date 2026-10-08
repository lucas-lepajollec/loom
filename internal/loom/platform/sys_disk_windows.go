package platform

// sys_disk_windows.go — espace libre d'un volume via GetDiskFreeSpaceExW. On
// veut l'espace réellement disponible pour l'utilisateur courant (quotas
// compris), donc le premier paramètre de sortie, pas le total du volume.

import (
	"errors"
	"syscall"
	"unsafe"
)

var procGetDiskFreeSpaceExW = syscall.NewLazyDLL("kernel32.dll").NewProc("GetDiskFreeSpaceExW")

// diskFreeAt renvoie les octets libres du volume contenant dir, ou -1 si la
// mesure échoue. dir doit exister (l'appelant remonte les parents au besoin).
func DiskFreeAt(dir string) int64 {
	available, _, _, err := diskSpaceAt(dir)
	if err != nil || available > 1<<62 {
		return -1
	}
	return int64(available)
}

// DiskUsageAt observes total and used bytes using the existing Windows API.
func DiskUsageAt(dir string) (used, total uint64, err error) {
	_, total, free, err := diskSpaceAt(dir)
	if err != nil {
		return 0, 0, err
	}
	return total - free, total, nil
}

func diskSpaceAt(dir string) (available, total, free uint64, err error) {
	p, err := syscall.UTF16PtrFromString(dir)
	if err != nil {
		return
	}
	r, _, _ := procGetDiskFreeSpaceExW.Call(uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&available)), uintptr(unsafe.Pointer(&total)), uintptr(unsafe.Pointer(&free)))
	if r == 0 {
		err = errors.New("disk space unavailable")
	}
	return
}
