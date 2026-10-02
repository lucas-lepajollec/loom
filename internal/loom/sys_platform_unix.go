//go:build unix

package loom

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// setLibraryPath ensures llama-server can load shared libs bundled next to the
// binary by prepending dir to LD_LIBRARY_PATH. It also appends the CUDA runtime
// lib directories: a CUDA-enabled build links against libcudart/libcublas, which
// live under /usr/local/cuda*/lib64 and are often absent from the global ld
// cache — without them llama-server fails to load the GPU backend (or runs
// degraded), costing a large chunk of throughput.
// Sur macOS, dyld IGNORE LD_LIBRARY_PATH : la variable équivalente est
// DYLD_LIBRARY_PATH. Les binaires macOS des releases llama.cpp sont livrés avec
// leurs libllama/libggml*.dylib À CÔTÉ de l'exécutable — sans cette variable,
// llama-server meurt instantanément sur « Library not loaded », donc sans le
// moindre message dans l'UI.
func setLibraryPath(dir string) {
	envVar, joined := libraryPathValue(dir)
	_ = os.Setenv(envVar, joined)
	if runtime.GOOS == "darwin" {
		// Filet de sécurité : DYLD_FALLBACK_LIBRARY_PATH est consulté en dernier
		// recours et survit à certains contextes où DYLD_LIBRARY_PATH est purgé.
		_ = os.Setenv("DYLD_FALLBACK_LIBRARY_PATH", joined+":/usr/local/lib:/usr/lib")
	}
}

// libraryPathValue calcule (nom de variable, valeur) sans toucher à
// l'environnement du process : utilisé pour lancer un llama-server ÉPHÉMÈRE
// (ex. `--list-devices` depuis l'UI) sans polluer — ni faire grossir à chaque
// appel — le LD_LIBRARY_PATH du serveur web lui-même.
func libraryPathValue(dir string) (string, string) {
	parts := []string{dir}
	parts = append(parts, cudaLibDirs()...)
	envVar := "LD_LIBRARY_PATH"
	if runtime.GOOS == "darwin" {
		envVar = "DYLD_LIBRARY_PATH"
	}
	if existing := os.Getenv(envVar); existing != "" {
		parts = append(parts, existing)
	}
	return envVar, strings.Join(parts, ":")
}

// libraryPathEnv renvoie un environnement complet (os.Environ + la variable de
// recherche de bibliothèques) pour une commande ponctuelle.
func libraryPathEnv(dir string) []string {
	k, v := libraryPathValue(dir)
	return append(os.Environ(), k+"="+v)
}

// cudaLibDirs returns the CUDA runtime lib directories present on the machine,
// preferring the highest-versioned install. Empty when no CUDA toolkit is found.
func cudaLibDirs() []string {
	var dirs []string
	seen := map[string]bool{}
	add := func(d string) {
		if d != "" && !seen[d] && isDir(d) {
			seen[d] = true
			dirs = append(dirs, d)
		}
	}
	// Default symlink first (usually points at the active toolkit).
	add("/usr/local/cuda/lib64")
	add("/usr/local/cuda/targets/x86_64-linux/lib")
	// Versioned installs, newest last so it takes precedence in PATH order.
	versioned, _ := filepath.Glob("/usr/local/cuda-*/lib64")
	sort.Strings(versioned)
	for i := len(versioned) - 1; i >= 0; i-- {
		add(versioned[i])
	}
	return dirs
}

// msvcGenerator is Windows-only; on Unix the default CMake generator (Unix
// Makefiles) is correct, so detectBuildPlan never calls this for real. Present
// only so the shared code compiles.
func msvcGenerator() string { return "" }

// ensureCompiler makes sure a C/C++ toolchain (cc + c++ + make) is present,
// installing it via the system package manager when missing. build-essential on
// Debian/Ubuntu pulls the lot; elsewhere we fall back to individual packages.
func ensureCompiler() error {
	haveCC := hasTool("cc") || hasTool("gcc") || hasTool("clang")
	haveCXX := hasTool("c++") || hasTool("g++") || hasTool("clang++")
	if haveCC && haveCXX && hasTool("make") {
		return nil
	}
	candidates := []string{"build-essential", "gcc", "g++", "make"}
	if _, err := exec.LookPath("apt-get"); err != nil {
		// Non-Debian: build-essential n'existe pas, on vise les paquets directs.
		candidates = []string{"gcc", "gcc-c++", "make"}
	}
	fmt.Println(yellow("[info]") + " C/C++ compiler missing — installing via the package manager…")
	for _, pkg := range candidates {
		_ = autoInstallTool(pkg) // best-effort, paquets variables selon la distro
	}
	if (hasTool("cc") || hasTool("gcc")) && (hasTool("c++") || hasTool("g++")) && hasTool("make") {
		fmt.Println(green("✓") + " C/C++ compiler ready.")
		return nil
	}
	return fmt.Errorf("C/C++ compiler not found — install gcc/g++/make (or build-essential) manually")
}

// ensureCudaVSIntegration is Windows-specific (MSBuild CUDA integration check);
// no-op on Unix.
func ensureCudaVSIntegration(toolkitDir string) error { return nil }

// ensureAccelerator is a no-op on Unix: CUDA/ROCm toolkits are installed through
// the distro (their layout is already probed by findNvcc / detectBuildPlan), and
// auto-installing multi-GB GPU toolkits across distros is too varied to do safely.
func ensureAccelerator() {}
