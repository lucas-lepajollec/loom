//go:build windows

package loom

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func pidFilePath() string { return filepath.Join(LoomHome(), serviceName()+".pid") }
func logFilePath() string { return filepath.Join(LoomHome(), serviceName()+".log") }

func serviceAction(action string) error {
	switch action {
	case "start":
		return svcStart()
	case "stop":
		return svcStop(true)
	case "restart":
		_ = svcStop(false)
		time.Sleep(500 * time.Millisecond)
		return svcStart()
	case "status":
		return svcStatus()
	case "enable", "disable":
		fmt.Printf("%s '%s' is not managed on Windows (no system service).\n", yellow("[info]"), action)
		fmt.Printf("       To start at boot, create a scheduled task or service using %s.\n", bold("sc.exe"))
		return nil
	default:
		return fmt.Errorf("unknown action: %s", action)
	}
}

func svcStart() error {
	if pid := readServicePID(); pid > 0 && processAlive(pid) {
		fmt.Printf("%s already started (PID %d)\n", yellow("[info]"), pid)
		return nil
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(LoomHome(), 0o755); err != nil {
		return err
	}
	logf, err := os.OpenFile(logFilePath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("opening log %s: %w", logFilePath(), err)
	}
	defer logf.Close()

	cmd := exec.Command(self, "serve")
	cmd.Stdout = logf
	cmd.Stderr = logf
	cmd.Dir = LoomHome() // les chemins relatifs de config.env se résolvent depuis LOOM_HOME
	// createNoWindow + HideWindow : le service enfant (`loom serve`) ne doit JAMAIS
	// faire clignoter de console noire quand Loom est lancé en mode app (double-clic).
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNewProcessGroup | detachedProcess | createNoWindow,
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting 'loom serve': %w", err)
	}
	pid := cmd.Process.Pid
	if err := os.WriteFile(pidFilePath(), []byte(strconv.Itoa(pid)), 0o644); err != nil {
		return fmt.Errorf("writing PID: %w", err)
	}
	// Don't wait — let it run detached.
	_ = cmd.Process.Release()
	return checkStarted(pid)
}

func checkStarted(pid int) error {
	time.Sleep(2 * time.Second)
	if processAlive(pid) {
		fmt.Printf("%s %s: started (PID %d)\n", green("[ok]"), serviceName(), pid)
		fmt.Printf("       logs: %s  (loom logs to follow)\n", dim(logFilePath()))
		return nil
	}
	fmt.Printf("%s %s: process stopped — latest logs:\n", red("[ERREUR]"), serviceName())
	fmt.Println("------------------------------------------------")
	fmt.Print(tailFile(logFilePath(), 20))
	fmt.Println("------------------------------------------------")
	fmt.Printf("→ loom logs   for more details\n→ loom edit   to fix config.env\n")
	_ = os.Remove(pidFilePath())
	return fmt.Errorf("service %s not started", serviceName())
}

func svcStop(verbose bool) error {
	pid := readServicePID()
	if pid <= 0 || !processAlive(pid) {
		_ = os.Remove(pidFilePath())
		if verbose {
			fmt.Println(yellow("[info]") + " no service running")
		}
		return nil
	}
	// taskkill /T tue aussi le processus enfant llama-server.
	cmd := hideCmd(exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/T", "/F"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("stopping PID %d: %w\n%s", pid, err, string(out))
	}
	_ = os.Remove(pidFilePath())
	if verbose {
		fmt.Println(green("[ok]") + " stopped")
	}
	return nil
}

func svcStatus() error {
	pid := readServicePID()
	if pid > 0 && processAlive(pid) {
		fmt.Printf("%s %s: active (PID %d)\n", green("[ok]"), serviceName(), pid)
	} else {
		fmt.Printf("%s %s: stopped\n", yellow("[info]"), serviceName())
	}
	fmt.Printf("  logs   : %s\n", logFilePath())
	return nil
}

func serviceLogs() error {
	path := logFilePath()
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("no log at %s (has the service ever started?): %w", path, err)
	}
	defer f.Close()
	// Print the tail, then follow appended bytes (poor man's tail -f).
	fmt.Print(tailFile(path, 80))
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		return err
	}
	buf := make([]byte, 4096)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			os.Stdout.Write(buf[:n])
		}
		if err == io.EOF {
			time.Sleep(500 * time.Millisecond)
			continue
		}
		if err != nil {
			return err
		}
	}
}

// serviceIsActive reports whether the background server is running.
func serviceIsActive() bool {
	pid := readServicePID()
	return pid > 0 && processAlive(pid)
}

func readServicePID() int {
	b, err := os.ReadFile(pidFilePath())
	if err != nil {
		return 0
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return pid
}

// serviceLogTail renvoie les n dernières lignes du journal du service (pour
// l'UI web).
func serviceLogTail(n int) string { return tailFile(logFilePath(), n) }
