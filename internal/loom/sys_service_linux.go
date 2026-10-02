//go:build linux

package loom

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// serviceAction démarre/arrête le moteur Loom. Sans unité systemd (le cas
// `go run` / pas encore installé), on supervise un processus enfant — jamais
// `sudo systemctl` sur une unité absente, et jamais loom-engine.
func serviceAction(action string) error {
	if systemdUnitLoaded() {
		return systemdServiceAction(action)
	}
	switch action {
	case "start":
		return directStart()
	case "stop":
		return directStop(true)
	case "restart":
		_ = directStop(false)
		time.Sleep(400 * time.Millisecond)
		return directStart()
	case "status":
		return directStatus()
	case "enable", "disable":
		return fmt.Errorf("no systemd unit %s — run loom install for a service, or use Start here", serviceName())
	default:
		return fmt.Errorf("unknown action: %s", action)
	}
}

func systemdUnitLoaded() bool {
	if isEngineWorker() {
		return false
	}
	out, err := exec.Command("systemctl", "show", serviceName(), "-p", "LoadState", "--value").Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "loaded"
}

func systemdServiceAction(action string) error {
	svc := serviceName()
	needsRoot := action == "start" || action == "stop" || action == "restart" || action == "enable" || action == "disable"
	args := []string{}
	bin := "systemctl"
	if needsRoot && os.Geteuid() != 0 {
		bin = "sudo"
		args = append(args, "-n", "systemctl")
	}
	args = append(args, action, svc)
	cmd := exec.Command(bin, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}
	switch action {
	case "start", "restart":
		return checkStarted(svc)
	case "stop":
		fmt.Println(green("[ok]") + " stopped")
	case "enable":
		fmt.Println(green("[ok]") + " automatic startup enabled")
	case "disable":
		fmt.Println(green("[ok]") + " automatic startup disabled")
	}
	return nil
}

// unitState renvoie ActiveState et SubState de l'unité (« activating » /
// « auto-restart » par exemple).
func unitState(svc string) (active, sub string) {
	out, _ := exec.Command("systemctl", "show", svc, "-p", "ActiveState", "-p", "SubState").Output()
	for _, line := range strings.Split(string(out), "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch k {
		case "ActiveState":
			active = v
		case "SubState":
			sub = v
		}
	}
	return
}

// checkStarted attend que l'unité se stabilise, puis dit la vérité.
//
// ⚠️ « activating » n'est PAS un succès : avec Restart=on-failure, un moteur qui
// meurt au démarrage repasse en boucle par activating (SubState auto-restart).
// L'ancien test l'acceptait, et `loom start` répondait « [ok] activating » sur
// un service qui ne démarrerait jamais — d'où des rapports « loom start dit ok
// mais loom test ne répond pas ». On distingue donc le SubState, et on laisse
// au moteur le temps de charger un gros modèle (il reste en activating, mais
// PAS en auto-restart).
func checkStarted(svc string) error {
	var state, sub string
	for i := 0; i < 5; i++ {
		time.Sleep(2 * time.Second)
		state, sub = unitState(svc)
		if state == "active" {
			fmt.Printf("%s %s: active\n", green("[ok]"), svc)
			return nil
		}
		if state == "failed" || sub == "auto-restart" {
			break // inutile d'attendre : il boucle sur un échec
		}
	}
	if state == "activating" && sub != "auto-restart" {
		// Chargement en cours (un gros .gguf prend des minutes) : légitime.
		fmt.Printf("%s %s: starting (loading model) — %s to follow\n",
			green("[ok]"), svc, bold("loom logs"))
		return nil
	}
	what := state
	if sub == "auto-restart" {
		what = "restarting in a loop (engine dies at startup)"
	}
	fmt.Printf("%s %s: %s — latest logs:\n", red("[ERREUR]"), svc, what)
	fmt.Println("------------------------------------------------")
	logs, _ := exec.Command("journalctl", "-u", svc, "-n", "20", "--no-pager").Output()
	fmt.Print(string(logs))
	fmt.Println("------------------------------------------------")
	fmt.Printf("→ loom logs   for more details\n→ loom edit   to fix the configuration (BIN, MODEL…)\n")
	return fmt.Errorf("service %s not started", svc)
}

func serviceLogs() error {
	if !systemdUnitLoaded() {
		fmt.Print(tailFile(directLogPath(), 80))
		return nil
	}
	cmd := exec.Command("journalctl", "-u", serviceName(), "-n", "80", "-f")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// serviceIsActive reports whether the owned engine is currently running.
func serviceIsActive() bool {
	if systemdUnitLoaded() {
		out, _ := exec.Command("systemctl", "is-active", serviceName()).Output()
		return strings.TrimSpace(string(out)) == "active"
	}
	pid := readServicePID()
	return pid > 0 && processAlive(pid) && ownedEnginePID(pid)
}

// serviceLogTail renvoie les n dernières lignes du journal du service (pour
// l'UI web). systemd : journalctl ; sinon le fichier de log du processus enfant.
func serviceLogTail(n int) string {
	if systemdUnitLoaded() {
		out, err := exec.Command("journalctl", "-u", serviceName(), "-n", strconv.Itoa(n), "--no-pager").CombinedOutput()
		if err != nil && len(out) == 0 {
			return "journalctl unavailable: " + err.Error()
		}
		return string(out)
	}
	return tailFile(directLogPath(), n)
}

func directPidPath() string { return filepath.Join(LoomHome(), serviceName()+".pid") }
func directLogPath() string { return filepath.Join(LoomHome(), serviceName()+".log") }

func readServicePID() int {
	b, err := os.ReadFile(directPidPath())
	if err != nil {
		return 0
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return pid
}

func ownedEnginePID(pid int) bool {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return false
	}
	return engineCmdlineOwned(string(b))
}

func directStart() error {
	if err := ensureLoomEngineBind(); err != nil {
		return err
	}
	if pid := readServicePID(); pid > 0 {
		if processAlive(pid) && ownedEnginePID(pid) {
			return nil
		}
		_ = os.Remove(directPidPath())
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(LoomHome(), 0o755); err != nil {
		return err
	}
	logf, err := os.OpenFile(directLogPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("opening journal: %w", err)
	}
	defer logf.Close()
	// Loom systemd fait Restart=on-failure : premier essai OOM (VRAM encore
	// occupée), second essai 3 s plus tard et le 27B passe. Sans unité, on
	// reproduisait ça en un seul coup — le moteur mourait à ~3 s et l'UI
	// restait « arrêté ». Un retry, comme systemd.
	var last string
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			time.Sleep(3 * time.Second)
		}
		pid, err := spawnServe(self, logf)
		if err != nil {
			last = err.Error()
			continue
		}
		time.Sleep(3500 * time.Millisecond)
		if processAlive(pid) {
			fmt.Printf("%s %s: started (PID %d)\n", green("[ok]"), serviceName(), pid)
			return nil
		}
		_ = os.Remove(directPidPath())
		last = lastEngineErr(tailFile(directLogPath(), 40))
	}
	return fmt.Errorf("the engine stopped — %s", last)
}

func lastEngineErr(log string) string {
	var found []string
	for _, l := range strings.Split(log, "\n") {
		low := strings.ToLower(l)
		if strings.Contains(l, "[err]") || strings.Contains(l, "BIN not set") ||
			strings.Contains(low, "exiting due to model") || strings.Contains(low, "out of memory") {
			found = append(found, strings.TrimSpace(l))
		}
	}
	if n := len(found); n > 0 {
		if n > 4 {
			found = found[n-4:]
		}
		return strings.Join(found, "\n")
	}
	return strings.TrimSpace(log)
}

func spawnServe(self string, logf *os.File) (int, error) {
	cmd := exec.Command(self, "serve")
	cmd.Stdout = logf
	cmd.Stderr = logf
	cmd.Dir = LoomHome()
	cmd.Env = append(os.Environ(), "LOOM_HOME="+LoomHome())
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("starting loom serve: %w", err)
	}
	pid := cmd.Process.Pid
	if err := os.WriteFile(directPidPath(), []byte(strconv.Itoa(pid)), 0o644); err != nil {
		_ = cmd.Process.Kill()
		return 0, fmt.Errorf("writing PID: %w", err)
	}
	go func() {
		_ = cmd.Wait()
		if readServicePID() == pid {
			_ = os.Remove(directPidPath())
		}
	}()
	return pid, nil
}

func directStop(verbose bool) error {
	pid := readServicePID()
	if pid <= 0 || !processAlive(pid) {
		_ = os.Remove(directPidPath())
		if verbose {
			fmt.Println(yellow("[info]") + " no Loom engine running")
		}
		return nil
	}
	if !ownedEnginePID(pid) {
		_ = os.Remove(directPidPath())
		return fmt.Errorf("PID %d is not a Loom engine — refusing to stop it", pid)
	}
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) && processAlive(pid) {
		time.Sleep(100 * time.Millisecond)
	}
	if processAlive(pid) {
		_ = syscall.Kill(-pid, syscall.SIGKILL)
	}
	_ = os.Remove(directPidPath())
	if verbose {
		fmt.Println(green("[ok]") + " stopped")
	}
	return nil
}

func directStatus() error {
	pid := readServicePID()
	if pid > 0 && processAlive(pid) && ownedEnginePID(pid) {
		fmt.Printf("%s %s: active (PID %d)\n", green("[ok]"), serviceName(), pid)
	} else {
		fmt.Printf("%s %s: stopped\n", yellow("[info]"), serviceName())
	}
	fmt.Printf("  logs : %s\n", directLogPath())
	return nil
}
