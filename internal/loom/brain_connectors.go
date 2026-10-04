package loom

import (
	"context"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/brain"
)

var (
	gitBranchName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)
	gitSCPRemote  = regexp.MustCompile(`^git@[A-Za-z0-9][A-Za-z0-9.-]*:[^[:space:]]+$`)
)

func managedBrainSourcesDir() string { return filepath.Join(LoomHome(), "brain", "sources") }

func validateBrainRemote(raw, branch string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 2048 || strings.ContainsAny(raw, "\r\n\x00") {
		return errors.New("a Git remote is required")
	}
	if branch != "" && (!gitBranchName.MatchString(branch) || strings.Contains(branch, "..")) {
		return errors.New("invalid Git branch")
	}
	if strings.HasPrefix(raw, "git@") {
		if !gitSCPRemote.MatchString(raw) {
			return errors.New("invalid SSH Git remote")
		}
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "ssh") || u.Host == "" {
		return errors.New("use an HTTPS or SSH Git URL")
	}
	if strings.HasPrefix(u.Hostname(), "-") {
		return errors.New("invalid Git host")
	}
	if u.Scheme == "https" && u.User != nil {
		return errors.New("credentials in Git URLs are not allowed; use the machine credential helper or SSH keys")
	}
	if u.Scheme == "ssh" && u.User != nil {
		if _, hasPassword := u.User.Password(); hasPassword || !regexp.MustCompile(`^[A-Za-z0-9._-]+$`).MatchString(u.User.Username()) {
			return errors.New("credentials in Git URLs are not allowed; use the machine credential helper or SSH keys")
		}
	}
	return nil
}

func cloneBrainRemote(ctx context.Context, id, remote, branch string) (string, error) {
	if ok, _ := regexp.MatchString(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`, id); !ok || id == "memory" || id == "conversations" || id == "distilled" {
		return "", errors.New("invalid or reserved source id")
	}
	if err := validateBrainRemote(remote, branch); err != nil {
		return "", err
	}
	root := managedBrainSourcesDir()
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", err
	}
	dst := filepath.Join(root, id)
	if _, err := os.Stat(dst); err == nil {
		checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(checkCtx, "git", "-C", dst, "remote", "get-url", "origin")
		out, remoteErr := cmd.Output()
		if remoteErr != nil || strings.TrimSpace(string(out)) != strings.TrimSpace(remote) {
			return "", errors.New("this managed second-brain checkout already exists for another source")
		}
		return dst, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	args := []string{"clone", "--filter=blob:none", "--single-branch"}
	if branch != "" {
		args = append(args, "--branch", branch)
	}
	args = append(args, remote, dst)
	cloneCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(cloneCtx, "git", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		_ = os.RemoveAll(dst)
		if errors.Is(cloneCtx.Err(), context.DeadlineExceeded) {
			return "", errors.New("Git clone timed out")
		}
		msg := strings.TrimSpace(string(out))
		if len(msg) > 500 {
			msg = msg[len(msg)-500:]
		}
		if msg == "" {
			msg = "Git clone failed"
		}
		return "", errors.New(msg)
	}
	return dst, nil
}

func syncBrainRemote(ctx context.Context, source brain.Source) error {
	if source.Remote == "" || source.Path == "" {
		return errors.New("this source is not a managed Git repository")
	}
	if err := validateBrainRemote(source.Remote, source.Branch); err != nil {
		return err
	}
	syncCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(syncCtx, "git", "-C", source.Path, "pull", "--ff-only")
	if out, err := cmd.CombinedOutput(); err != nil {
		if errors.Is(syncCtx.Err(), context.DeadlineExceeded) {
			return errors.New("Git sync timed out")
		}
		msg := strings.TrimSpace(string(out))
		if len(msg) > 500 {
			msg = msg[len(msg)-500:]
		}
		if msg == "" {
			msg = "Git sync failed"
		}
		return errors.New(msg)
	}
	return nil
}
