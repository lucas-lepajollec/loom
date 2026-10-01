package loom

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
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

var skillSlugRe = regexp.MustCompile(`[^a-z0-9]+`)

func skillSlug(c Capability) string {
	s := strings.Trim(skillSlugRe.ReplaceAllString(strings.ToLower(c.Name), "-"), "-")
	if s == "" {
		s = strings.Trim(skillSlugRe.ReplaceAllString(strings.ToLower(c.ID), "-"), "-")
	}
	if len(s) > 48 {
		s = strings.Trim(s[:48], "-")
	}
	return "loom-" + s
}

// SKILL.md with the Agent Skills front matter (name, description).
func skillMarkdown(c Capability, slug string) string {
	desc := strings.Join(strings.Fields(c.Description), " ")
	if desc == "" {
		desc = "Skill Loom : " + c.Name
	}
	return fmt.Sprintf("---\nname: %s\ndescription: %q\n---\n\n<!-- Écrit par Loom. Modifier la skill dans Loom › Ressources. -->\n\n# %s\n\n%s\n", slug, desc, c.Name, strings.TrimSpace(c.Instructions))
}

// syncSkillSinks makes every enabled target contain exactly Loom's skills and
// removes from disabled targets the folders Loom wrote. Errors are reported per
// target and never touch folders outside the manifest.
func syncSkillSinks() []skillSinkTarget {
	skillSinkMu.Lock()
	defer skillSinkMu.Unlock()
	list := loadSkillSinks()
	skills := listCapabilities()
	bindings := skillBindings()
	for i := range list {
		t := &list[i]
		want := map[string]Capability{}
		if t.Enabled {
			for _, c := range skills {
				if skillBound(bindings, c.ID, t.ID) {
					want[skillSlug(c)] = c
				}
			}
		}
		kept := []string{}
		for _, name := range t.Written {
			if _, still := want[name]; still || !strings.HasPrefix(name, "loom-") {
				continue
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
			dir := filepath.Join(t.Dir, name)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Error = err.Error()
				continue
			}
			if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skillMarkdown(want[name], name)), 0o644); err != nil {
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
