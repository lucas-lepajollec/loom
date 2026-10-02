//go:build !linux

package loom

import "fmt"

func canUseSystemUpdater(string) bool { return false }
func runSystemUpdater(string) (string, error) {
	return "", fmt.Errorf("system updater is only supported on Linux")
}
func cmdSystemUpdate([]string) error { return fmt.Errorf("system updater is only supported on Linux") }
