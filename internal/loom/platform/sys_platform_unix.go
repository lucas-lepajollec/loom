//go:build unix

package platform

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// defaultLoomHome is user-local Loom data. Never /etc/loom (that's Loom).
func DefaultLoomHome() string {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "loom")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".local", "share", "loom")
	}
	return filepath.Join(os.TempDir(), "loom")
}

func unixHome(name string) string {
	if runtime.GOOS != "darwin" || os.Geteuid() == 0 {
		return "/etc/" + name
	}
	if isWritableDir("/etc/" + name) {
		return "/etc/" + name // installé par `sudo loom install` puis chown à l'utilisateur
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, "Library", "Application Support", name)
	}
	return "/etc/" + name
}

// isWritableDir teste qu'un dossier existe ET qu'on peut y écrire (le seul test
// fiable : les permissions POSIX seules ignorent ACL, SIP, volumes read-only).
func isWritableDir(dir string) bool {
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		return false
	}
	probe := filepath.Join(dir, ".loom-write-test")
	f, err := os.OpenFile(probe, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return false
	}
	f.Close()
	_ = os.Remove(probe)
	return true
}

// defaultEditor is used by `loom edit` when $EDITOR is unset.
func DefaultEditor() string { return "nano" }

// hideCmd : no-op sur Unix (pas de fenêtre de console à masquer).
func HideCmd(cmd *exec.Cmd) *exec.Cmd { return cmd }

// openBrowser ouvre l'URL dans le navigateur par défaut (macOS: open, Linux:
// xdg-open). Best-effort.
func OpenBrowser(url string) error {
	bin := "xdg-open"
	if runtime.GOOS == "darwin" {
		bin = "open"
	}
	return exec.Command(bin, url).Start()
}

// totalRAMGB renvoie la RAM physique totale en Go (Linux: /proc/meminfo,
// macOS: sysctl hw.memsize).
func TotalRAMGB() float64 {
	if runtime.GOOS == "darwin" {
		out, err := exec.Command("sysctl", "-n", "hw.memsize").Output()
		if err != nil {
			return 0
		}
		if v, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64); err == nil {
			return v / (1024 * 1024 * 1024)
		}
		return 0
	}
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "MemTotal:") {
			f := strings.Fields(line)
			if len(f) >= 2 {
				if kb, err := strconv.ParseFloat(f[1], 64); err == nil {
					return kb / (1024 * 1024) // kB → Go
				}
			}
		}
	}
	return 0
}

// autoInstallTool installs a missing build tool with the system package manager
// (apt/dnf/pacman on Linux, brew on macOS). Best-effort: returns an error when no
// known manager is available or the install fails. Uses sudo on Linux when not
// already root (brew refuses to run as root).
func AutoInstallTool(name string) error {
	managers := []struct {
		bin     string
		install []string // args before the package name
	}{
		{"apt-get", []string{"install", "-y"}},
		{"dnf", []string{"install", "-y"}},
		{"pacman", []string{"-S", "--noconfirm"}},
		{"brew", []string{"install"}},
	}
	for _, m := range managers {
		if _, err := exec.LookPath(m.bin); err != nil {
			continue
		}
		argv := append(append([]string{m.bin}, m.install...), name)
		if m.bin != "brew" && os.Geteuid() != 0 {
			if _, err := exec.LookPath("sudo"); err != nil {
				return fmt.Errorf("%s requiert root (ni root ni sudo disponibles)", m.bin)
			}
			argv = append([]string{"sudo"}, argv...)
		}
		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.Stdout, cmd.Stderr, cmd.Stdin = os.Stdout, os.Stderr, os.Stdin
		return cmd.Run()
	}
	return fmt.Errorf("aucun gestionnaire de paquets connu — installe %s manuellement", name)
}

// cudaPathEnv is Windows-specific (the MSBuild CUDA integration needs CUDA_PATH);
// on Unix the Makefiles/Ninja generators find nvcc via PATH/CUDACXX, so there's
// nothing extra to inject.
func CudaPathEnv(toolkitDir string) []string { return nil }

// refreshToolPath is a no-op on Unix: package managers install into directories
// already on PATH (/usr/bin, /usr/local/bin), unlike Windows.
func RefreshToolPath() {}

// execServer replaces the current process with llama-server (so systemd
// supervises llama-server directly, as the old start.sh did with `exec`).
// args[0] must be the binary path.
func ExecServer(bin string, args []string) error {
	return syscall.Exec(bin, args, os.Environ())
}

// newShellCmd builds the command used by the run_shell tool. The second return
// value is a cleanup func the caller must defer (a no-op here; on Windows it
// removes the temporary .bat file — see the Windows twin).
func NewShellCmd(ctx context.Context, command string) (*exec.Cmd, func()) {
	return exec.CommandContext(ctx, "/bin/bash", "-c", command), func() {}
}

// ramUsageMB renvoie (utilisée, totale) en Mo pour /api/ram. Linux : /proc/meminfo.
// macOS : sysctl pour le total, vm_stat pour ce qui est réellement libre (pages
// free + inactive + speculative ; le reste — wired, active, compressé — est
// considéré occupé, comme le fait le Moniteur d'activité).
func RamUsageMB() (used, total int) {
	if runtime.GOOS == "darwin" {
		total = int(TotalRAMGB() * 1024)
		if total == 0 {
			return 0, 0
		}
		out, err := exec.Command("vm_stat").Output()
		if err != nil {
			return 0, total
		}
		pageSize := 4096
		freePages := 0
		for _, line := range strings.Split(string(out), "\n") {
			if strings.HasPrefix(line, "Mach Virtual Memory Statistics") {
				// « … (page size of 16384 bytes) » — l'Apple Silicon n'est pas en 4 Ko.
				if i := strings.Index(line, "page size of "); i >= 0 {
					if v, err := strconv.Atoi(strings.Fields(line[i+len("page size of "):])[0]); err == nil {
						pageSize = v
					}
				}
				continue
			}
			k, v, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			switch strings.TrimSpace(k) {
			case "Pages free", "Pages inactive", "Pages speculative":
				if n, err := strconv.Atoi(strings.Trim(strings.TrimSpace(v), ".")); err == nil {
					freePages += n
				}
			}
		}
		freeMB := freePages * pageSize / (1024 * 1024)
		if freeMB > total {
			freeMB = total
		}
		return total - freeMB, total
	}
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	var totalKB, availKB int
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		v, _ := strconv.Atoi(f[1]) // kB
		switch f[0] {
		case "MemTotal:":
			totalKB = v
		case "MemAvailable:":
			availKB = v
		}
	}
	if totalKB == 0 {
		return 0, 0
	}
	return (totalKB - availKB) / 1024, totalKB / 1024
}

// --- Supervision de processus détachés (worker de lien, service en mode
// utilisateur). Pendant Unix de sys_platform_windows.go.

// spawnDetached prépare une commande qui survivra à la mort de Loom : Setsid la
// place dans une nouvelle session, donc elle n'est pas tuée avec notre groupe.
func SpawnDetached(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd
}

// pidAlive : le signal 0 ne tue rien, il teste juste l'existence du process.
func PidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}

// killTree arrête le process ET ses enfants. Setsid ayant fait de lui un chef de
// groupe, un PID négatif vise le groupe entier ; SIGKILL en dernier recours.
func KillTree(pid int) {
	if pid <= 0 {
		return
	}
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil {
		_ = syscall.Kill(pid, syscall.SIGTERM)
	}
	for i := 0; i < 30 && PidAlive(pid); i++ {
		time.Sleep(100 * time.Millisecond)
	}
	if PidAlive(pid) {
		_ = syscall.Kill(-pid, syscall.SIGKILL)
	}
}
