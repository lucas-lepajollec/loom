package loom

import (
	"os"
	"os/exec"
	"path/filepath"

	"runtime"
	"strings"
)

func lifecycleOS(m *RemoteMachine) string {
	osName := runtime.GOOS
	if m != nil {
		osName = strings.ToLower(m.OS)
	}
	if strings.Contains(osName, "windows") || osName == "win32" {
		return "windows"
	}
	return "unix"
}

func lifecycleLocalPath() string {
	home, _ := os.UserHomeDir()
	dirs := []string{}
	if runtime.GOOS == "windows" {
		dirs = append(dirs, filepath.Join(os.Getenv("APPDATA"), "npm"), filepath.Join(home, ".local", "bin"))
	} else {
		dirs = append(dirs, filepath.Join(home, ".local", "bin"), filepath.Join(home, ".npm-global", "bin"), filepath.Join(home, ".bun", "bin"), filepath.Join(home, ".volta", "bin"), "/usr/local/bin", "/opt/homebrew/bin")
		nvm, _ := filepath.Glob(filepath.Join(home, ".nvm", "versions", "node", "*", "bin"))
		dirs = append(dirs, nvm...)
	}
	dirs = append(dirs, os.Getenv("PATH"))
	return strings.Join(dirs, string(os.PathListSeparator))
}

func lifecycleLookPath(name string) (string, error) {
	if filepath.IsAbs(name) {
		return exec.LookPath(name)
	}
	// Services launched before installation may have an older PATH.
	for _, d := range filepath.SplitList(lifecycleLocalPath()) {
		if d == "" {
			continue
		}
		if p, err := exec.LookPath(filepath.Join(d, name)); err == nil {
			return p, nil
		}
	}
	return "", exec.ErrNotFound
}
