package loom

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Keep the executable and launcher usable if a local self-updater fails or its
// replacement cannot report a version. Upstream runtime/account state remains
// owned by the agent; this snapshot contains only the selected executable.
func preserveHarnessExecutable(path string) (func(bool) error, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, err
	}
	source, err := os.Open(resolved)
	if err != nil {
		return nil, err
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil {
		return nil, err
	}
	backup, err := os.CreateTemp("", "loom-agent-backup-*")
	if err != nil {
		return nil, fmt.Errorf("preserve native executable: %w", err)
	}
	backupPath := backup.Name()
	_, copyErr := io.Copy(backup, source)
	chmodErr := backup.Chmod(info.Mode().Perm())
	closeErr := backup.Close()
	if err := errors.Join(copyErr, chmodErr, closeErr); err != nil {
		_ = os.Remove(backupPath)
		return nil, err
	}
	type savedLink struct{ path, target string }
	links := []savedLink{}
	for p := path; p != filepath.Dir(p); p = filepath.Dir(p) {
		if target, err := os.Readlink(p); err == nil {
			links = append(links, savedLink{p, target})
		}
	}
	return func(success bool) error {
		if success {
			return os.Remove(backupPath)
		}
		// Restore original files even if the updater removed the release directory.
		if err := os.MkdirAll(filepath.Dir(resolved), 0755); err != nil {
			return fmt.Errorf("restore native executable (backup %s): %w", backupPath, err)
		}
		if err := restoreHarnessExecutable(backupPath, resolved, info.Mode()); err != nil {
			return fmt.Errorf("restore native executable (backup %s): %w", backupPath, err)
		}
		var restoreErr error
		for i := len(links) - 1; i >= 0; i-- {
			link, target := links[i].path, links[i].target
			current, err := os.Readlink(link)
			if err == nil && current == target {
				continue
			}
			if err := os.Remove(link); err != nil && !os.IsNotExist(err) {
				restoreErr = errors.Join(restoreErr, err)
				continue
			}
			if err := os.Symlink(target, link); err != nil {
				restoreErr = errors.Join(restoreErr, err)
			}
		}
		return errors.Join(restoreErr, os.Remove(backupPath))
	}, nil
}

// The backup survives upstream release-directory cleanup. Restore via a
// temporary file beside the destination so promotion remains an atomic rename.
func restoreHarnessExecutable(backupPath, destination string, mode os.FileMode) error {
	source, err := os.Open(backupPath)
	if err != nil {
		return err
	}
	defer source.Close()
	target, err := os.CreateTemp(filepath.Dir(destination), ".loom-agent-restore-*")
	if err != nil {
		return err
	}
	defer os.Remove(target.Name())
	_, copyErr := io.Copy(target, source)
	chmodErr := target.Chmod(mode.Perm())
	closeErr := target.Close()
	if err := errors.Join(copyErr, chmodErr, closeErr); err != nil {
		return err
	}
	return os.Rename(target.Name(), destination)
}
