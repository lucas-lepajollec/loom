package doctor

import "os"

// Windows ACLs cannot be proven writable without a write; report the available
// read-only permission observation and let an actual operation report errors.
func Writable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().Perm()&0200 != 0
}
