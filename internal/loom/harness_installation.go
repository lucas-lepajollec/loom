package loom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
)

const unknownHarnessChannel = "installed outside Loom's known channels; update it the way you installed it"

type harnessInstallation struct {
	Channel string
	Path    string
	Prefix  string
	Formula string
	Cask    bool
}

type harnessInstallationFiles struct {
	Home     string `json:"home"`
	Path     string `json:"path"`
	Resolved string `json:"resolved"`
	Shim     string `json:"shim"`
}

func installationPath(path string) string { return strings.ReplaceAll(path, `\`, "/") }

// Require evidence of package ownership. A generic <prefix>/bin executable is
// not evidence of npm; treating it as such creates a second installation.
func classifyHarnessInstallation(spec inspectSpec, files harnessInstallationFiles) harnessInstallation {
	result := harnessInstallation{Channel: "unknown", Path: files.Path}
	path, resolved := installationPath(files.Path), installationPath(files.Resolved)
	home := strings.TrimRight(installationPath(files.Home), "/")
	for _, p := range []string{resolved, path} {
		for _, kind := range []string{"Cellar", "Caskroom"} {
			marker := "/" + kind + "/"
			i := strings.Index(p, marker)
			if i < 0 {
				continue
			}
			root := p[:i]
			if root != "/usr/local" && root != "/opt/homebrew" && !strings.HasSuffix(root, "/.linuxbrew") {
				continue
			}
			formula := strings.Split(p[i+len(marker):], "/")[0]
			for _, name := range spec.BrewNames {
				if formula == name {
					result.Channel, result.Formula, result.Cask = "homebrew", formula, kind == "Caskroom"
					return result
				}
			}
		}
	}
	pkg := ""
	if spec.Latest != nil {
		pkg = spec.Latest.NPM
	}
	if pkg == "" {
		for _, cmd := range spec.Install {
			if lifecycleGlobalNPM(cmd) {
				pkg = npmPackage(cmd[3])
				break
			}
		}
	}
	if pkg != "" {
		for _, p := range []string{resolved, path} {
			marker := "/node_modules/" + pkg + "/"
			if i := strings.Index(p, marker); i >= 0 {
				prefix := strings.TrimSuffix(p[:i], "/lib")
				result.Channel, result.Prefix = "npm", prefix
				return result
			}
		}
		// Windows npm .cmd/.bat launchers identify their package explicitly.
		for _, match := range npmShimPath.FindAllStringSubmatch(files.Shim, -1) {
			if strings.HasPrefix(installationPath(match[1]), "node_modules/"+pkg+"/") && strings.LastIndex(path, "/") > 0 {
				result.Channel, result.Prefix = "npm", path[:strings.LastIndex(path, "/")]
				return result
			}
		}
	}
	for _, root := range spec.NativeRoots {
		root = strings.Replace(root, "~/", home+"/", 1)
		for _, p := range []string{resolved, path} {
			if (strings.HasSuffix(root, "/") && strings.HasPrefix(p, root)) || p == root {
				result.Channel = "native"
				return result
			}
		}
	}
	return result
}

func localInstallationFiles(path string) harnessInstallationFiles {
	home, _ := os.UserHomeDir()
	resolved, _ := filepath.EvalSymlinks(path)
	files := harnessInstallationFiles{Home: home, Path: path, Resolved: resolved}
	if strings.EqualFold(filepath.Ext(path), ".cmd") || strings.EqualFold(filepath.Ext(path), ".bat") {
		content, _ := os.ReadFile(path)
		files.Shim = string(content)
	}
	return files
}

// Candidates are read-only recovery observations. They are never used for
// generation or installation until the user chooses Repair.
func harnessRepairPatterns(spec inspectSpec, home string) []string {
	patterns := []string{}
	for _, p := range spec.RepairPaths {
		patterns = append(patterns, strings.Replace(p, "~/", home+"/", 1))
	}
	for _, prefix := range []string{home + "/.local", home + "/.npm-global", home + "/.nvm/versions/node/*", home + "/.volta/tools/image/packages/*", home + "/AppData/Roaming/npm"} {
		patterns = append(patterns, prefix+"/bin/"+spec.Binary, prefix+"/"+spec.Binary+".cmd", prefix+"/"+spec.Binary+".exe")
		pkg := ""
		if spec.Latest != nil {
			pkg = spec.Latest.NPM
		}
		if pkg != "" {
			patterns = append(patterns, prefix+"/lib/node_modules/"+pkg+"/package.json", prefix+"/node_modules/"+pkg+"/package.json")
		}
	}
	return patterns
}

func localHarnessInstallation(ctx context.Context, spec inspectSpec, path string) (harnessInstallation, error) {
	if path != "" {
		unwrapped, err := lifecycleUnwrapVolta(ctx, path, spec.Binary)
		if err != nil {
			return harnessInstallation{}, err
		}
		result := classifyHarnessInstallation(spec, localInstallationFiles(unwrapped))
		result.Path = path
		return result, nil
	}
	home, _ := os.UserHomeDir()
	patterns := harnessRepairPatterns(spec, home)
	// A user-configured npm prefix can live anywhere and be absent from PATH.
	if npm, err := lifecycleLookPath("npm"); err == nil {
		args, err := harnessNativeArgv([]string{npm, "prefix", "-g"})
		if err == nil {
			cmd := exec.CommandContext(ctx, args[0], args[1:]...)
			cmd.Env = append(os.Environ(), "PATH="+lifecycleLocalPath())
			if out, err := cmd.Output(); err == nil {
				prefix := strings.TrimSpace(string(out))
				if filepath.IsAbs(prefix) {
					bin, _ := npmLayout(prefix)
					patterns = append(patterns, filepath.Join(bin, spec.Binary), filepath.Join(bin, spec.Binary+".cmd"))
				}
			}
		}
	}
	for _, pattern := range patterns {
		paths, _ := filepath.Glob(filepath.FromSlash(pattern))
		// Current/stable launchers precede versioned candidates; among releases use
		// semantic versions rather than lexicographic order (2.10 follows 2.9).
		sort.SliceStable(paths, func(i, j int) bool { return harnessUpdateAvailable(paths[j], paths[i]) })
		for _, candidate := range paths {
			if filepath.Base(candidate) == "package.json" {
				content, _ := os.ReadFile(candidate)
				var pkg struct {
					Bin json.RawMessage `json:"bin"`
				}
				if json.Unmarshal(content, &pkg) != nil {
					continue
				}
				var entry string
				if json.Unmarshal(pkg.Bin, &entry) != nil {
					var entries map[string]string
					_ = json.Unmarshal(pkg.Bin, &entries)
					entry = entries[spec.Binary]
				}
				if entry == "" {
					continue
				}
				candidate = filepath.Join(filepath.Dir(candidate), entry)
			}
			st, err := os.Stat(candidate)
			if err != nil || st.IsDir() || (runtime.GOOS != "windows" && st.Mode()&0111 == 0) {
				continue
			}
			result := classifyHarnessInstallation(spec, localInstallationFiles(candidate))
			if result.Channel != "unknown" {
				return result, nil
			}
		}
	}
	return harnessInstallation{Channel: "unknown"}, nil
}

// SSH observations run on the target. Paired nodes use the local implementation
// in their own lifecycle service, not controller filesystem or environment.
func inspectHarnessInstallation(ctx context.Context, m *RemoteMachine, spec inspectSpec, path string) (harnessInstallation, error) {
	if m == nil {
		return localHarnessInstallation(ctx, spec, path)
	}
	if lifecycleOS(m) == "windows" {
		return inspectWindowsHarnessInstallation(ctx, m, spec, path)
	}
	script := unixHarnessInstallationScript(spec, path)
	out, err := runHarnessLifecycleRaw(ctx, m, []string{"sh", "-c", script})
	if err != nil {
		return harnessInstallation{}, err
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 3 {
		return harnessInstallation{Channel: "unknown"}, nil
	}
	return classifyHarnessInstallation(spec, harnessInstallationFiles{Home: lines[0], Path: lines[1], Resolved: lines[2]}), nil
}

func unixHarnessInstallationScript(spec inspectSpec, path string) string {
	script := "loom_path=" + shellQuote(path) + "\n"
	if path == "" {
		script += "for loom_candidate in"
		for _, pattern := range harnessRepairPatterns(spec, "$HOME") {
			// Only catalog-owned wildcards expand; all other text is quoted.
			parts := strings.Split(strings.TrimPrefix(pattern, "$HOME"), "*")
			for i := range parts {
				parts[i] = shellQuote(parts[i])
			}
			script += ` "$HOME"` + strings.Join(parts, "*")
		}
		script += "; do\n[ -x \"$loom_candidate\" ] || continue\nloom_path=$loom_candidate; break\ndone\n"
		script += `if [ -z "$loom_path" ] && command -v npm >/dev/null 2>&1; then
 loom_prefix=$(npm prefix -g 2>/dev/null) || loom_prefix=''
 case "$loom_prefix" in /*) loom_candidate="$loom_prefix/bin/"` + shellQuote(spec.Binary) + `; if [ -x "$loom_candidate" ]; then loom_path=$loom_candidate; fi ;; esac
fi
`
	}
	script += `printf '%s\n' "$HOME" "$loom_path"
loom_resolved=$loom_path
for loom_count in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16; do
 [ -L "$loom_resolved" ] || break
 loom_link=$(readlink "$loom_resolved") || exit 1
 case "$loom_link" in /*) loom_resolved=$loom_link ;; *) loom_resolved=$(dirname "$loom_resolved")/$loom_link ;; esac
done
if [ -e "$loom_resolved" ]; then
 loom_dir=$(cd -P "$(dirname "$loom_resolved")" && pwd) || exit 1
 loom_resolved=$loom_dir/$(basename "$loom_resolved")
fi
printf '%s\n' "$loom_resolved"
`
	return script
}

func inspectWindowsHarnessInstallation(ctx context.Context, m *RemoteMachine, spec inspectSpec, path string) (harnessInstallation, error) {
	script := "$p = " + powershellLiteral(path) + "\n"
	if path == "" {
		script += "$candidates = @("
		for i, pattern := range harnessRepairPatterns(spec, "$env:USERPROFILE") {
			if i > 0 {
				script += ","
			}
			script += "($env:USERPROFILE + " + powershellLiteral(strings.TrimPrefix(pattern, "$env:USERPROFILE")) + ")"
		}
		script += ")\nforeach ($candidate in $candidates) { $f = Get-Item $candidate -ErrorAction SilentlyContinue | Select-Object -First 1; if ($f -and !$f.PSIsContainer -and $f.Name -ne 'package.json') { $p=$f.FullName; break } }\n"
	}
	script += `if (!$p) {
 $npm=Get-Command npm -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
 if ($npm) { $prefix=(& $npm.Source prefix -g 2>$null | Select-Object -First 1); if ($prefix -and [IO.Path]::IsPathRooted($prefix)) { $candidate=Join-Path $prefix ` + powershellLiteral(spec.Binary+".cmd") + `; if (Test-Path -LiteralPath $candidate -PathType Leaf) { $p=$candidate } } }
}
$resolved=$p; $shim=''
if ($p) {
 $f=Get-Item -LiteralPath $p -ErrorAction Stop
 if ($f.Target) { $resolved=[string]($f.Target | Select-Object -First 1); if (![IO.Path]::IsPathRooted($resolved)) { $resolved=Join-Path $f.DirectoryName $resolved } }
 if ($p -match '\.(cmd|bat)$') { $shim=Get-Content -Raw -LiteralPath $p }
}
@{home=$env:USERPROFILE;path=$p;resolved=$resolved;shim=$shim} | ConvertTo-Json -Compress
`
	out, err := runHarnessLifecycleRaw(ctx, m, []string{"powershell", "-NoProfile", "-NonInteractive", "-Command", script})
	if err != nil {
		return harnessInstallation{}, err
	}
	var files harnessInstallationFiles
	if err := json.Unmarshal([]byte(out), &files); err != nil {
		return harnessInstallation{}, err
	}
	return classifyHarnessInstallation(spec, files), nil
}

var harnessUpdaterHelp = regexp.MustCompile(`(?m)^\s*(update|upgrade|self-update)(?:[ |\t]|$)`)

func (s *harnessLifecycleService) installationUpdateCommand(ctx context.Context, m *RemoteMachine, spec inspectSpec, install harnessInstallation) ([]string, error) {
	switch install.Channel {
	case "npm":
		for _, cmd := range spec.Install {
			if lifecycleGlobalNPM(cmd) {
				if lifecycleGlobalNPM(spec.Update) {
					return append([]string{}, spec.Update...), nil
				}
				return []string{"npm", "install", "-g", npmPackage(cmd[3]) + "@latest"}, nil
			}
		}
	case "native":
		updater := spec.NativeUpdate
		if len(updater) == 0 {
			help, err := s.run(ctx, m, []string{install.Path, "--help"})
			if err != nil {
				return nil, fmt.Errorf("read native updater help: %w", err)
			}
			match := harnessUpdaterHelp.FindStringSubmatch(help)
			if len(match) > 1 {
				updater = []string{match[1]}
			}
		}
		if len(updater) > 0 {
			return append([]string{install.Path}, updater...), nil
		}
		return nil, errors.New("native installation has no advertised self-updater; update it the way you installed it")
	case "homebrew":
		cmd := []string{"brew", "upgrade"}
		if install.Cask {
			cmd = append(cmd, "--cask")
		}
		return append(cmd, install.Formula), nil
	}
	return nil, errors.New(unknownHarnessChannel)
}

func repairHarnessInstallation(ctx context.Context, m *RemoteMachine, spec inspectSpec, path string) error {
	if path == "" {
		return errors.New("no known installation to repair")
	}
	if m == nil {
		if runtime.GOOS == "windows" {
			return putStoreJSON(bkState, "harness_executable:"+spec.Binary, path)
		}
		home, _ := os.UserHomeDir()
		link := filepath.Join(home, ".local", "bin", spec.Binary)
		if _, err := os.Stat(link); err == nil {
			return errors.New("existing launcher would be overwritten")
		}
		oldTarget, linkErr := os.Readlink(link)
		if _, err := os.Lstat(link); err == nil && linkErr != nil {
			return errors.New("existing launcher would be overwritten")
		}
		if err := os.MkdirAll(filepath.Dir(link), 0755); err != nil {
			return err
		}
		if linkErr == nil {
			if err := os.Remove(link); err != nil {
				return err
			}
		}
		if err := os.Symlink(path, link); err != nil {
			if linkErr == nil {
				_ = os.Symlink(oldTarget, link)
			}
			return err
		}
		if err := putStoreJSON(bkState, "harness_executable:"+spec.Binary, link); err != nil {
			_ = os.Remove(link)
			if linkErr == nil {
				_ = os.Symlink(oldTarget, link)
			}
			return err
		}
		return nil
	}
	// SSH has no Loom state store. Restore a missing launcher, never overwrite a
	// working executable. The subsequent inventory uses this same user bin path.
	if lifecycleOS(m) == "windows" {
		extension := strings.ToLower(filepath.Ext(path))
		if extension != ".cmd" && extension != ".bat" {
			extension = ".exe"
		}
		script := "$dir = Join-Path $env:USERPROFILE '.local/bin'\n$link = Join-Path $dir " + powershellLiteral(spec.Binary+extension) + "\nNew-Item -ItemType Directory -Force -Path $dir | Out-Null\n" + `if (Test-Path -LiteralPath $link) { throw 'existing launcher would be overwritten' }
$old=Get-Item -LiteralPath $link -Force -ErrorAction SilentlyContinue
if ($old) { if (!$old.LinkType) { throw 'existing launcher would be overwritten' }; Remove-Item -LiteralPath $link -Force }
New-Item -ItemType SymbolicLink -Path $link -Target ` + powershellLiteral(path) + ` | Out-Null
`
		_, err := runHarnessLifecycleRaw(ctx, m, []string{"powershell", "-NoProfile", "-NonInteractive", "-Command", script})
		return err
	}
	script := `mkdir -p "$HOME/.local/bin" || exit 1
loom_link="$HOME/.local/bin/"` + shellQuote(spec.Binary) + `
if [ -e "$loom_link" ]; then echo 'existing launcher would be overwritten' >&2; exit 1; fi
ln -sfn ` + shellQuote(path) + ` "$loom_link"`
	_, err := runHarnessLifecycleRaw(ctx, m, []string{"sh", "-c", script})
	return err
}
