package platform

import (
	"math"
	"strconv"
	"strings"
)

// ParseProcMeminfo shares the Linux RAM interpretation between local readers
// and SSH observations. Values are bytes; a missing available value is unknown.
func ParseProcMeminfo(text string) (total, available uint64, haveAvailable bool) {
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) != 3 || f[2] != "kB" {
			continue
		}
		if f[0] != "MemTotal:" && f[0] != "MemAvailable:" {
			continue
		}
		n, err := strconv.ParseUint(f[1], 10, 64)
		if err != nil || n > math.MaxUint64/1024 {
			continue
		}
		if f[0] == "MemTotal:" {
			total = n * 1024
		} else {
			available, haveAvailable = n*1024, true
		}
	}
	return
}
