package loom

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// A project links what its discussions need: its folder (and the Git
// repository there), context files read from that folder, a default work
// folder for harnesses and the execution a new discussion starts with.

const (
	maxProjectContextFiles = 8
	maxProjectFileBytes    = 64 << 10
	maxProjectContextBytes = 48 << 10
)

// projectFilePath resolves a context file inside the project folder; it
// refuses anything outside it (absolute paths, "..", symlinks escaping it).
func projectFilePath(dir, rel string) (string, error) {
	rel = filepath.Clean(strings.TrimSpace(rel))
	if dir == "" || rel == "." || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("fichier hors du dossier du projet")
	}
	full := filepath.Join(dir, rel)
	real, err := filepath.EvalSymlinks(full)
	if err != nil {
		return "", errors.New("fichier introuvable : " + rel)
	}
	root, err := filepath.EvalSymlinks(dir)
	if err != nil || (real != root && !strings.HasPrefix(real, root+string(filepath.Separator))) {
		return "", errors.New("fichier hors du dossier du projet")
	}
	info, err := os.Stat(real)
	if err != nil || !info.Mode().IsRegular() {
		return "", errors.New("pas un fichier : " + rel)
	}
	if info.Size() > maxProjectFileBytes {
		return "", errors.New(rel + " dépasse 64 Ko")
	}
	return real, nil
}

func validProjectContextFiles(dir string, files []string) ([]string, error) {
	if len(files) > maxProjectContextFiles {
		return nil, errors.New("8 fichiers de contexte maximum")
	}
	out := []string{}
	seen := map[string]bool{}
	for _, f := range files {
		if _, err := projectFilePath(dir, f); err != nil {
			return nil, err
		}
		rel := filepath.ToSlash(filepath.Clean(f))
		if !seen[rel] {
			seen[rel] = true
			out = append(out, rel)
		}
	}
	return out, nil
}

// projectContextFiles reads the chosen files for the discussion context,
// within the total budget. A missing file is reported, never fatal.
func projectContextFiles(p ChatProject) ([]string, string) {
	parts := []string{}
	warning := ""
	total := 0
	for _, rel := range p.ContextFiles {
		full, err := projectFilePath(p.Directory, rel)
		if err != nil {
			warning = "Un fichier de contexte du projet est introuvable et ne sera pas envoyé."
			continue
		}
		b, err := os.ReadFile(full)
		if err != nil {
			warning = "Un fichier de contexte du projet est illisible et ne sera pas envoyé."
			continue
		}
		if total+len(b) > maxProjectContextBytes {
			warning = "Les fichiers de contexte du projet dépassent 48 Ko : les derniers ne sont pas envoyés."
			break
		}
		total += len(b)
		parts = append(parts, "Project file "+rel+":\n"+strings.TrimSpace(string(b)))
	}
	return parts, warning
}

// projectDir validates a project folder on its machine: an existing folder
// here, an absolute path on a remote machine (Loom cannot check it there).
func projectDir(machine, dir string) (string, error) {
	dir = strings.TrimSpace(dir)
	if machine != "" {
		return remoteWorkdir(dir)
	}
	if !filepath.IsAbs(dir) {
		return "", errors.New("le dossier doit être un chemin absolu")
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return "", errors.New("ce dossier n’existe pas ou n’est pas accessible : " + dir)
	}
	return filepath.Clean(dir), nil
}

func machineByID(id string) *RemoteMachine {
	for _, m := range loadRemoteMachines() {
		if m.ID == id {
			return &m
		}
	}
	return nil
}

// projectFolders gives a harness its project's folders when they live on the
// harness's machine: the main one and, for local harnesses, the extra ones.
func projectFolders(projectID string, agent acpAgent) (string, []string) {
	if projectID == "" {
		return "", nil
	}
	p, ok := getProject(projectID)
	if !ok || p.Directory == "" {
		return "", nil
	}
	if agent.Remote {
		if p.Machine != "" && p.Machine == agent.Machine {
			return p.Directory, nil
		}
		return "", nil
	}
	if p.Machine != "" {
		return "", nil
	}
	if info, err := os.Stat(p.Directory); err != nil || !info.IsDir() {
		return "", nil
	}
	extra := []string{}
	for _, d := range p.ExtraDirs {
		if info, err := os.Stat(d); err == nil && info.IsDir() {
			extra = append(extra, d)
		}
	}
	return p.Directory, extra
}

// projectWorkdir is the project folder of this machine (terminals, previews).
func projectWorkdir(projectID string) string {
	dir, _ := projectFolders(projectID, acpAgent{})
	return dir
}

type projectRepo struct {
	Root    string `json:"root"`
	Branch  string `json:"branch,omitempty"`
	Remote  string `json:"remote,omitempty"`
	Changed int    `json:"changed"`
	Last    string `json:"last,omitempty"`
	LastAt  int64  `json:"last_at,omitempty"`
}

func gitOut(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0")
	b, err := cmd.Output()
	return strings.TrimSpace(string(b)), err
}

// cleanRemote drops any credentials embedded in a remote URL.
func cleanRemote(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Scheme != "" && u.Host != "" {
		u.User = nil
		return u.String()
	}
	return raw // scp-like git@host:path carries no secret
}

func readProjectRepo(dir string) *projectRepo {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	root, err := gitOut(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil || root == "" {
		return nil
	}
	r := &projectRepo{Root: root}
	r.Branch, _ = gitOut(ctx, dir, "branch", "--show-current")
	if remote, err := gitOut(ctx, dir, "remote", "get-url", "origin"); err == nil {
		r.Remote = cleanRemote(remote)
	}
	if st, err := gitOut(ctx, dir, "status", "--porcelain"); err == nil && st != "" {
		r.Changed = len(strings.Split(st, "\n"))
	}
	if last, err := gitOut(ctx, dir, "log", "-1", "--format=%ct%x00%s"); err == nil {
		if at, subject, ok := strings.Cut(last, "\x00"); ok {
			r.Last = subject
			if sec, err := strconv.ParseInt(at, 10, 64); err == nil {
				r.LastAt = sec * 1000
			}
		}
	}
	return r
}

type projectFileCandidate struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// projectCandidates lists files worth offering as context: agent and readme
// files at the root, then Markdown files under docs/ (one level).
func projectCandidates(dir string) []projectFileCandidate {
	out := []projectFileCandidate{}
	add := func(rel string) {
		if info, err := os.Stat(filepath.Join(dir, rel)); err == nil && info.Mode().IsRegular() && info.Size() <= maxProjectFileBytes {
			out = append(out, projectFileCandidate{Path: filepath.ToSlash(rel), Size: info.Size()})
		}
	}
	for _, name := range []string{"AGENTS.md", "CLAUDE.md", "GEMINI.md", "README.md", "CONTRIBUTING.md", "ARCHITECTURE.md"} {
		add(name)
	}
	if entries, err := os.ReadDir(filepath.Join(dir, "docs")); err == nil {
		names := []string{}
		for _, e := range entries {
			if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".md") {
				names = append(names, e.Name())
			}
		}
		sort.Strings(names)
		for i, n := range names {
			if i >= 30 {
				break
			}
			add(filepath.Join("docs", n))
		}
	}
	return out
}

// GET ?id=: the project's folder state (exists, Git repository, files that
// can be added as context).
func handleProjectInfo(w http.ResponseWriter, r *http.Request) {
	p, ok := getProject(r.URL.Query().Get("id"))
	if !ok {
		sendJSON(w, 404, map[string]any{"ok": false, "error": "projet introuvable"})
		return
	}
	out := map[string]any{"ok": true, "directory": p.Directory, "machine": p.Machine}
	if p.Machine != "" {
		// A remote folder is not read from here.
		sendJSON(w, 200, out)
		return
	}
	if p.Directory == "" {
		sendJSON(w, 200, out)
		return
	}
	if info, err := os.Stat(p.Directory); err != nil || !info.IsDir() {
		out["missing"] = true
		sendJSON(w, 200, out)
		return
	}
	out["repo"] = readProjectRepo(p.Directory)
	out["candidates"] = projectCandidates(p.Directory)
	sendJSON(w, 200, out)
}
