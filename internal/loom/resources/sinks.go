package resources

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

type SkillSinkTarget struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Harnesses []string `json:"harnesses"`
	Dir       string   `json:"dir"`
	Enabled   bool     `json:"enabled"`
	Written   []string `json:"written"`
	Error     string   `json:"error,omitempty"`
}

func HasName(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// IsLinkInto: a link pointing at one of the known skills' folders.
func IsLinkInto(p string, skills []Capability) bool {
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

// SkillSinkCurrent: the placed link still points at the skill (copies are
// refreshed on every sync).
func SkillSinkCurrent(dest, src string) bool {
	if target, err := os.Readlink(dest); err == nil {
		return filepath.Clean(target) == filepath.Clean(src)
	}
	return false
}

// PlaceSkill links dest to the skill folder; where links are unavailable
// (Windows without developer mode) it copies the folder with a marker.
func PlaceSkill(src, dest string) error {
	if runtime.GOOS != "windows" {
		if err := os.Symlink(src, dest); err == nil {
			return nil
		}
	}
	if err := CopySkillDir(src, dest); err != nil {
		_ = os.RemoveAll(dest)
		return err
	}
	return os.WriteFile(filepath.Join(dest, ".loom-copy"), []byte("Copie gérée par Loom, remplacée à chaque synchronisation.\n"), 0o644)
}

func CopySkillDir(src, dest string) error {
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

// SyncSkillSinks applies an already resolved manifest; Loom owns locking and persistence.
func (lib Library) SyncSkillSinks(list []SkillSinkTarget, skills []Capability, bindings map[string]map[string]bool, displayPath func(string) string) []SkillSinkTarget {
	for i := range list {
		t := &list[i]
		t.Error = ""
		want := map[string]Capability{}
		if t.Enabled {
			for _, c := range skills {
				// A skill already living in this folder (linked source) is not re-added.
				if !SkillBound(bindings, c.ID, t.ID) || c.Dir == "" || filepath.Dir(c.Dir) == filepath.Clean(t.Dir) {
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
			return strings.HasPrefix(name, "loom-") || lib.IsLoomSkillLink(p) || IsLinkInto(p, skills)
		}
		kept := []string{}
		for _, name := range t.Written {
			if c, still := want[name]; still && SkillSinkCurrent(filepath.Join(t.Dir, name), c.Dir) {
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
			if HasName(kept, name) {
				continue
			}
			dest := filepath.Join(t.Dir, name)
			if _, err := os.Lstat(dest); err == nil {
				t.Error = "un dossier « " + name + " » existe déjà dans " + displayPath(t.Dir) + " : Loom ne le remplace pas"
				continue
			}
			if err := os.MkdirAll(t.Dir, 0o755); err != nil {
				t.Error = err.Error()
				continue
			}
			if err := PlaceSkill(want[name].Dir, dest); err != nil {
				t.Error = err.Error()
				continue
			}
			kept = append(kept, name)
		}
		t.Written = kept
	}
	return list
}
