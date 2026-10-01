package loom

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
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

type SkillSource struct {
	ID       string `json:"id"`
	Path     string `json:"path"`
	Label    string `json:"label"`
	Builtin  bool   `json:"builtin,omitempty"`
	Writable bool   `json:"writable"`
	Count    int    `json:"count"`
	Error    string `json:"error,omitempty"`
}

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

// parseSkillMarkdown splits front matter (a small YAML subset: key: value and
// one level of nested maps) from the body.
func parseSkillMarkdown(raw string) (map[string]string, string) {
	meta := map[string]string{}
	raw = strings.TrimPrefix(raw, "\uFEFF")
	if !strings.HasPrefix(raw, "---") {
		return meta, strings.TrimSpace(raw)
	}
	rest := raw[3:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return meta, strings.TrimSpace(raw)
	}
	head, body := rest[:end], rest[end+4:]
	parent := ""
	for _, line := range strings.Split(head, "\n") {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		indented := strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")
		k, v, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.Trim(strings.TrimSpace(v), `"'`)
		if !indented {
			parent = ""
			if v == "" {
				parent = k
				continue
			}
			meta[k] = v
		} else if parent != "" {
			meta[parent+"."+k] = v
		}
	}
	return meta, strings.TrimSpace(strings.TrimPrefix(body, "\n"))
}

func yamlQuote(s string) string {
	return "\"" + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", " ").Replace(s) + "\""
}

func skillFileContent(slug string, c Capability) string {
	var b strings.Builder
	fmt.Fprintf(&b, "---\nname: %s\ndescription: %s\nmetadata:\n  title: %s\n", slug, yamlQuote(strings.Join(strings.Fields(c.Description), " ")), yamlQuote(c.Name))
	if c.ID != "" && !strings.Contains(c.ID, ":") {
		fmt.Fprintf(&b, "  loom-id: %s\n", c.ID)
	}
	fmt.Fprintf(&b, "---\n\n%s\n", strings.TrimSpace(c.Instructions))
	return b.String()
}

// readSkillDir reads one skill folder of a source.
func readSkillDir(src SkillSource, dir string) (Capability, bool) {
	raw, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		return Capability{}, false
	}
	meta, body := parseSkillMarkdown(string(raw))
	base := filepath.Base(dir)
	c := Capability{ID: meta["metadata.loom-id"], Name: meta["metadata.title"], Description: meta["description"], Instructions: body,
		Source: src.ID, SourceLabel: src.Label, Dir: dir, ReadOnly: !src.Writable}
	if c.ID == "" {
		c.ID = src.ID + ":" + base
	}
	if c.Name == "" {
		c.Name = meta["name"]
	}
	if c.Name == "" {
		c.Name = base
	}
	if entries, err := os.ReadDir(dir); err == nil {
		c.Files = len(entries) - 1
	}
	return c, true
}

// scanSkills lists the skills of every source; a skill of Loom's own folder
// wins over a linked one with the same id.
func scanSkills() []Capability {
	out := []Capability{}
	seen := map[string]bool{}
	for _, src := range skillSources() {
		entries, err := os.ReadDir(src.Path)
		if err != nil {
			continue
		}
		for _, e := range entries {
			dir := filepath.Join(src.Path, e.Name())
			if info, err := os.Stat(dir); err != nil || !info.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			// Folders Loom itself distributed into this source are not new skills.
			if src.ID != "loom" && isLoomSkillLink(dir) {
				continue
			}
			c, ok := readSkillDir(src, dir)
			if !ok || seen[c.ID] {
				continue
			}
			seen[c.ID] = true
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

// isLoomSkillLink: a link or copy that Loom put in a harness folder.
func isLoomSkillLink(dir string) bool {
	if target, err := os.Readlink(dir); err == nil {
		return strings.HasPrefix(filepath.Clean(target), filepath.Clean(loomSkillsDir())+string(filepath.Separator))
	}
	_, err := os.Stat(filepath.Join(dir, ".loom-copy"))
	return err == nil
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

var skillSlugAllowed = func(r rune) rune {
	switch {
	case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		return r
	}
	return '-'
}

func skillDirSlug(name string) string {
	s := strings.Map(skillSlugAllowed, strings.ToLower(asciiFold(name)))
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	s = strings.Trim(s, "-")
	if len(s) > 48 {
		s = strings.Trim(s[:48], "-")
	}
	if s == "" {
		s = "skill"
	}
	return s
}

func asciiFold(s string) string {
	return strings.NewReplacer("é", "e", "è", "e", "ê", "e", "ë", "e", "à", "a", "â", "a", "ä", "a", "î", "i", "ï", "i",
		"ô", "o", "ö", "o", "ù", "u", "û", "u", "ü", "u", "ç", "c", "œ", "oe", "æ", "ae").Replace(s)
}

// writeLoomSkill creates or rewrites SKILL.md in Loom's folder; other files
// of the skill folder are left untouched. dir is the existing folder, if any.
func writeLoomSkill(c Capability, dir string) (string, error) {
	root := loomSkillsDir()
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	if c.ID == "" {
		c.ID = newSessionID() // stable even if the folder is renamed later
	}
	if dir == "" {
		slug := skillDirSlug(c.Name)
		dir = filepath.Join(root, slug)
		for i := 2; ; i++ {
			if _, err := os.Stat(dir); os.IsNotExist(err) {
				break
			}
			dir = filepath.Join(root, fmt.Sprintf("%s-%d", slug, i))
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
	}
	tmp := filepath.Join(dir, ".SKILL.md.loom-tmp")
	if err := os.WriteFile(tmp, []byte(skillFileContent(filepath.Base(dir), c)), 0o644); err != nil {
		return "", err
	}
	return dir, os.Rename(tmp, filepath.Join(dir, "SKILL.md"))
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

var errReadOnlySkill = errors.New("cette skill vient d’un dossier lié : modifie-la dans ce dossier, ou copie-la dans Loom")
