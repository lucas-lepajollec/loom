package loom

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// engineKind dit si BIN est un llama.cpp complet (dépôt / sources au-dessus)
// ou un llama-server isolé (binaire officiel, copie seule).
func engineKind(bin string) string {
	bin = strings.TrimSpace(bin)
	if bin == "" || !isFile(bin) || !isLlamaServerName(filepath.Base(bin)) {
		return ""
	}
	if engineRepo(bin) != "" {
		return "full"
	}
	return "server"
}

// engineRepo remonte au checkout llama.cpp au-dessus du binaire. Vide si
// llama-server n'a pas de sources à côté (zip officiel, copie seule).
func engineRepo(bin string) string {
	bin = strings.TrimSpace(bin)
	if bin == "" {
		return ""
	}
	if real, err := filepath.EvalSymlinks(bin); err == nil {
		bin = real
	}
	return findRepoRoot(bin)
}

func isLlamaServerName(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	return n == "llama-server" || n == "llama-server.exe"
}

func isLlamaServerPath(p string) bool {
	p = strings.TrimSpace(p)
	if p == "" || !isFile(p) {
		return false
	}
	return isLlamaServerName(filepath.Base(p))
}

func engineServerName() string {
	if runtime.GOOS == "windows" {
		return "llama-server.exe"
	}
	return "llama-server"
}

// probedLlamaServers liste des llama-server déjà présents (PATH, arbres
// habituels, BIN actuel, installations Loom). Aucun chemin n'est écrit en dur
// dans la config : on ne propose que ce qui existe.
func probedLlamaServers() []string {
	var out []string
	seen := map[string]bool{}
	add := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" {
			return
		}
		if real, err := filepath.EvalSymlinks(p); err == nil {
			p = real
		}
		p = filepath.Clean(p)
		if !isLlamaServerPath(p) {
			return
		}
		key := strings.ToLower(filepath.ToSlash(p))
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, p)
	}

	if p, err := exec.LookPath("llama-server"); err == nil {
		add(p)
	}
	if runtime.GOOS == "windows" {
		if p, err := exec.LookPath("llama-server.exe"); err == nil {
			add(p)
		}
	}

	home, err := os.UserHomeDir()
	name := engineServerName()
	if err == nil && home != "" {
		for _, rel := range []string{
			filepath.Join("IA", "llama.cpp", "build", "bin", name),
			filepath.Join("llama.cpp", "build", "bin", name),
			filepath.Join("src", "llama.cpp", "build", "bin", name),
			filepath.Join("opt", "llama.cpp", "build", "bin", name),
		} {
			add(filepath.Join(home, rel))
		}
	}

	add(ReadConfig()["BIN"])
	add(llamaServerBin(defaultRepoDir()))
	add(prebuiltServerBin())
	return out
}

// resolveInstallDir choisit le dossier d'un clone llama.cpp. Vide = données Loom.
func resolveInstallDir(dir string) (string, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return defaultRepoDir(), nil
	}
	if !filepath.IsAbs(dir) {
		return "", fmt.Errorf("absolute directory required")
	}
	dir = filepath.Clean(dir)
	if isLlamaServerName(filepath.Base(dir)) && isFile(dir) {
		return "", fmt.Errorf("specify the llama.cpp directory, not the llama-server binary")
	}
	if isFile(dir) {
		return "", fmt.Errorf("this path is a file, not a directory: %s", dir)
	}
	return dir, nil
}
