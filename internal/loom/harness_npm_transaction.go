package loom

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// npm never touches the working installation until its replacement can run.
// The staging directory and backups live on the prefix's filesystem so that
// promotion and rollback use rename rather than a partial package copy.
func runHarnessNPMInstall(ctx context.Context, argv []string) (string, error) {
	prefix, err := lifecycleNPMPrefix(ctx, argv)
	if err != nil {
		return "", err
	}
	spec, known := lifecycleNPMSpec(argv)
	if !known {
		return "", errors.New("npm lifecycle package is not in the harness catalog")
	}
	previousPath, _ := lifecycleLookPath(spec.Binary)
	if err := os.MkdirAll(prefix, 0755); err != nil {
		return "", err
	}
	stage, err := os.MkdirTemp(prefix, ".loom-npm-")
	if err != nil {
		return "", err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(stage)
		}
	}()
	command := append([]string{}, argv[:3]...)
	// An installation under a version manager (nvm, volta, a private Node)
	// belongs to that Node's npm, not to whichever npm comes first on PATH.
	if own := filepath.Join(prefix, "bin", "npm"); runtime.GOOS != "windows" && isExecutableFile(own) {
		command[0] = own
	}
	command = append(command, "--prefix", stage)
	command = append(command, argv[3:]...)
	output, err := runHarnessLifecycleRaw(ctx, nil, command)
	if err != nil {
		return output, err
	}
	stageBin, stageModules := npmLayout(stage)
	bin, modules := npmDestinationLayout(prefix, lifecycleNPMPackage(argv))
	versionCommand := func(dir string) []string {
		path := filepath.Join(dir, spec.Binary)
		if runtime.GOOS == "windows" {
			if _, err := os.Stat(path + ".cmd"); err == nil {
				path += ".cmd"
			}
		}
		return append([]string{path}, spec.Version[1:]...)
	}
	verify := func(dir string) error {
		out, err := runHarnessLifecycleRaw(ctx, nil, versionCommand(dir))
		if err != nil {
			return fmt.Errorf("replacement --version failed: %w\n%s", err, out)
		}
		if strings.TrimSpace(out) == "" {
			return errors.New("replacement --version returned no version")
		}
		return nil
	}
	if err := verify(stageBin); err != nil {
		return output, err
	}
	if err := ctx.Err(); err != nil {
		return output, err
	}
	pkg := filepath.FromSlash(lifecycleNPMPackage(argv))
	entries, err := os.ReadDir(stageBin)
	if err != nil {
		return output, err
	}
	type replacement struct {
		dest, source, backup string
		saved, installed     bool
	}
	changes := []replacement{{dest: filepath.Join(modules, pkg), source: filepath.Join(stageModules, pkg), backup: filepath.Join(stage, "previous-package")}}
	// npm's bin directory contains only the package's launchers. On Windows it
	// also contains node_modules; launchers are files and relative .cmd shims.
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		source := filepath.Join(stageBin, entry.Name())
		if target, err := os.Readlink(source); err == nil {
			if !filepath.IsAbs(target) {
				target = filepath.Join(stageBin, target)
			}
			rel, err := filepath.Rel(stageModules, filepath.Clean(target))
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return output, errors.New("npm launcher points outside staged packages")
			}
			_ = os.Remove(source)
			if err := os.Symlink(filepath.Join(modules, rel), source); err != nil {
				return output, err
			}
		}
		changes = append(changes, replacement{dest: filepath.Join(bin, entry.Name()), source: source, backup: filepath.Join(stage, "previous-bin-"+entry.Name())})
	}
	rollback := func(cause error) (string, error) {
		for i := len(changes) - 1; i >= 0; i-- {
			c := changes[i]
			if c.installed {
				if err := os.RemoveAll(c.dest); err != nil {
					cause = errors.Join(cause, err)
					cleanup = false
				}
			}
			if c.saved {
				if err := os.Rename(c.backup, c.dest); err != nil {
					cause = errors.Join(cause, fmt.Errorf("restore %s: %w (backup %s)", c.dest, err, c.backup))
					cleanup = false
				}
			}
		}
		return output, cause
	}
	for i := range changes {
		c := &changes[i]
		if err := os.MkdirAll(filepath.Dir(c.dest), 0755); err != nil {
			return rollback(err)
		}
		if _, err := os.Lstat(c.dest); err == nil {
			if err := os.Rename(c.dest, c.backup); err != nil {
				return rollback(err)
			}
			c.saved = true
		} else if !os.IsNotExist(err) {
			return rollback(err)
		}
		if err := os.Rename(c.source, c.dest); err != nil {
			return rollback(err)
		}
		c.installed = true
	}
	if err := verify(bin); err != nil {
		return rollback(err)
	}
	path := versionCommand(bin)[0]
	if previousPath != "" {
		path = previousPath
	}
	if err := putStoreJSON(bkState, "harness_executable:"+spec.Binary, path); err != nil {
		return rollback(err)
	}
	return output, nil
}

func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode()&0111 != 0
}
