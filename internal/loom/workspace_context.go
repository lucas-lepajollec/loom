package loom

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const bkCapabilities = "workspace_capabilities"
const maxProjectInstructions = 12000
const maxCapabilityInstructions = 8000
const maxProjectCapabilities = 8

var workspaceMu sync.Mutex

// Capability is user-authored instruction content, not a permission grant or
// executable tool. Selection is explicit per project, with no default injection.
type Capability struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	Instructions string `json:"instructions"`
}

func listCapabilities() []Capability {
	out := []Capability{}
	for id := range allKV(bkCapabilities) {
		var c Capability
		if getStoreJSON(bkCapabilities, id, &c) && c.ID != "" {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
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
	if c.ID == "" {
		c.ID = newSessionID()
	} else {
		var old Capability
		if !getStoreJSON(bkCapabilities, c.ID, &old) {
			return c, fmt.Errorf("capacité introuvable")
		}
	}
	return c, putStoreJSON(bkCapabilities, c.ID, c)
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
	ids := []string{}
	seen := map[string]bool{}
	for _, id := range p.CapabilityIDs {
		var c Capability
		if !getStoreJSON(bkCapabilities, id, &c) {
			return p, fmt.Errorf("capacité introuvable : actualise la liste")
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
		var c Capability
		if getStoreJSON(bkCapabilities, id, &c) && c.Instructions != "" {
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
