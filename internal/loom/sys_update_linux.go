//go:build linux

package loom

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/lucas-lepajollec/loom/internal/loom/platform"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
)

const systemUpdateBinary = "/usr/local/bin/loom"
const systemUpdateHelper = "/usr/local/libexec/loom-update"
const systemUpdateRule = "/etc/sudoers.d/loom-update"

// No caller-chosen command, binary, environment, URL or target path. The current
// root-owned Loom handles the official release under a clean environment.
const systemUpdateScript = `#!/bin/sh
set -eu
[ "$#" -eq 0 ] || exit 2
exec /usr/bin/env -i PATH=/usr/sbin:/usr/bin:/sbin:/bin /bin/sh -c '
set -eu
cd /
for path in / /usr /usr/local /usr/local/bin /usr/local/bin/loom; do
  [ ! -L "$path" ] || exit 1
  [ "$(/usr/bin/stat -c %u "$path")" = 0 ] || exit 1
  mode=$(/usr/bin/stat -c %a "$path")
  [ "$((0$mode & 022))" -eq 0 ] || exit 1
done
[ -f /usr/local/bin/loom ] || exit 1
exec /usr/local/bin/loom system-update
'
`

func rootTrustedPath(path string) error {
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) {
		return fmt.Errorf("absolute path required")
	}
	for {
		fi, err := os.Lstat(path)
		if err != nil {
			return err
		}
		st, ok := fi.Sys().(*syscall.Stat_t)
		if !ok || st.Uid != 0 || fi.Mode()&os.ModeSymlink != 0 || fi.Mode().Perm()&0o022 != 0 || (!fi.IsDir() && !fi.Mode().IsRegular()) {
			return fmt.Errorf("%s must be root-owned, not symlinked or writable by other users", path)
		}
		if path == "/" {
			return nil
		}
		path = filepath.Dir(path)
	}
}

func canUseSystemUpdater(exe string) bool {
	if os.Geteuid() == 0 || exe != systemUpdateBinary {
		return false
	}
	if rootTrustedPath(systemUpdateBinary) != nil || rootTrustedPath(systemUpdateHelper) != nil {
		return false
	}
	return exec.Command("sudo", "-n", "-l", systemUpdateHelper).Run() == nil
}

func runSystemUpdater(expected, channel string) (string, error) {
	body, _ := json.Marshal(struct {
		Version string `json:"version"`
		Channel string `json:"channel,omitempty"`
	}{expected, channel})
	cmd := exec.Command("sudo", "-n", systemUpdateHelper)
	cmd.Stdin = bytes.NewReader(body)
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("system update failed: %s", strings.TrimSpace(stderr.String()))
	}
	var result struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(out.Bytes(), &result) != nil || result.Version == "" {
		return "", fmt.Errorf("invalid system updater response")
	}
	return result.Version, nil
}

func cmdSystemUpdate(args []string) error {
	if len(args) != 0 || os.Geteuid() != 0 {
		return fmt.Errorf("system-update is restricted to the installed administrator helper")
	}
	exe, err := os.Executable()
	if err != nil || exe != systemUpdateBinary {
		return fmt.Errorf("system-update requires %s", systemUpdateBinary)
	}
	if err := rootTrustedPath(exe); err != nil {
		return err
	}
	if err := rootTrustedPath(filepath.Dir(systemUpdateHelper)); err != nil {
		return err
	}
	// A lock in a root-only trusted directory bounds concurrent helper processes.
	lock, err := os.OpenFile("/usr/local/libexec/.loom-update.lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return fmt.Errorf("another system update is running")
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	var req struct {
		Version string `json:"version"`
		Channel string `json:"channel"`
	}
	dec := json.NewDecoder(io.LimitReader(os.Stdin, 128))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return fmt.Errorf("invalid system update request")
	}
	if req.Channel != "" && req.Channel != platform.ChannelStable && req.Channel != platform.ChannelEdge {
		return fmt.Errorf("invalid system update request")
	}
	// Both channels are official releases of the fixed repository, verified
	// against their published SHA256SUMS.
	rel, err := platform.FetchRelease(req.Channel)
	if err != nil {
		return err
	}
	if req.Version != "" && strings.TrimPrefix(ensureV(rel.TagName), "v") != req.Version {
		return fmt.Errorf("release changed; check again before updating")
	}
	version, err := installReleaseUpdate(rel, systemUpdateBinary)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]string{"version": version})
}

// Called only by the administrator installer. Source/symlink installs keep the
// portable updater and are never granted root execution of user-owned code.
func installSystemUpdater(targetUser string) error {
	if !regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]*\$?$`).MatchString(targetUser) {
		return fmt.Errorf("invalid updater user")
	}
	if err := rootTrustedPath(systemUpdateBinary); err != nil {
		fmt.Println("  Interface system updates unavailable for this source/symlink install; use a regular root-owned release binary.")
		return nil
	}
	dir := filepath.Dir(systemUpdateHelper)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := rootTrustedPath(dir); err != nil {
		return err
	}
	if err := writeUpdaterFile(systemUpdateHelper, []byte(systemUpdateScript), 0o755); err != nil {
		return err
	}
	rule := fmt.Sprintf("# Loom official-release updater only; no arguments or arbitrary commands.\n%s ALL=(root) NOPASSWD: %s \"\"\n", targetUser, systemUpdateHelper)
	f, err := os.CreateTemp("/etc/sudoers.d", ".loom-update-*")
	if err != nil {
		return err
	}
	temp := f.Name()
	defer os.Remove(temp)
	_, err = f.WriteString(rule)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := os.Chmod(temp, 0o440); err != nil {
		return err
	}
	if out, err := exec.Command("visudo", "-cf", temp).CombinedOutput(); err != nil {
		return fmt.Errorf("updater sudoers validation failed: %s", out)
	}
	if err := os.Rename(temp, systemUpdateRule); err != nil {
		return err
	}
	fmt.Println("  ✓ official-release updates enabled from the interface")
	return nil
}

func writeUpdaterFile(path string, data []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".loom-updater-*")
	if err != nil {
		return err
	}
	temp := f.Name()
	defer os.Remove(temp)
	_, err = f.Write(data)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := os.Chmod(temp, mode); err != nil {
		return err
	}
	return os.Rename(temp, path)
}
