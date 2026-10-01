package loom

import (
	"net/http"
	"os"
	"path/filepath"

	"runtime"
	"sort"
	"strings"
	"sync"
)

// Skill distribution: Loom owns the skills and writes them, on explicit opt-in,
// into the native skills folder of each harness family. Loom only ever touches
// folders named loom-<slug> that its manifest says it wrote.

type skillSinkTarget struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Harnesses []string `json:"harnesses"`
	Dir       string   `json:"dir"`
	Enabled   bool     `json:"enabled"`
	Written   []string `json:"written"`
	Error     string   `json:"error,omitempty"`
}

const skillSinkState = "skill_sinks"

var skillSinkMu sync.Mutex

// Folder conventions: Claude Code reads ~/.claude/skills; Codex, Pi and other
// Agent Skills readers use the shared ~/.agents/skills.
func skillSinkDefs() []skillSinkTarget {
	home, _ := os.UserHomeDir()
	return []skillSinkTarget{
		{ID: "claude", Name: "Claude Code", Harnesses: []string{"claude-code"}, Dir: filepath.Join(home, ".claude", "skills")},
		{ID: "agents", Name: "Agent Skills", Harnesses: []string{"codex", "pi", "gemini"}, Dir: filepath.Join(home, ".agents", "skills")},
	}
}

func loadSkillSinks() []skillSinkTarget {
	saved := map[string]skillSinkTarget{}
	_ = getStoreJSON(bkState, skillSinkState, &saved)
	out := skillSinkDefs()
	for i := range out {
		if s, ok := saved[out[i].ID]; ok {
			out[i].Enabled, out[i].Written = s.Enabled, s.Written
		}
	}
	return out
}

func saveSkillSinks(list []skillSinkTarget) error {
	m := map[string]skillSinkTarget{}
	for _, t := range list {
		m[t.ID] = skillSinkTarget{ID: t.ID, Enabled: t.Enabled, Written: t.Written}
	}
	return putStoreJSON(bkState, skillSinkState, m)
}

// syncSkillSinks makes every enabled target contain Loom's bound skills, as
// links to their folders (a marked copy on Windows), and removes what Loom put
// there before and no longer distributes. Loom never touches a folder it did
// not create: legacy generated loom-* folders, its links, its marked copies.
func syncSkillSinks() []skillSinkTarget {
	skillSinkMu.Lock()
	defer skillSinkMu.Unlock()
	list := loadSkillSinks()
	skills := listCapabilities()
	bindings := skillBindings()
	for i := range list {
		t := &list[i]
		t.Error = ""
		want := map[string]Capability{}
		if t.Enabled {
			for _, c := range skills {
				// A skill already living in this folder (linked source) is not re-added.
				if !skillBound(bindings, c.ID, t.ID) || c.Dir == "" || filepath.Dir(c.Dir) == filepath.Clean(t.Dir) {
					continue
				}
				name := filepath.Base(c.Dir)
				if _, dup := want[name]; !dup {
					want[name] = c
				}
			}
		}
		owned := func(name string) bool {
			p := filepath.Join(t.Dir, name)
			return strings.HasPrefix(name, "loom-") || isLoomSkillLink(p) || isLinkInto(p, skills)
		}
		kept := []string{}
		for _, name := range t.Written {
			if c, still := want[name]; still && skillSinkCurrent(filepath.Join(t.Dir, name), c.Dir) {
				kept = append(kept, name)
				continue
			}
			if !owned(name) {
				continue // replaced by the user: not ours anymore
			}
			if err := os.RemoveAll(filepath.Join(t.Dir, name)); err != nil {
				t.Error = err.Error()
				kept = append(kept, name)
			}
		}
		names := make([]string, 0, len(want))
		for name := range want {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if hasName(kept, name) {
				continue
			}
			dest := filepath.Join(t.Dir, name)
			if _, err := os.Lstat(dest); err == nil {
				t.Error = "un dossier « " + name + " » existe déjà dans " + home(t.Dir) + " : Loom ne le remplace pas"
				continue
			}
			if err := os.MkdirAll(t.Dir, 0o755); err != nil {
				t.Error = err.Error()
				continue
			}
			if err := placeSkill(want[name].Dir, dest); err != nil {
				t.Error = err.Error()
				continue
			}
			kept = append(kept, name)
		}
		t.Written = kept
	}
	_ = saveSkillSinks(list)
	return list
}

func hasName(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// isLinkInto: a link pointing at one of the known skills' folders.
func isLinkInto(p string, skills []Capability) bool {
	target, err := os.Readlink(p)
	if err != nil {
		return false
	}
	for _, c := range skills {
		if filepath.Clean(target) == filepath.Clean(c.Dir) {
			return true
		}
	}
	return false
}

// skillSinkCurrent: the placed link still points at the skill (copies are
// refreshed on every sync).
func skillSinkCurrent(dest, src string) bool {
	if target, err := os.Readlink(dest); err == nil {
		return filepath.Clean(target) == filepath.Clean(src)
	}
	return false
}

// placeSkill links dest to the skill folder; where links are unavailable
// (Windows without developer mode) it copies the folder with a marker.
func placeSkill(src, dest string) error {
	if runtime.GOOS != "windows" {
		if err := os.Symlink(src, dest); err == nil {
			return nil
		}
	}
	if err := copySkillDir(src, dest); err != nil {
		_ = os.RemoveAll(dest)
		return err
	}
	return os.WriteFile(filepath.Join(dest, ".loom-copy"), []byte("Copie gérée par Loom, remplacée à chaque synchronisation.\n"), 0o644)
}

func copySkillDir(src, dest string) error {
	return filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		out := filepath.Join(dest, rel)
		if d.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(out, b, 0o644)
	})
}

// GET: targets with state. POST {id, enabled}: toggle one target and sync.
func handleSkillSinks(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		sendJSON(w, 200, map[string]any{"ok": true, "targets": loadSkillSinks(), "bindings": skillBindings()})
		return
	}
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		ID      string `json:"id"`
		Enabled bool   `json:"enabled"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	list := loadSkillSinks()
	found := false
	for i := range list {
		if list[i].ID == req.ID {
			list[i].Enabled, found = req.Enabled, true
		}
	}
	if !found {
		sendJSON(w, 404, map[string]any{"ok": false, "error": "cible inconnue"})
		return
	}
	skillSinkMu.Lock()
	err := saveSkillSinks(list)
	skillSinkMu.Unlock()
	if err != nil {
		sendJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "targets": syncSkillSinks()})
}
