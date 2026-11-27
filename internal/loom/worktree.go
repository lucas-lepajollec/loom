package loom

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// Git worktree isolation for Loom sub-sessions, inspired by Orchestrator.inc
// and T3 Code (.worktreeinclude bootstrap copying).
//
// Worktrees live under <repo>/.loom-worktrees/<slug>, excluded automatically
// via <git-common-dir>/info/exclude so tracked .gitignore and git status stay
// clean while remaining inside Codeman's authorized workspace directory.

const loomWorktreesDir = ".loom-worktrees"

type WorktreeInfo struct {
	RepoRoot     string   `json:"repo_root"`
	WorktreePath string   `json:"worktree_path"`
	Branch       string   `json:"branch"`
	CopiedFiles  []string `json:"copied_files,omitempty"`
}

type WorktreeStatus struct {
	Exists     bool   `json:"exists"`
	Branch     string `json:"branch,omitempty"`
	DirtyFiles int    `json:"dirty_files"`
	Ahead      int    `json:"ahead"`
}

var wtSlugBadRe = regexp.MustCompile(`[^a-z0-9-]+`)

func slugifyWorktree(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	s = strings.ReplaceAll(s, "_", "-")
	s = strings.ReplaceAll(s, " ", "-")
	s = wtSlugBadRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	if s == "" {
		s = "worker"
	}
	if len(s) > 32 {
		s = strings.TrimRight(s[:32], "-")
	}
	return s + "-" + randomHexID(3)
}

func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	trimmed := strings.TrimSpace(string(out))
	if err != nil {
		if trimmed != "" {
			return trimmed, fmt.Errorf("git %s: %s", strings.Join(args, " "), trimmed)
		}
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return trimmed, nil
}

func gitRepoRoot(ctx context.Context, dir string) (string, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return "", errors.New("working directory is required for worktree isolation")
	}
	root, err := runGit(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil || root == "" {
		return "", errors.New("working directory is not inside a Git repository")
	}
	return filepath.Clean(root), nil
}

func ensureGitExclude(ctx context.Context, repoRoot string, entries ...string) error {
	commonDir, err := runGit(ctx, repoRoot, "rev-parse", "--git-common-dir")
	if err != nil || commonDir == "" {
		return err
	}
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(repoRoot, commonDir)
	}
	excludePath := filepath.Join(commonDir, "info", "exclude")
	if err := os.MkdirAll(filepath.Dir(excludePath), 0o755); err != nil {
		return err
	}
	existingBytes, _ := os.ReadFile(excludePath)
	existing := string(existingBytes)
	lines := map[string]bool{}
	for _, line := range strings.Split(existing, "\n") {
		lines[strings.TrimSpace(line)] = true
	}
	var add []string
	for _, e := range entries {
		e = strings.TrimSpace(e)
		if e != "" && !lines[e] {
			add = append(add, e)
			lines[e] = true
		}
	}
	if len(add) == 0 {
		return nil
	}
	var b strings.Builder
	b.WriteString(existing)
	if len(existing) > 0 && !strings.HasSuffix(existing, "\n") {
		b.WriteByte('\n')
	}
	for _, e := range add {
		b.WriteString(e)
		b.WriteByte('\n')
	}
	return os.WriteFile(excludePath, []byte(b.String()), 0o644)
}

func copyFileContent(src, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode.Perm())
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func copyPathRecursive(src, dst string) error {
	st, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if st.Mode()&os.ModeSymlink != 0 {
		return nil // Never follow symlinks across repo boundaries.
	}
	if !st.IsDir() {
		return copyFileContent(src, dst, st.Mode())
	}
	if err := os.MkdirAll(dst, st.Mode().Perm()); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := copyPathRecursive(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

// copyWorktreeIncludes copies files listed in <repoRoot>/.worktreeinclude into
// the new worktree (T3 Code convention for local untracked bootstrap files).
func copyWorktreeIncludes(repoRoot, wtPath string) ([]string, error) {
	incPath := filepath.Join(repoRoot, ".worktreeinclude")
	f, err := os.Open(incPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	absRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		return nil, err
	}
	var copied []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || filepath.IsAbs(line) {
			continue
		}
		clean := filepath.Clean(line)
		if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
			continue
		}
		src := filepath.Join(absRoot, clean)
		rel, err := filepath.Rel(absRoot, src)
		if err != nil || strings.HasPrefix(rel, "..") {
			continue
		}
		if _, err := os.Lstat(src); err != nil {
			continue
		}
		dst := filepath.Join(wtPath, rel)
		if err := copyPathRecursive(src, dst); err == nil {
			copied = append(copied, filepath.ToSlash(rel))
		}
	}
	return copied, sc.Err()
}

// writeWorktreeMCPConfig drops a secret-free .mcp.json inside a Loom-owned
// worktree so MCP clients automatically discover Loom via `loom mcp-bridge`
// using the session's injected LOOM_MCP_URL / LOOM_MCP_TOKEN env vars.
func writeWorktreeMCPConfig(wtPath string) error {
	mcpFile := filepath.Join(wtPath, ".mcp.json")
	if _, err := os.Stat(mcpFile); err == nil {
		// Respect an existing project .mcp.json — never overwrite tracked config.
		return nil
	}
	bin, err := os.Executable()
	if err != nil || strings.TrimSpace(bin) == "" {
		bin = "loom"
	}
	bridgeArgs := []string{"mcp-bridge"}
	if envHome := strings.TrimSpace(os.Getenv("LOOM_HOME")); envHome != "" {
		bridgeArgs = append(bridgeArgs, "--home", envHome)
	}
	doc := map[string]any{
		"mcpServers": map[string]any{
			"loom": map[string]any{
				"command": bin,
				"args":    bridgeArgs,
			},
		},
	}
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return os.WriteFile(mcpFile, raw, 0o644)
}

func sanitizeBranchHint(branchHint, slug string) string {
	b := strings.TrimSpace(branchHint)
	if b == "" {
		return "loom/" + slug
	}
	// Strip unsafe Git ref characters.
	b = strings.ReplaceAll(b, " ", "-")
	b = strings.ReplaceAll(b, "..", "-")
	b = strings.TrimLeft(b, "/-")
	b = strings.TrimRight(b, "/.")
	if b == "" || b == "main" || b == "master" || b == "HEAD" {
		return "loom/" + slug
	}
	return b
}

func createLoomWorktree(ctx context.Context, baseDir, nameHint, branchHint string, writeMCP bool) (WorktreeInfo, error) {
	repoRoot, err := gitRepoRoot(ctx, baseDir)
	if err != nil {
		return WorktreeInfo{}, err
	}
	// If baseDir is already inside a worktree under .loom-worktrees, resolve the
	// main repository root so nested sub-workers don't nest .loom-worktrees/.
	if idx := strings.Index(filepath.ToSlash(repoRoot), "/"+loomWorktreesDir+"/"); idx > 0 {
		parentRepo := filepath.FromSlash(filepath.ToSlash(repoRoot)[:idx])
		if pr, err := gitRepoRoot(ctx, parentRepo); err == nil {
			repoRoot = pr
		}
	}
	slug := slugifyWorktree(nameHint)
	branch := sanitizeBranchHint(branchHint, slug)
	wtRoot := filepath.Join(repoRoot, loomWorktreesDir)
	if err := os.MkdirAll(wtRoot, 0o755); err != nil {
		return WorktreeInfo{}, fmt.Errorf("create worktree base dir: %w", err)
	}
	_ = ensureGitExclude(ctx, repoRoot, loomWorktreesDir+"/")

	wtPath := filepath.Join(wtRoot, slug)
	if _, err := runGit(ctx, repoRoot, "worktree", "add", "-b", branch, wtPath, "HEAD"); err != nil {
		// Retry once with a unique branch name if requested branch already exists.
		branch = "loom/" + slug
		if _, err2 := runGit(ctx, repoRoot, "worktree", "add", "-b", branch, wtPath, "HEAD"); err2 != nil {
			return WorktreeInfo{}, err2
		}
	}

	copied, _ := copyWorktreeIncludes(repoRoot, wtPath)
	if writeMCP {
		if err := writeWorktreeMCPConfig(wtPath); err == nil {
			_ = ensureGitExclude(ctx, wtPath, ".mcp.json")
		}
	}
	return WorktreeInfo{
		RepoRoot:     repoRoot,
		WorktreePath: wtPath,
		Branch:       branch,
		CopiedFiles:  copied,
	}, nil
}

func worktreeStatus(ctx context.Context, wtPath string) WorktreeStatus {
	wtPath = strings.TrimSpace(wtPath)
	if wtPath == "" {
		return WorktreeStatus{}
	}
	if st, err := os.Stat(wtPath); err != nil || !st.IsDir() {
		return WorktreeStatus{Exists: false}
	}
	res := WorktreeStatus{Exists: true}
	if b, err := runGit(ctx, wtPath, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		res.Branch = b
	}
	if out, err := runGit(ctx, wtPath, "status", "--porcelain"); err == nil && out != "" {
		for _, line := range strings.Split(out, "\n") {
			if strings.TrimSpace(line) != "" {
				res.DirtyFiles++
			}
		}
	}
	return res
}

func removeLoomWorktree(ctx context.Context, repoRoot, wtPath string, force bool) error {
	repoRoot = filepath.Clean(strings.TrimSpace(repoRoot))
	wtPath = filepath.Clean(strings.TrimSpace(wtPath))
	if repoRoot == "" || wtPath == "" {
		return errors.New("repo_root and worktree_path are required")
	}
	wtBase := filepath.Join(repoRoot, loomWorktreesDir)
	rel, err := filepath.Rel(wtBase, wtPath)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return errors.New("refusing to remove path outside .loom-worktrees")
	}
	if _, err := os.Stat(wtPath); errors.Is(err, os.ErrNotExist) {
		_, _ = runGit(ctx, repoRoot, "worktree", "prune")
		return nil
	}
	if !force {
		st := worktreeStatus(ctx, wtPath)
		if st.DirtyFiles > 0 {
			return fmt.Errorf("worktree has %d uncommitted file(s); commit or pass force=true to remove", st.DirtyFiles)
		}
	}
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, wtPath)
	if _, err := runGit(ctx, repoRoot, args...); err != nil {
		return err
	}
	_, _ = runGit(ctx, repoRoot, "worktree", "prune")
	return nil
}
