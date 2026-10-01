package loom

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const maxProjectInstructions = 12000
const maxCapabilityInstructions = 8000
const maxProjectCapabilities = 8

var workspaceMu sync.Mutex

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

func listCapabilities() []Capability {
	migrateSkillsToFolders()
	return scanSkills()
}

func saveCapability(c Capability) (Capability, error) {
	workspaceMu.Lock()
	defer workspaceMu.Unlock()
	c.Name = strings.TrimSpace(c.Name)
	c.Description = strings.TrimSpace(c.Description)
	c.Instructions = strings.TrimSpace(c.Instructions)
	if c.Name == "" || len(c.Name) > 160 || len(c.Description) > 500 || c.Instructions == "" || len(c.Instructions) > maxCapabilityInstructions {
		return c, fmt.Errorf("nom et instructions requis (nom : 160 octets, description : 500, instructions : 8000 maximum)")
	}
	dir := ""
	if c.ID != "" {
		old, ok := getCapability(c.ID)
		if !ok {
			return c, fmt.Errorf("skill introuvable")
		}
		if old.ReadOnly {
			return c, errReadOnlySkill
		}
		dir = old.Dir
	}
	dir, err := writeLoomSkill(c, dir)
	if err != nil {
		return c, err
	}
	saved, ok := readSkillDir(skillSources()[0], dir)
	if !ok {
		return c, fmt.Errorf("skill illisible après écriture")
	}
	return saved, nil
}

// deleteCapability removes a skill of Loom's folder (linked ones are not Loom's).
func deleteCapability(id string) error {
	c, ok := getCapability(id)
	if !ok {
		return fmt.Errorf("skill introuvable")
	}
	if c.ReadOnly {
		return errReadOnlySkill
	}
	return os.RemoveAll(c.Dir)
}

func saveProjectContext(p ChatProject) (ChatProject, error) {
	workspaceMu.Lock()
	defer workspaceMu.Unlock()
	old, ok := getProject(p.ID)
	if p.ID == "" {
		p.ID = newSessionID()
		old.CreatedAt = time.Now().UnixMilli()
	} else if !ok {
		return p, fmt.Errorf("projet introuvable")
	}
	if p.MCPServers == nil {
		p.MCPServers = old.MCPServers
	}
	if p.MCPServers != nil {
		if err := validateACPMCPSelection(*p.MCPServers); err != nil {
			return p, err
		}
	}
	p.Name = strings.TrimSpace(p.Name)
	p.Instructions = strings.TrimSpace(p.Instructions)
	p.Directory = strings.TrimSpace(p.Directory)
	if p.Name == "" || len([]rune(p.Name)) > 80 || len(p.Instructions) > maxProjectInstructions || len(p.CapabilityIDs) > maxProjectCapabilities {
		return p, fmt.Errorf("nom requis (80 caractères maximum), contexte de 12000 octets maximum et 8 capacités maximum")
	}
	if p.Directory != "" {
		if !filepath.IsAbs(p.Directory) {
			return p, fmt.Errorf("le dossier doit être un chemin absolu")
		}
		info, err := os.Stat(p.Directory)
		if err != nil || !info.IsDir() {
			return p, fmt.Errorf("ce dossier n’existe pas ou n’est pas accessible")
		}
		p.Directory = filepath.Clean(p.Directory)
	}
	files, err := validProjectContextFiles(p.Directory, p.ContextFiles)
	if err != nil {
		return p, err
	}
	p.ContextFiles = files
	p.DefaultChoice = strings.TrimSpace(p.DefaultChoice)
	if len(p.DefaultChoice) > 400 {
		return p, fmt.Errorf("exécution par défaut invalide")
	}
	ids := []string{}
	seen := map[string]bool{}
	for _, id := range p.CapabilityIDs {
		if _, ok := getCapability(id); !ok {
			return p, fmt.Errorf("skill introuvable : actualise la liste")
		}
		if !seen[id] {
			ids = append(ids, id)
			seen[id] = true
		}
	}
	p.CapabilityIDs = ids
	p.CreatedAt = old.CreatedAt
	return p, putStoreJSON(bkProjects, p.ID, p)
}

// projectContext is evaluated once for the turn. Neither directory contents nor
// unrelated skills are read. Paths are metadata for future harness/terminal
// adapters, never an implicit filesystem permission or model prompt.
func projectContext(id string) string {
	p, ok := getProject(id)
	if !ok {
		return ""
	}
	parts := []string{}
	if p.Instructions != "" {
		parts = append(parts, "Project instructions:\n"+p.Instructions)
	}
	for _, id := range p.CapabilityIDs {
		if c, ok := getCapability(id); ok && c.Instructions != "" {
			parts = append(parts, "Skill: "+c.Name+"\n"+c.Instructions)
		}
	}
	return strings.Join(parts, "\n\n")
}

func withProjectContext(messages []Message, context string) []Message {
	if context == "" {
		return messages
	}
	return normalizeSystemMessages(append([]Message{{Role: "system", Content: context}}, messages...))
}
