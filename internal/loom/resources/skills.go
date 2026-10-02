package resources

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Capability is user-authored instruction content, not a permission grant or
// executable tool. Selection is explicit per project, with no default injection.
// Skills live in folders (skill_library.go): Loom's own, and linked ones.
type Capability struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	Instructions string `json:"instructions"`
	Source       string `json:"source,omitempty"`       // "loom" or a linked folder id
	SourceLabel  string `json:"source_label,omitempty"` // for display
	Dir          string `json:"dir,omitempty"`          // the skill's folder
	Files        int    `json:"files,omitempty"`        // other files next to SKILL.md
	ReadOnly     bool   `json:"read_only,omitempty"`    // from a linked folder
}

type SkillSource struct {
	ID       string `json:"id"`
	Path     string `json:"path"`
	Label    string `json:"label"`
	Builtin  bool   `json:"builtin,omitempty"`
	Writable bool   `json:"writable"`
	Count    int    `json:"count"`
	Error    string `json:"error,omitempty"`
}

var skillSlugAllowed = func(r rune) rune {
	switch {
	case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		return r
	}
	return '-'
}

var ErrReadOnlySkill = errors.New("cette skill vient d’un dossier lié : modifie-la dans ce dossier, ou copie-la dans Loom")

// ParseSkillMarkdown splits front matter (a small YAML subset: key: value and
// one level of nested maps) from the body.
func ParseSkillMarkdown(raw string) (map[string]string, string) {
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

func YAMLQuote(s string) string {
	return "\"" + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", " ").Replace(s) + "\""
}

func SkillFileContent(slug string, c Capability) string {
	var b strings.Builder
	fmt.Fprintf(&b, "---\nname: %s\ndescription: %s\nmetadata:\n  title: %s\n", slug, YAMLQuote(strings.Join(strings.Fields(c.Description), " ")), YAMLQuote(c.Name))
	if c.ID != "" && !strings.Contains(c.ID, ":") {
		fmt.Fprintf(&b, "  loom-id: %s\n", c.ID)
	}
	fmt.Fprintf(&b, "---\n\n%s\n", strings.TrimSpace(c.Instructions))
	return b.String()
}

// ReadSkillDir reads one skill folder of a source.
func ReadSkillDir(src SkillSource, dir string) (Capability, bool) {
	raw, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		return Capability{}, false
	}
	meta, body := ParseSkillMarkdown(string(raw))
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

func SkillDirSlug(name string) string {
	s := strings.Map(skillSlugAllowed, strings.ToLower(ASCIIFold(name)))
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

func ASCIIFold(s string) string {
	return strings.NewReplacer("é", "e", "è", "e", "ê", "e", "ë", "e", "à", "a", "â", "a", "ä", "a", "î", "i", "ï", "i",
		"ô", "o", "ö", "o", "ù", "u", "û", "u", "ü", "u", "ç", "c", "œ", "oe", "æ", "ae").Replace(s)
}

// Library receives the owned skills directory and ID generator from Loom.
type Library struct {
	Root  string
	NewID func() string
}

// isLoomSkillLink: a link or copy that Loom put in a harness folder.
func (lib Library) IsLoomSkillLink(dir string) bool {
	if target, err := os.Readlink(dir); err == nil {
		return strings.HasPrefix(filepath.Clean(target), filepath.Clean(lib.Root)+string(filepath.Separator))
	}
	_, err := os.Stat(filepath.Join(dir, ".loom-copy"))
	return err == nil
}

// scanSkills lists the skills of every source; a skill of Loom's own folder
// wins over a linked one with the same id.
func (lib Library) ScanSkills(sources []SkillSource) []Capability {
	out := []Capability{}
	seen := map[string]bool{}
	for _, src := range sources {
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
			if src.ID != "loom" && lib.IsLoomSkillLink(dir) {
				continue
			}
			c, ok := ReadSkillDir(src, dir)
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

// writeLoomSkill creates or rewrites SKILL.md in Loom's folder; other files
// of the skill folder are left untouched. dir is the existing folder, if any.
func (lib Library) WriteSkill(c Capability, dir string) (string, error) {
	root := lib.Root
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	if c.ID == "" {
		c.ID = lib.NewID() // stable even if the folder is renamed later
	}
	if dir == "" {
		slug := SkillDirSlug(c.Name)
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
	if err := os.WriteFile(tmp, []byte(SkillFileContent(filepath.Base(dir), c)), 0o644); err != nil {
		return "", err
	}
	return dir, os.Rename(tmp, filepath.Join(dir, "SKILL.md"))
}
