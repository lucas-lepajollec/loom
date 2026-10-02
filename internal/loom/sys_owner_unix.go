//go:build !windows

package loom

import (
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// adoptUserLoomHome keeps a user's data owned by that user when a Loom
// command runs as root (sudo loom install, sudo loom ui start…). Without it,
// root would create LOOM_HOME, its folders or loom.db, and the services, which
// run as the user, could no longer write them. It also repairs data created
// that way by earlier versions.
func adoptUserLoomHome() {
	if os.Geteuid() != 0 {
		return
	}
	home := filepath.Clean(LoomHome())
	uid, gid, ok := nearestNonRootOwner(home)
	if !ok {
		return
	}
	// Missing folders between the user's existing directory and LOOM_HOME.
	var missing []string
	for d := home; ; d = filepath.Dir(d) {
		if _, err := os.Stat(d); err == nil || d == filepath.Dir(d) {
			break
		}
		missing = append(missing, d)
	}
	for i := len(missing) - 1; i >= 0; i-- {
		if os.Mkdir(missing[i], 0o755) == nil {
			_ = os.Chown(missing[i], uid, gid)
		}
	}
	for _, d := range dataDirs() {
		if _, err := os.Stat(d); os.IsNotExist(err) && os.MkdirAll(d, 0o755) == nil {
			_ = os.Chown(d, uid, gid)
		}
	}
	// An empty file is initialised by the store on first use and keeps its owner.
	if db := dbPath(); !pathExists(db) {
		if f, err := os.OpenFile(db, os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
			f.Close()
		}
	}
	// Hand back anything root created inside LOOM_HOME, and the parents up to
	// the user's own directory.
	_ = filepath.WalkDir(home, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if info, err := d.Info(); err == nil {
			if st, ok := info.Sys().(*syscall.Stat_t); ok && st.Uid == 0 {
				_ = os.Lchown(p, uid, gid)
			}
		}
		return nil
	})
	for d := filepath.Dir(home); ; d = filepath.Dir(d) {
		info, err := os.Stat(d)
		if err != nil || d == filepath.Dir(d) {
			break
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok || st.Uid != 0 {
			break
		}
		_ = os.Chown(d, uid, gid)
	}
}

// nearestNonRootOwner returns the owner of the closest existing ancestor of
// path (path included) when that owner is not root.
func nearestNonRootOwner(path string) (int, int, bool) {
	for d := path; ; d = filepath.Dir(d) {
		info, err := os.Stat(d)
		if err == nil {
			st, ok := info.Sys().(*syscall.Stat_t)
			if !ok {
				return 0, 0, false
			}
			if st.Uid != 0 {
				return int(st.Uid), int(st.Gid), true
			}
			// A root-owned folder directly inside a user's home (left by an
			// earlier install): keep looking upward for the user.
		}
		if d == filepath.Dir(d) {
			return 0, 0, false
		}
	}
}

func pathExists(p string) bool { _, err := os.Stat(p); return err == nil }
