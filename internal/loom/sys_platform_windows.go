//go:build windows

package loom

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// setLibraryPath ensures llama-server can load its dependent DLLs. Windows
// resolves them via PATH (and the binary's own directory), so we prepend dir.
// For a CUDA build we must also add the CUDA Toolkit's bin: ggml-cuda.dll links
// against cublas64_*/cublasLt64_*.dll which live there, not next to the binary —
// without it the server dies with 0xC0000135 (DLL not found) unless the launching
// shell happened to have CUDA on PATH.
func setLibraryPath(dir string) {
	parts := []string{dir}
	if nvcc := findNvcc(); nvcc != "" {
		binDir := filepath.Dir(nvcc) // …\CUDA\vX.Y\bin
		parts = append(parts, binDir)
		// CUDA 13+ a déplacé les DLL runtime (cublas64_*, cublasLt64_*, cudart64_*)
		// dans bin\x64\ ; sur CUDA 12 elles sont directement dans bin\. On ajoute
		// les deux pour couvrir les deux layouts.
		if x64 := filepath.Join(binDir, "x64"); isDir(x64) {
			parts = append(parts, x64)
		}
	}
	if existing := os.Getenv("PATH"); existing != "" {
		parts = append(parts, existing)
	}
	_ = os.Setenv("PATH", strings.Join(parts, string(os.PathListSeparator)))
}

// libraryPathEnv : même chose pour une commande ponctuelle (ex. --list-devices
// depuis l'UI), sans modifier le PATH du process web.
func libraryPathEnv(dir string) []string {
	parts := []string{dir}
	if nvcc := findNvcc(); nvcc != "" {
		binDir := filepath.Dir(nvcc)
		parts = append(parts, binDir)
		if x64 := filepath.Join(binDir, "x64"); isDir(x64) {
			parts = append(parts, x64)
		}
	}
	if existing := os.Getenv("PATH"); existing != "" {
		parts = append(parts, existing)
	}
	return append(os.Environ(), "PATH="+strings.Join(parts, string(os.PathListSeparator)))
}

// vswherePath returns the location of vswhere.exe, the official tool for
// locating Visual Studio / Build Tools installs. It ships in a fixed spot.
func vswherePath() string {
	base := os.Getenv("ProgramFiles(x86)")
	if base == "" {
		base = os.Getenv("ProgramFiles")
	}
	return filepath.Join(base, "Microsoft Visual Studio", "Install", "vswhere.exe")
}

// msvcInstallVersion returns the major version of the newest MSVC install that
// has the C++ toolchain (e.g. "17"), or "" if none is found.
func msvcInstallVersion() string {
	vs := vswherePath()
	if _, err := os.Stat(vs); err != nil {
		return ""
	}
	out, err := hideCmd(exec.Command(vs, "-latest", "-products", "*",
		"-requires", "Microsoft.VisualStudio.Component.VC.Tools.x86.x64",
		"-property", "installationVersion")).Output()
	if err != nil {
		return ""
	}
	ver := strings.TrimSpace(string(out))
	if ver == "" {
		return ""
	}
	if i := strings.IndexByte(ver, '.'); i > 0 {
		return ver[:i]
	}
	return ver
}

// msvcGenerator returns the CMake generator name for the installed MSVC, falling
// back to VS 2022 (the version `ensureCompiler` installs).
func msvcGenerator() string {
	switch msvcInstallVersion() {
	case "16":
		return "Visual Studio 16 2019"
	case "15":
		return "Visual Studio 15 2017"
	default:
		return "Visual Studio 17 2022"
	}
}

// ensureCompiler makes sure an MSVC C++ toolchain is available, installing the
// Visual Studio 2022 Build Tools (VCTools workload) via winget if not. This is a
// large download but keeps `loom llamacpp install` fully unattended on a bare
// Windows box.
func ensureCompiler() error {
	if msvcInstallVersion() != "" {
		return nil
	}
	if _, err := exec.LookPath("winget"); err != nil {
		return fmt.Errorf("C++ compiler missing and winget not found — install “Visual Studio Build Tools” (C++ workload) manually")
	}
	fmt.Printf("%s C++ compiler missing — installing MSVC Build Tools (large download, once only)…\n", yellow("[info]"))
	cmd := hideCmd(exec.Command("winget", "install", "--id", "Microsoft.VisualStudio.2022.BuildTools", "-e",
		"--accept-source-agreements", "--accept-package-agreements",
		"--disable-interactivity",
		"--override", "--quiet --wait --norestart --add Microsoft.VisualStudio.Workload.VCTools --includeRecommended"))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("MSVC Build Tools installation failed: %w", err)
	}
	if msvcInstallVersion() == "" {
		return fmt.Errorf("Build Tools installed but C++ toolchain not found — run the command again or check the Visual Studio installation")
	}
	fmt.Printf("%s C++ compiler ready.\n", green("✓"))
	return nil
}

// ensureAccelerator installs the CUDA Toolkit when an NVIDIA GPU is present but
// nvcc isn't, so the build can target the GPU instead of falling back to CPU.
// Best-effort: any failure just leaves the machine on the CPU path (the caller
// ignores the return value). The CUDA download is large; we only trigger it when
// a GPU is actually detected.
func ensureAccelerator() {
	if !hasNvidiaGPU() {
		return // pas de GPU NVIDIA visible → rien à installer, build CPU
	}
	if findNvcc() != "" {
		return // toolkit déjà présent
	}
	if _, err := exec.LookPath("winget"); err != nil {
		fmt.Printf("%s NVIDIA GPU detected but CUDA Toolkit missing and winget not found — CPU build (install the CUDA Toolkit for GPU acceleration)\n", yellow("[info]"))
		return
	}
	fmt.Printf("%s NVIDIA GPU detected — installing the CUDA Toolkit for GPU acceleration (large download, once only)…\n", yellow("[info]"))
	cmd := hideCmd(exec.Command("winget", "install", "--id", "Nvidia.CUDA", "-e",
		"--accept-source-agreements", "--accept-package-agreements",
		"--disable-interactivity"))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Printf("%s CUDA Toolkit installation failed (%v) — continuing with CPU\n", yellow("[warn]"), err)
		return
	}
	refreshToolPath()
	if findNvcc() != "" {
		fmt.Printf("%s CUDA Toolkit ready — GPU build enabled.\n", green("✓"))
	} else {
		fmt.Printf("%s CUDA Toolkit installed but nvcc not found in this session — run the command again to enable the GPU\n", yellow("[info]"))
	}
}

// ensureCudaVSIntegration vérifie que l'intégration MSBuild de CUDA (fichiers
// « CUDA x.y.props/targets » dans BuildCustomizations de Visual Studio) est en
// place — sans elle, le générateur Visual Studio échoue sur le cryptique
// « No CUDA toolset found » (CMakeDetermineCompilerId). Cas typiques : CUDA
// installé dans un chemin custom (ex. F:\Cuda) sans cocher « Visual Studio
// Integration », ou VS (ré)installé APRÈS CUDA — l'installeur NVIDIA n'intègre
// que les VS présents au moment où il tourne. On tente d'abord de copier les
// fichiers depuis le toolkit (extras\visual_studio_integration\MSBuildExtensions) ;
// si ça échoue (droits), on explique quoi faire au lieu de laisser l'erreur
// CMake brute. Best-effort : sans vswhere/VS détectable on laisse cmake juger.
func ensureCudaVSIntegration(toolkitDir string) error {
	vs := vswherePath()
	if _, err := os.Stat(vs); err != nil {
		return nil
	}
	out, err := hideCmd(exec.Command(vs, "-latest", "-products", "*",
		"-requires", "Microsoft.VisualStudio.Component.VC.Tools.x86.x64",
		"-property", "installationPath")).Output()
	if err != nil {
		return nil
	}
	installPath := strings.TrimSpace(string(out))
	if installPath == "" {
		return nil
	}
	dsts, _ := filepath.Glob(filepath.Join(installPath, "MSBuild", "Microsoft", "VC", "*", "BuildCustomizations"))
	for _, d := range dsts {
		if m, _ := filepath.Glob(filepath.Join(d, "CUDA *.props")); len(m) > 0 {
			return nil // intégration déjà en place
		}
	}
	// Absente : tentative de réparation depuis le toolkit lui-même.
	src := filepath.Join(toolkitDir, "extras", "visual_studio_integration", "MSBuildExtensions")
	files, _ := filepath.Glob(filepath.Join(src, "*"))
	copied := 0
	for _, dst := range dsts {
		ok := len(files) > 0
		for _, f := range files {
			b, err := os.ReadFile(f)
			if err != nil {
				ok = false
				break
			}
			if err := os.WriteFile(filepath.Join(dst, filepath.Base(f)), b, 0o644); err != nil {
				ok = false
				break
			}
		}
		if ok {
			copied++
		}
	}
	if copied > 0 {
		fmt.Printf("%s CUDA Visual Studio integration missing — repaired (files copied from %s)\n", yellow("[fix]"), src)
		return nil
	}
	return fmt.Errorf(`CUDA Visual Studio integration is missing: no “CUDA x.y.props” file under
  %s\MSBuild\Microsoft\VC\<version>\BuildCustomizations
Without it, CMake fails with “No CUDA toolset found”. To fix it, either:
  1. run the CUDA Toolkit installer again (custom installation) and select “CUDA → Visual Studio Integration” — Visual Studio must already be installed at that point;
  2. or copy (as administrator) the files from
       %s
     to the BuildCustomizations directory above, then run loom llamacpp install again`, installPath, src)
}
