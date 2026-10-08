package loom

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lucas-lepajollec/loom/internal/loom/brain"
)

const skillsHomeState = "skills_home"

type skillsHomeSetting struct {
	Mode     string `json:"mode"`
	Relative string `json:"relative"`
}
type detectedSkillsHome struct {
	Relative string `json:"relative"`
	Count    int    `json:"count"`
}
type skillsHomeStatus struct {
	Mode        string               `json:"mode"`
	Dir         string               `json:"dir"`
	Relative    string               `json:"relative"`
	BrainSource string               `json:"brain_source"`
	Fallback    bool                 `json:"fallback"`
	Reason      string               `json:"reason"`
	Count       int                  `json:"count"`
	Detected    []detectedSkillsHome `json:"detected"`
}

func skillsHomeConfig() skillsHomeSetting {
	c := skillsHomeSetting{Mode: "loom", Relative: "skills"}
	_ = getStoreJSON(bkState, skillsHomeState, &c)
	return c
}

func skillsHomeRelative(relative string) (string, error) {
	if relative == "" {
		relative = "skills"
	}
	if !filepath.IsLocal(relative) || strings.Contains(relative, "\\") {
		return "", errors.New("use a relative folder inside the primary brain")
	}
	for _, part := range strings.Split(filepath.ToSlash(relative), "/") {
		if part == ".." {
			return "", errors.New("parent path components are not allowed")
		}
	}
	relative = filepath.Clean(relative)
	if relative == "." || relative == ".loom" || strings.HasPrefix(relative, ".loom"+string(filepath.Separator)) {
		return "", errors.New("choose a skills folder outside the brain root and .loom")
	}
	return relative, nil
}

func skillsPrimary(storage brainStorage) (brain.Source, error) {
	if err := brainAvailable(); err != nil {
		return brain.Source{}, err
	}
	sources, err := storage.LoadSources()
	if err != nil {
		return brain.Source{}, err
	}
	for _, s := range sources {
		if s.Primary && !s.ReadOnly && s.Permission == "write" {
			return s, nil
		}
	}
	return brain.Source{}, errors.New("no writable primary brain is configured")
}

func resolveSkillsHome(storage brainStorage) skillsHomeStatus {
	c := skillsHomeConfig()
	out := skillsHomeStatus{Mode: c.Mode, Relative: filepath.ToSlash(c.Relative), Dir: filepath.Join(filepath.Dir(storage.dir), "skills"), Detected: []detectedSkillsHome{}}
	if c.Mode != "brain" {
		return out
	}
	fail := func(err error) skillsHomeStatus { out.Fallback, out.Reason = true, err.Error(); return out }
	s, err := skillsPrimary(storage)
	if err != nil {
		return fail(err)
	}
	out.BrainSource = s.ID
	rel, err := skillsHomeRelative(c.Relative)
	if err != nil {
		return fail(err)
	}
	root, err := os.OpenRoot(s.Path)
	if err != nil {
		return fail(fmt.Errorf("primary brain unavailable: %w", err))
	}
	defer root.Close()
	folder, err := root.OpenRoot(rel)
	if err != nil {
		return fail(fmt.Errorf("skills folder unavailable: %w", err))
	}
	folder.Close()
	out.Dir = filepath.Join(s.Path, rel)
	return out
}

func loomSkillsDir() string { return resolveSkillsHome(theBrain().storage).Dir }

func countSkills(root *os.Root, relative string) int {
	dir, err := root.Open(relative)
	if err != nil {
		return 0
	}
	defer dir.Close()
	count := 0
	for {
		entries, err := dir.ReadDir(128)
		for _, d := range entries {
			if !d.IsDir() || strings.HasPrefix(d.Name(), ".") {
				continue
			}
			switch d.Name() {
			case ".git", "node_modules", ".loom", ".obsidian":
				continue
			}
			info, err := root.Stat(filepath.Join(relative, d.Name(), "SKILL.md"))
			if err == nil && info.Mode().IsRegular() {
				count++
			}
		}
		if err != nil {
			return count
		}
	}
}

func detectSkillsHomes(root *os.Root) []detectedSkillsHome {
	out := []detectedSkillsHome{}
	var walk func(string, int)
	walk = func(rel string, depth int) {
		if count := countSkills(root, rel); count > 0 {
			out = append(out, detectedSkillsHome{filepath.ToSlash(rel), count})
		}
		if depth == 3 {
			return
		}
		dir, err := root.Open(rel)
		if err != nil {
			return
		}
		defer dir.Close()
		for {
			entries, err := dir.ReadDir(128)
			for _, d := range entries {
				if !d.IsDir() {
					continue
				}
				switch d.Name() {
				case ".git", "node_modules", ".loom", ".obsidian":
					continue
				}
				walk(filepath.Join(rel, d.Name()), depth+1)
			}
			if err != nil {
				return
			}
		}
	}
	walk(".", 0)
	sort.Slice(out, func(i, j int) bool { return out[i].Relative < out[j].Relative })
	return out
}

func skillsHomeInfo(storage brainStorage) skillsHomeStatus {
	out := resolveSkillsHome(storage)
	if root, err := os.OpenRoot(out.Dir); err == nil {
		out.Count = countSkills(root, ".")
		root.Close()
	}
	if s, err := skillsPrimary(storage); err == nil {
		out.BrainSource = s.ID
		if root, err := os.OpenRoot(s.Path); err == nil {
			out.Detected = detectSkillsHomes(root)
			root.Close()
		}
	}
	return out
}

func copySkillsHome(previous string, target *os.Root) ([]string, []string, error) {
	copied, conflicts := []string{}, []string{}
	source, err := os.OpenRoot(previous)
	if os.IsNotExist(err) {
		return copied, conflicts, nil
	}
	if err != nil {
		return copied, conflicts, err
	}
	defer source.Close()
	entries, err := fs.ReadDir(source.FS(), ".")
	if err != nil {
		return copied, conflicts, err
	}
	for _, d := range entries {
		name := d.Name()
		if !d.IsDir() || strings.HasPrefix(name, ".") {
			continue
		}
		info, err := source.Stat(filepath.Join(name, "SKILL.md"))
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		if _, err = target.Lstat(name); err == nil {
			conflicts = append(conflicts, name)
			continue
		}
		if !os.IsNotExist(err) {
			return copied, conflicts, err
		}
		if err = target.Mkdir(name, 0o755); err != nil {
			return copied, conflicts, err
		}
		err = fs.WalkDir(source.FS(), name, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if brain.IsCredentialFile(entry.Name()) {
				return errors.New("refusing to copy credential files into the brain")
			}
			if entry.IsDir() {
				switch entry.Name() {
				case ".git", ".loom", ".ssh", ".gnupg", ".aws", ".codex", ".claude":
					return fs.SkipDir
				}
				return target.MkdirAll(path, 0o755)
			}
			if !entry.Type().IsRegular() {
				return errors.New("skill copy requires regular files and directories")
			}
			in, err := source.Open(path)
			if err != nil {
				return err
			}
			defer in.Close()
			info, err := in.Stat()
			if err != nil || !info.Mode().IsRegular() {
				return errors.New("skill copy requires a regular file")
			}
			out, err := target.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
			if err != nil {
				return err
			}
			_, err = io.Copy(out, in)
			if closeErr := out.Close(); err == nil {
				err = closeErr
			}
			return err
		})
		if err != nil {
			_ = target.RemoveAll(name)
			return copied, conflicts, err
		}
		copied = append(copied, name)
	}
	return copied, conflicts, nil
}

func handleSkillsHome(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodGet {
		sendJSON(w, 200, skillsHomeInfo(theBrain().storage))
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		sendJSON(w, 405, map[string]any{"error": "method not allowed"})
		return
	}
	var req skillsHomeSetting
	if !workspaceDecode(w, r, &req) {
		return
	}
	if req.Mode != "loom" && req.Mode != "brain" {
		brainResponse(w, nil, errors.New("mode must be loom or brain"))
		return
	}
	relative, err := skillsHomeRelative(req.Relative)
	if err != nil {
		brainResponse(w, nil, err)
		return
	}
	req.Relative = relative
	skillLibMu.Lock()
	defer skillLibMu.Unlock()
	workspaceMu.Lock()
	defer workspaceMu.Unlock()
	copied, conflicts := []string{}, []string{}
	if req.Mode == "brain" {
		s, err := skillsPrimary(theBrain().storage)
		if err != nil {
			brainResponse(w, nil, err)
			return
		}
		root, err := os.OpenRoot(s.Path)
		if err != nil {
			brainResponse(w, nil, err)
			return
		}
		defer root.Close()
		migrateSkillsToFolders()
		previous := loomSkillsDir()
		if err = skillsHomeOverlap(previous, filepath.Join(s.Path, relative)); err != nil {
			brainResponse(w, nil, err)
			return
		}
		if err = root.MkdirAll(relative, 0o755); err != nil {
			brainResponse(w, nil, err)
			return
		}
		target, err := root.OpenRoot(relative)
		if err != nil {
			brainResponse(w, nil, err)
			return
		}
		if !sameSkillsHome(previous, filepath.Join(s.Path, relative)) {
			copied, conflicts, err = copySkillsHome(previous, target)
		}
		target.Close()
		if err != nil {
			brainResponse(w, nil, err)
			return
		}
	}
	// Upgrade older name-only manifests while their original links are known.
	syncSkillSinks()
	if err = putStoreJSON(bkState, skillsHomeState, req); err != nil {
		brainResponse(w, nil, err)
		return
	}
	syncSkillSinksAsync()
	if e, err := theBrain().get(); err == nil {
		_ = e.Refresh(r.Context())
	}
	sendJSON(w, 200, struct {
		skillsHomeStatus
		Copied    []string `json:"copied"`
		Conflicts []string `json:"conflicts"`
	}{skillsHomeInfo(theBrain().storage), copied, conflicts})
}

func (s *brainService) skillsIndexExclusions() []string {
	home := resolveSkillsHome(s.storage)
	dir, err := filepath.EvalSymlinks(home.Dir)
	if err != nil {
		return nil
	}
	return []string{dir}
}

func openOwnedSkillsRoot() (*os.Root, error) { return openSkillsRoot(true) }

func openSkillsRoot(create bool) (*os.Root, error) {
	storage := theBrain().storage
	home := resolveSkillsHome(storage)
	if home.Mode == "brain" && !home.Fallback {
		source, err := skillsPrimary(storage)
		if err != nil {
			return nil, err
		}
		root, err := os.OpenRoot(source.Path)
		if err != nil {
			return nil, err
		}
		defer root.Close()
		return root.OpenRoot(filepath.FromSlash(home.Relative))
	}
	if create {
		if err := os.MkdirAll(home.Dir, 0o755); err != nil {
			return nil, err
		}
	}
	return os.OpenRoot(home.Dir)
}

func canonicalSkillsHome(dir string) string {
	dir = filepath.Clean(dir)
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		return resolved
	}
	parent := filepath.Dir(dir)
	if parent == dir {
		return dir
	}
	return filepath.Join(canonicalSkillsHome(parent), filepath.Base(dir))
}
func sameSkillsHome(a, b string) bool { return canonicalSkillsHome(a) == canonicalSkillsHome(b) }
func skillsHomeOverlap(a, b string) error {
	a, b = canonicalSkillsHome(a), canonicalSkillsHome(b)
	if a == b {
		return nil
	}
	for _, pair := range [][2]string{{a, b}, {b, a}} {
		if rel, err := filepath.Rel(pair[0], pair[1]); err == nil && filepath.IsLocal(rel) {
			return errors.New("skills homes must not overlap")
		}
	}
	return nil
}
