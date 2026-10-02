package loom

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	npm, err := lifecycleLookPath("npm")
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, npm, "prefix", "-g")
	cmd.Env = append(os.Environ(), "PATH="+lifecycleLocalPath())
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("read npm global prefix: %w", err)
	}
	prefix := strings.TrimSpace(string(output))
	if !filepath.IsAbs(prefix) {
		return "", fmt.Errorf("npm returned an invalid global prefix")
	}
	paths := []string{prefix, filepath.Join(prefix, "bin"), filepath.Join(prefix, "lib", "node_modules")}
	for _, arg := range argv[3:] {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		pkg := arg
		if i := strings.LastIndex(pkg, "@"); i > 0 {
			pkg = pkg[:i]
		}
		paths = append(paths, filepath.Join(prefix, "lib", "node_modules", filepath.FromSlash(pkg)))
	}
	writable := true
	for _, path := range paths {
		if !installDirWritable(path) {
			writable = false
			break
		}
	}
	if writable {
		return prefix, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	prefix = filepath.Join(home, ".local")
	for _, path := range []string{prefix, filepath.Join(prefix, "bin"), filepath.Join(prefix, "lib", "node_modules")} {
		if !installDirWritable(path) {
			return "", fmt.Errorf("npm user installation directory is not writable: %s", path)
		}
	}
	return prefix, nil
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
	return strings.Replace(remoteNPMPrefixPreamble, `"$loom_npm_prefix/lib/node_modules"; do`, `"$loom_npm_prefix/lib/node_modules"`+extra+`; do`, 1)
}
