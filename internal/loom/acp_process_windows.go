//go:build windows

package loom

import (
	"os/exec"
	"strconv"
	"syscall"
)

func acpProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}
func acpKillProcessGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		// taskkill /T covers the child tree created by npx and the native CLI.
		_ = exec.Command("taskkill", "/PID", strconv.Itoa(cmd.Process.Pid), "/T", "/F").Run()
		_ = cmd.Process.Kill()
	}
}
