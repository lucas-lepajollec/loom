//go:build windows

package loom

import (
	"os/exec"
	"strconv"
	"syscall"
)

func ownedProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}
func killOwnedProcessGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		// taskkill /T covers the owned subprocess tree.
		_ = exec.Command("taskkill", "/PID", strconv.Itoa(cmd.Process.Pid), "/T", "/F").Run()
		_ = cmd.Process.Kill()
	}
}
