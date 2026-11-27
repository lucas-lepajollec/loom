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
// All repository/worktree file I/O is confined with os.OpenRoot.

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

var (
	wtSlugBadRe = regexp.MustCompile(`[^a-z0-9-]+`)
	wtBranchRe  = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9/_-]{0,63}$`)
	wtSlugRe    = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,47}$`)
)

const (
	slugAlphabet   = "abcdefghijklmnopqrstuvwxyz0123456789-"
	branchAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789/_-"
)

func filterAlphabet(raw, alphabet string, maxLen int) string {
	var b strings.Builder
	for i := 0; i < len(raw) && b.Len() < maxLen; i++ {
		idx := strings.IndexByte(alphabet, raw[i])
		if idx >= 0 {
			b.WriteByte(alphabet[idx])
		}
	}
	return b.String()
}

func slugifyWorktree(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	s = strings.ReplaceAll(s, "_", "-")
	s = strings.ReplaceAll(s, " ", "-")
	s = wtSlugBadRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	s = filterAlphabet(s, slugAlphabet, 32)
	s = strings.Trim(s, "-")
	if s == "" {
		s = "worker"
	}
	return s + "-" + filterAlphabet(randomHexID(3), slugAlphabet, 6)
}

func validateExistingDir(dir string) (string, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" || strings.ContainsRune(dir, '\x00') {
		return "", errors.New("working directory is required")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	realPath, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	root, err := os.OpenRoot(realPath)
	if err != nil {
		return "", errors.New("working directory does not exist")
	}
	_ = root.Close()
	return filepath.Clean(realPath), nil
}

func execGitFixed(ctx context.Context, dir string, argv ...string) (string, error) {
	cleanDir, err := validateExistingDir(dir)
	if err != nil {
		return "", err
	}
	cmd := hideCmd(exec.CommandContext(ctx, "git", argv...))
	cmd.Dir = cleanDir
	out, err := cmd.CombinedOutput()
	trimmed := strings.TrimSpace(string(out))
	if err != nil {
		if trimmed != "" {
			return trimmed, fmt.Errorf("git %s: %s", argv[0], trimmed)
		}
		return "", fmt.Errorf("git %s: %w", argv[0], err)
	}
	return trimmed, nil
}

func gitRepoRoot(ctx context.Context, dir string) (string, error) {
	root, err := execGitFixed(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil || root == "" {
		return "", errors.New("working directory is not inside a Git repository")
	}
	return validateExistingDir(root)
}

func gitStatusShort(ctx context.Context, dir string) (string, error) {
	return execGitFixed(ctx, dir, "status", "--short")
}

func ensureGitExclude(ctx context.Context, repoRoot string, entries ...string) error {
	commonDir, err := execGitFixed(ctx, repoRoot, "rev-parse", "--git-common-dir")
	if err != nil || commonDir == "" {
		return err
	}
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(repoRoot, commonDir)
	}
	commonDir, err = validateExistingDir(commonDir)
	if err != nil {
		return err
	}
	gitRoot, err := os.OpenRoot(commonDir)
	if err != nil {
		return err
	}
	defer gitRoot.Close()

	_ = gitRoot.Mkdir("info", 0o755)
	var existing string
	if f, err := gitRoot.Open("info/exclude"); err == nil {
		raw, _ := io.ReadAll(io.LimitReader(f, 256<<10))
		_ = f.Close()
		existing = string(raw)
	}
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
	out, err := gitRoot.OpenFile("info/exclude", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.WriteString(out, b.String())
	return err
}

func mkdirAllInRoot(root *os.Root, relDir string) error {
	relDir = filepath.ToSlash(filepath.Clean(relDir))
	if relDir == "." || relDir == "" {
		return nil
	}
	parts := strings.Split(relDir, "/")
	cur := ""
	for _, p := range parts {
		if p == "" || p == "." {
			continue
		}
		if cur == "" {
			cur = p
		} else {
			cur = cur + "/" + p
		}
		if err := root.Mkdir(cur, 0o755); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
	}
	return nil
}

func copyFileInRoots(srcRoot, dstRoot *os.Root, rel string, mode os.FileMode) error {
	if parent := filepath.Dir(rel); parent != "." && parent != "" {
		if err := mkdirAllInRoot(dstRoot, parent); err != nil {
			return err
		}
	}
	in, err := srcRoot.Open(rel)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := dstRoot.OpenFile(rel, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode.Perm())
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func copyPathInRoots(srcRoot, dstRoot *os.Root, rel string) error {
	st, err := srcRoot.Lstat(rel)
	if err != nil {
		return err
	}
	if st.Mode()&os.ModeSymlink != 0 {
		return nil // Never follow symlinks across repo boundaries.
	}
	if !st.IsDir() {
		return copyFileInRoots(srcRoot, dstRoot, rel, st.Mode())
	}
	if err := mkdirAllInRoot(dstRoot, rel); err != nil {
		return err
	}
	dir, err := srcRoot.Open(rel)
	if err != nil {
		return err
	}
	entries, err := dir.ReadDir(-1)
	_ = dir.Close()
	if err != nil {
		return err
	}
	for _, e := range entries {
		child := filepath.ToSlash(filepath.Join(rel, e.Name()))
		if err := copyPathInRoots(srcRoot, dstRoot, child); err != nil {
			return err
		}
	}
	return nil
}

// copyWorktreeIncludes copies files listed in <repoRoot>/.worktreeinclude into
// the new worktree (T3 Code convention for local untracked bootstrap files).
func copyWorktreeIncludes(repoFS, wtFS *os.Root) ([]string, error) {
	f, err := repoFS.Open(".worktreeinclude")
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var copied []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || filepath.IsAbs(line) {
			continue
		}
		clean := filepath.ToSlash(filepath.Clean(line))
		if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.ContainsRune(clean, '\x00') {
			continue
		}
		if _, err := repoFS.Lstat(clean); err != nil {
			continue
		}
		if err := copyPathInRoots(repoFS, wtFS, clean); err == nil {
			copied = append(copied, clean)
		}
	}
	return copied, sc.Err()
}

// writeWorktreeMCPConfig drops a secret-free .mcp.json inside a Loom-owned
// worktree so MCP clients automatically discover Loom via `loom mcp-bridge`
// using the session's scoped 0600 bridge file.
func writeWorktreeMCPConfig(wtFS *os.Root) error {
	if _, err := wtFS.Stat(".mcp.json"); err == nil {
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
	f, err := wtFS.OpenFile(".mcp.json", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(raw)
	return err
}

func sanitizeBranchHint(branchHint, slug string) string {
	raw := strings.TrimSpace(branchHint)
	if raw == "" || !wtBranchRe.MatchString(raw) || strings.Contains(raw, "..") || strings.HasSuffix(raw, "/") || raw == "main" || raw == "master" || raw == "HEAD" {
		return "loom/" + slug
	}
	safe := filterAlphabet(raw, branchAlphabet, 64)
	if safe == "" || safe != raw || strings.HasPrefix(safe, "-") || strings.HasPrefix(safe, "/") {
		return "loom/" + slug
	}
	return safe
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
	if !wtSlugRe.MatchString(slug) {
		slug = "worker-" + randomHexID(4)
	}
	branch := sanitizeBranchHint(branchHint, slug)

	repoFS, err := os.OpenRoot(repoRoot)
	if err != nil {
		return WorktreeInfo{}, fmt.Errorf("open repository root: %w", err)
	}
	defer repoFS.Close()

	if err := repoFS.Mkdir(loomWorktreesDir, 0o755); err != nil && !errors.Is(err, os.ErrExist) {
		return WorktreeInfo{}, fmt.Errorf("create worktree base dir: %w", err)
	}
	_ = ensureGitExclude(ctx, repoRoot, loomWorktreesDir+"/")

	relWT := loomWorktreesDir + "/" + slug
	wtPath := filepath.Join(repoRoot, loomWorktreesDir, slug)
	if _, err := execGitFixed(ctx, repoRoot, "worktree", "add", "-b", branch, "--", relWT, "HEAD"); err != nil {
		// Retry once with a unique branch name if requested branch already exists.
		branch = "loom/" + slug
		if _, err2 := execGitFixed(ctx, repoRoot, "worktree", "add", "-b", branch, "--", relWT, "HEAD"); err2 != nil {
			return WorktreeInfo{}, err2
		}
	}

	wtFS, err := repoFS.OpenRoot(relWT)
	if err != nil {
		return WorktreeInfo{}, fmt.Errorf("open worktree root: %w", err)
	}
	defer wtFS.Close()

	copied, _ := copyWorktreeIncludes(repoFS, wtFS)
	if writeMCP {
		if err := writeWorktreeMCPConfig(wtFS); err == nil {
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
	cleanDir, err := validateExistingDir(wtPath)
	if err != nil {
		return WorktreeStatus{Exists: false}
	}
	res := WorktreeStatus{Exists: true}
	if b, err := execGitFixed(ctx, cleanDir, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		res.Branch = b
	}
	if out, err := execGitFixed(ctx, cleanDir, "status", "--porcelain"); err == nil && out != "" {
		for _, line := range strings.Split(out, "\n") {
			if strings.TrimSpace(line) != "" {
				res.DirtyFiles++
			}
		}
	}
	return res
}

func removeLoomWorktree(ctx context.Context, repoRoot, wtPath string, force bool) error {
	cleanRepo, err := validateExistingDir(repoRoot)
	if err != nil {
		return errors.New("repo_root is invalid")
	}
	wtPath = filepath.Clean(strings.TrimSpace(wtPath))
	if wtPath == "" {
		return errors.New("worktree_path is required")
	}
	wtBase := filepath.Join(cleanRepo, loomWorktreesDir)
	rel, err := filepath.Rel(wtBase, wtPath)
	if err != nil || rel == "." || rel == ".." || strings.ContainsAny(rel, "/\\") || !wtSlugRe.MatchString(rel) {
		return errors.New("refusing to remove path outside .loom-worktrees")
	}
	relWT := loomWorktreesDir + "/" + rel

	repoFS, err := os.OpenRoot(cleanRepo)
	if err != nil {
		return err
	}
	defer repoFS.Close()

	if _, err := repoFS.Stat(relWT); errors.Is(err, os.ErrNotExist) {
		_, _ = execGitFixed(ctx, cleanRepo, "worktree", "prune")
		return nil
	}
	if !force {
		st := worktreeStatus(ctx, filepath.Join(cleanRepo, loomWorktreesDir, rel))
		if st.DirtyFiles > 0 {
			return fmt.Errorf("worktree has %d uncommitted file(s); commit or pass force=true to remove", st.DirtyFiles)
		}
	}
	if force {
		if _, err := execGitFixed(ctx, cleanRepo, "worktree", "remove", "--force", "--", relWT); err != nil {
			return err
		}
	} else {
		if _, err := execGitFixed(ctx, cleanRepo, "worktree", "remove", "--", relWT); err != nil {
			return err
		}
	}
	_, _ = execGitFixed(ctx, cleanRepo, "worktree", "prune")
	return nil
}
