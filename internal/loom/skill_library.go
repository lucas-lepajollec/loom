package loom

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Skills are folders in the Agent Skills format (<name>/SKILL.md plus any
// files), the format Claude Code, Codex, Pi and others read. Loom keeps its own
// skills in LOOM_HOME/skills, editable with any editor and versionable with
// Git, and can link folders the user already has (another tool's skills
// folder, a repository of skills). Linked folders are read-only: Loom lists
// and distributes their skills, never rewrites them.
//
// SKILL.md front matter written by Loom:
//
//	---
//	name: revue-de-code
//	description: "…"
//	metadata:
//	  title: Revue de code
//	  loom-id: 1790…   (kept for skills created before folders, so projects keep them)
//	---

const skillSourcesState = "skill_sources"

var skillLibMu sync.Mutex

func loomSkillsDir() string { return filepath.Join(LoomHome(), "skills") }

func linkedSkillSources() []SkillSource {
	list := []SkillSource{}
	_ = getStoreJSON(bkState, skillSourcesState, &list)
	return list
}

func skillSources() []SkillSource {
	out := []SkillSource{{ID: "loom", Path: loomSkillsDir(), Label: "Loom", Builtin: true, Writable: true}}
	return append(out, linkedSkillSources()...)
}

// migrateSkillsToFolders writes the skills stored in the database before
// folders existed into LOOM_HOME/skills (once, keeping their ids).
var skillMigrateOnce sync.Once

func migrateSkillsToFolders() {
	skillMigrateOnce.Do(func() {
		if getStr(bkState, "skills_migrated") == "1" {
			return
		}
		ok := true
		for id := range allKV(bkCapabilities) {
			var c Capability
			if !getStoreJSON(bkCapabilities, id, &c) || c.ID == "" {
				continue
			}
			if _, err := writeLoomSkill(c, ""); err != nil {
				ok = false
			}
		}
		if ok {
			_ = putStr(bkState, "skills_migrated", "1")
		}
	})
}

func getCapability(id string) (Capability, bool) {
	for _, c := range listCapabilities() {
		if c.ID == id {
			return c, true
		}
	}
	return Capability{}, false
}

// --- linked folders ---

func handleSkillSources(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		list := skillSources()
		skills := listCapabilities()
		for i := range list {
			if _, err := os.Stat(list[i].Path); err != nil && !list[i].Builtin {
				list[i].Error = "dossier introuvable"
			}
			for _, c := range skills {
				if c.Source == list[i].ID {
					list[i].Count++
				}
			}
		}
		sendJSON(w, 200, map[string]any{"ok": true, "sources": list, "suggested": suggestedSkillFolders(list)})
		return
	}
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		Path   string `json:"path"`
		Label  string `json:"label"`
		Unlink string `json:"unlink"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	skillLibMu.Lock()
	defer skillLibMu.Unlock()
	list := linkedSkillSources()
	if req.Unlink != "" {
		kept := []SkillSource{}
		for _, s := range list {
			if s.ID != req.Unlink {
				kept = append(kept, s)
			}
		}
		_ = putStoreJSON(bkState, skillSourcesState, kept)
		go syncSkillSinks()
		sendJSON(w, 200, map[string]any{"ok": true})
		return
	}
	path := filepath.Clean(strings.TrimSpace(req.Path))
	if !filepath.IsAbs(path) {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "chemin absolu requis"})
		return
	}
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "dossier introuvable"})
		return
	}
	if path == filepath.Clean(loomSkillsDir()) {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "c’est déjà le dossier de skills de Loom"})
		return
	}
	for _, s := range list {
		if s.Path == path {
			sendJSON(w, 409, map[string]any{"ok": false, "error": "dossier déjà lié"})
			return
		}
	}
	if len(list) >= 16 {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "16 dossiers liés maximum"})
		return
	}
	label := strings.TrimSpace(req.Label)
	if label == "" {
		label = home(path)
	}
	sum := sha256.Sum256([]byte(path))
	src := SkillSource{ID: "dir-" + hex.EncodeToString(sum[:4]), Path: path, Label: label}
	if err := putStoreJSON(bkState, skillSourcesState, append(list, src)); err != nil {
		sendJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	go syncSkillSinks()
	sendJSON(w, 200, map[string]any{"ok": true, "source": src})
}

func home(p string) string {
	if h, err := os.UserHomeDir(); err == nil && strings.HasPrefix(p, h) {
		return "~" + strings.TrimPrefix(p, h)
	}
	return p
}

// suggestedSkillFolders: skills folders of known tools that exist here and
// are not linked yet.
func suggestedSkillFolders(linked []SkillSource) []map[string]string {
	h, _ := os.UserHomeDir()
	cands := [][2]string{{filepath.Join(h, ".claude", "skills"), "Claude Code"}, {filepath.Join(h, ".agents", "skills"), "Agent Skills (Codex, Pi…)"},
		{filepath.Join(h, ".codex", "skills"), "Codex"}, {filepath.Join(h, ".gemini", "skills"), "Gemini"}}
	out := []map[string]string{}
	for _, c := range cands {
		if info, err := os.Stat(c[0]); err != nil || !info.IsDir() {
			continue
		}
		taken := false
		for _, s := range linked {
			taken = taken || s.Path == c[0]
		}
		if !taken {
			out = append(out, map[string]string{"path": c[0], "label": c[1]})
		}
	}
	return out
}
