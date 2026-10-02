package platform

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

const UIUnitName = "loom-ui"

func UIServiceName() string {
	if n := os.Getenv("LOOM_UI_SERVICE"); n != "" {
		return n
	}
	if n := os.Getenv("LOOM_UI_SERVICE"); n != "" {
		return n
	}
	return UIUnitName
}

func UIServiceActive() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	out, err := exec.Command("systemctl", "is-active", UIServiceName()).Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "active"
}

func CmdUI(args []string) error {
	action := "status"
	if len(args) > 0 {
		action = args[0]
	}
	if runtime.GOOS != "linux" {
		return fmt.Errorf("the ui command is only available on Linux (systemd)")
	}
	svc := UIServiceName()
	needsRoot := action == "start" || action == "stop" || action == "restart" || action == "enable" || action == "disable"
	var cmdArgs []string
	bin := "systemctl"
	if needsRoot && os.Geteuid() != 0 {
		bin = "sudo"
		cmdArgs = append(cmdArgs, "-n", "systemctl")
	}
	cmdArgs = append(cmdArgs, action, svc)
	cmd := exec.Command(bin, cmdArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
