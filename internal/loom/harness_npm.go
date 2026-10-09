package loom

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func lifecycleGlobalNPM(argv []string) bool {
	return len(argv) >= 4 && argv[0] == "npm" && (argv[1] == "install" || argv[1] == "update") && argv[2] == "-g"
}

// A write probe respects ACLs and mounted read-only filesystems. Probe the
// nearest existing ancestor without creating package directories or changing
// npm's global/user configuration.
func installDirWritable(path string) bool {
	for {
		st, err := os.Stat(path)
		if err == nil {
			if !st.IsDir() {
				return false
			}
			f, err := os.CreateTemp(path, ".loom-write-check-*")
			if err != nil {
				return false
			}
			name := f.Name()
			_ = f.Close()
			_ = os.Remove(name)
			return true
		}
		if !os.IsNotExist(err) {
			return false
		}
		parent := filepath.Dir(path)
		if parent == path {
			return false
		}
		path = parent
	}
}

func lifecycleNPMPrefix(ctx context.Context, argv []string) (string, error) {
	// npm's configured prefix need not own the executable Loom is using (nvm,
	// user prefixes and service PATHs commonly differ).
	if spec, ok := lifecycleNPMSpec(argv); ok {
		if path, err := lifecycleLookPath(spec.Binary); err == nil {
			install, err := localHarnessInstallation(ctx, spec, path)
			if err != nil {
				return "", err
			}
			if install.Channel != "npm" {
				return "", fmt.Errorf("refusing npm over %s installation: %s", install.Channel, unknownHarnessChannel)
			}
			prefix := install.Prefix
			if !npmPrefixWritable(prefix, argv) {
				return "", fmt.Errorf("existing npm installation is not writable: %s", prefix)
			}
			return prefix, nil
		}
	}
	npm, err := lifecycleLookPath("npm")
	if err != nil {
		return "", err
	}
	args, err := harnessNativeArgv([]string{npm, "prefix", "-g"})
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Env = append(os.Environ(), "PATH="+lifecycleLocalPath())
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("read npm global prefix: %w", err)
	}
	prefix := strings.TrimSpace(string(output))
	if !filepath.IsAbs(prefix) {
		return "", fmt.Errorf("npm returned an invalid global prefix")
	}
	if npmPrefixWritable(prefix, argv) {
		return prefix, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	prefix = filepath.Join(home, ".local")
	if !npmPrefixWritable(prefix, argv) {
		return "", fmt.Errorf("npm user installation directory is not writable: %s", prefix)
	}
	return prefix, nil
}

// Volta's launchers are executable routers, not symlinks. Its read-only which
// command identifies the installation without overwriting a managed shim.
func lifecycleUnwrapVolta(ctx context.Context, path, binary string) (string, error) {
	home := os.Getenv("VOLTA_HOME")
	if home == "" {
		userHome, _ := os.UserHomeDir()
		home = filepath.Join(userHome, ".volta")
	}
	if filepath.Dir(path) != filepath.Join(home, "bin") {
		return path, nil
	}
	volta, err := lifecycleLookPath("volta")
	if err != nil {
		return "", fmt.Errorf("resolve Volta installation: %w", err)
	}
	cmd := exec.CommandContext(ctx, volta, "which", binary)
	cmd.Env = append(os.Environ(), "PATH="+lifecycleLocalPath())
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("resolve Volta %s: %w", binary, err)
	}
	resolved := strings.TrimSpace(string(out))
	if !filepath.IsAbs(resolved) || resolved == path {
		return "", fmt.Errorf("invalid Volta executable for %s", binary)
	}
	return resolved, nil
}

func npmPackage(arg string) string {
	if i := strings.LastIndex(arg, "@"); i > 0 {
		return arg[:i]
	}
	return arg
}

func lifecycleNPMPackage(argv []string) string {
	if spec, ok := lifecycleNPMSpec(argv); ok && spec.Latest != nil && spec.Latest.NPM != "" {
		return spec.Latest.NPM
	}
	return npmPackage(argv[3])
}

func lifecycleNPMSpec(argv []string) (inspectSpec, bool) {
	if !lifecycleGlobalNPM(argv) {
		return inspectSpec{}, false
	}
	harnessInspectSpec("") // initialize the embedded catalog
	for _, spec := range inspectSpecs {
		for _, command := range spec.Install {
			if lifecycleGlobalNPM(command) && npmPackage(command[3]) == npmPackage(argv[3]) {
				return spec, true
			}
		}
	}
	return inspectSpec{}, false
}

func npmExecutablePrefix(path string) string {
	// Resolve symlinked npm launchers through node_modules first.
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		for dir := filepath.Dir(resolved); dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
			if filepath.Base(dir) == "node_modules" {
				parent := filepath.Dir(dir)
				if filepath.Base(parent) == "lib" {
					return filepath.Dir(parent)
				}
				return parent
			}
		}
	}
	if filepath.Base(filepath.Dir(path)) == "bin" {
		return filepath.Dir(filepath.Dir(path))
	}
	if runtime.GOOS == "windows" {
		return filepath.Dir(path)
	}
	return ""
}

func npmLayout(prefix string) (bin, modules string) {
	if runtime.GOOS == "windows" {
		return prefix, filepath.Join(prefix, "node_modules")
	}
	return filepath.Join(prefix, "bin"), filepath.Join(prefix, "lib", "node_modules")
}

func npmDestinationLayout(prefix, pkg string) (bin, modules string) {
	bin, modules = npmLayout(prefix)
	// Some managers keep their npm image directly under node_modules rather
	// than lib/node_modules. Keep that image in place for their existing shims.
	if runtime.GOOS != "windows" {
		direct := filepath.Join(prefix, "node_modules")
		if _, err := os.Stat(filepath.Join(direct, filepath.FromSlash(pkg))); err == nil {
			modules = direct
		}
	}
	return bin, modules
}

func npmPrefixWritable(prefix string, argv []string) bool {
	bin, modules := npmDestinationLayout(prefix, lifecycleNPMPackage(argv))
	paths := []string{prefix, bin, modules}
	paths = append(paths, filepath.Join(modules, filepath.FromSlash(lifecycleNPMPackage(argv))))
	for _, path := range paths {
		if !installDirWritable(path) {
			return false
		}
	}
	return true
}

const remoteNPMPrefixPreamble = `loom_npm_prefix=$(npm prefix -g) || exit 1
case "$loom_npm_prefix" in /*) ;; *) echo 'invalid npm global prefix' >&2; exit 1 ;; esac
loom_npm_writable=true
for loom_npm_dir in "$loom_npm_prefix" "$loom_npm_prefix/bin" "$loom_npm_prefix/lib/node_modules"; do
 while [ ! -e "$loom_npm_dir" ]; do loom_npm_dir=$(dirname "$loom_npm_dir"); done
 [ -d "$loom_npm_dir" ] && [ -w "$loom_npm_dir" ] || loom_npm_writable=false
done
if [ "$loom_npm_writable" != true ]; then loom_npm_prefix="$HOME/.local"; fi
`

// Check existing package directories too: a user-writable prefix can still
// contain a package created by an earlier sudo installation.
func remoteNPMPrefixScript(argv []string) string {
	extra := ""
	for _, arg := range argv[3:] {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		pkg := arg
		if i := strings.LastIndex(pkg, "@"); i > 0 {
			pkg = pkg[:i]
		}
		extra += ` "$loom_npm_prefix/lib/node_modules/"` + shellQuote(pkg)
	}
	preamble := remoteNPMPrefixPreamble
	if spec, ok := lifecycleNPMSpec(argv); ok {
		pkg := lifecycleNPMPackage(argv)
		preamble = `loom_existing=$(command -v ` + shellQuote(spec.Binary) + ` 2>/dev/null || true)
if [ -n "$loom_existing" ]; then
 loom_resolved=$loom_existing
 if command -v volta >/dev/null 2>&1; then case "$loom_existing" in */.volta/bin/*) loom_resolved=$(volta which ` + shellQuote(spec.Binary) + `) || exit 1 ;; esac; fi
 for loom_count in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16; do
  [ -L "$loom_resolved" ] || break
  loom_link=$(readlink "$loom_resolved") || exit 1
  case "$loom_link" in /*) loom_resolved=$loom_link ;; *) loom_resolved=$(dirname "$loom_resolved")/$loom_link ;; esac
 done
 loom_dir=$(cd -P "$(dirname "$loom_resolved")" && pwd) || exit 1
 loom_resolved=$loom_dir/$(basename "$loom_resolved")
 case "$loom_resolved" in
  */lib/node_modules/` + pkg + `/*) loom_npm_prefix=${loom_resolved%%/lib/node_modules/` + pkg + `/*} ;;
  */node_modules/` + pkg + `/*) loom_npm_prefix=${loom_resolved%%/node_modules/` + pkg + `/*} ;;
  *) echo "` + unknownHarnessChannel + `" >&2; exit 1 ;;
 esac
 for loom_npm_dir in "$loom_npm_prefix" "$loom_npm_prefix/bin" "$loom_npm_prefix/lib/node_modules"; do
  while [ ! -e "$loom_npm_dir" ]; do loom_npm_dir=$(dirname "$loom_npm_dir"); done
  [ -d "$loom_npm_dir" ] && [ -w "$loom_npm_dir" ] || { echo 'existing npm installation is not writable' >&2; exit 1; }
 done
else
` + preamble + "fi\n"
	}
	return strings.Replace(preamble, `"$loom_npm_prefix/lib/node_modules"; do`, `"$loom_npm_prefix/lib/node_modules"`+extra+`; do`, 1)
}

// Windows SSH npm launchers carry their owning prefix in the shim location.
// Never use npm's default prefix over an existing native/unknown executable.
func windowsRemoteNPMPrefixScript(argv []string) string {
	spec, known := lifecycleNPMSpec(argv)
	script := `$loom_existing=$null
`
	if known {
		script += "$loom_existing=Get-Command " + powershellLiteral(spec.Binary) + " -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1\n"
	}
	pkg := lifecycleNPMPackage(argv)
	script += `if ($loom_existing) {
 $loom_path=$loom_existing.Source
 $loom_file=Get-Item -LiteralPath $loom_path -ErrorAction Stop
 if ($loom_file.Target) { $loom_path=[string]($loom_file.Target | Select-Object -First 1); if (![IO.Path]::IsPathRooted($loom_path)) { $loom_path=Join-Path $loom_file.DirectoryName $loom_path } }
 $loom_normal=$loom_path.Replace('\','/')
 $loom_marker='/node_modules/'+` + powershellLiteral(pkg) + `+'/'
 $loom_index=$loom_normal.IndexOf($loom_marker)
 if ($loom_index -ge 0) { $loom_npm_prefix=$loom_normal.Substring(0,$loom_index) }
 elseif ($loom_path -match '\.(cmd|bat)$') {
  $loom_content=(Get-Content -Raw -LiteralPath $loom_path).Replace('\','/')
  if (!$loom_content.Contains('%dp0%/node_modules/'+` + powershellLiteral(pkg) + `+'/')) { throw ` + powershellLiteral(unknownHarnessChannel) + ` }
  $loom_npm_prefix=Split-Path -Parent $loom_path
 } else { throw ` + powershellLiteral(unknownHarnessChannel) + ` }
} else {
 $loom_npm_prefix=(& npm prefix -g | Select-Object -First 1)
}
if (!$loom_npm_prefix -or ![IO.Path]::IsPathRooted($loom_npm_prefix)) { throw 'invalid npm global prefix' }
function Test-LoomNpmWritable($prefix) {
 foreach ($dir in @($prefix,(Join-Path $prefix 'node_modules'),(Join-Path $prefix ` + powershellLiteral("node_modules/"+pkg) + `))) {
  while (!(Test-Path -LiteralPath $dir)) { $parent=Split-Path -Parent $dir; if (!$parent -or $parent -eq $dir) { return $false }; $dir=$parent }
  if (!(Test-Path -LiteralPath $dir -PathType Container)) { return $false }
  $probe=Join-Path $dir ([IO.Path]::GetRandomFileName())
  try { $stream=[IO.File]::Open($probe,[IO.FileMode]::CreateNew,[IO.FileAccess]::Write,[IO.FileShare]::None); $stream.Dispose() }
  catch { return $false }
  finally { if (Test-Path -LiteralPath $probe) { Remove-Item -LiteralPath $probe -Force } }
 }
 return $true
}
if (!(Test-LoomNpmWritable $loom_npm_prefix)) {
 if ($loom_existing) { throw 'existing npm installation is not writable' }
 $loom_npm_prefix=Join-Path $env:USERPROFILE '.local/bin'
 if (!(Test-LoomNpmWritable $loom_npm_prefix)) { throw 'npm user installation directory is not writable' }
}
`
	return script
}
