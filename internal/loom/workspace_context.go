package loom

import (
	"fmt"
	"github.com/lucas-lepajollec/loom/internal/loom/project"
	"os"
	"strings"
	"sync"
	"time"
)

const maxProjectInstructions = 12000
const maxCapabilityInstructions = 8000
const maxProjectCapabilities = 8

var workspaceMu sync.Mutex

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
		return c, fmt.Errorf("name and instructions required (maximum: name 160 bytes, description 500, instructions 8000)")
	}
	dir := ""
	if c.ID != "" {
		old, ok := getCapability(c.ID)
		if !ok {
			return c, fmt.Errorf("skill not found")
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
		return c, fmt.Errorf("skill unreadable after writing")
	}
	return saved, nil
}

// deleteCapability removes a skill of Loom's folder (linked ones are not Loom's).
func deleteCapability(id string) error {
	c, ok := getCapability(id)
	if !ok {
		return fmt.Errorf("skill not found")
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
		return p, fmt.Errorf("project not found")
	}
	if p.Continuity == nil {
		p.Continuity = old.Continuity
	}
	if p.Continuity != nil {
		if err := project.Validate(*p.Continuity); err != nil {
			return p, err
		}
		for _, ref := range p.Continuity.References {
			var session RuntimeSession
			var archive convArchive
			if !getStoreJSON(bkRuntimeSessions, ref.DiscussionID, &session) && !getStoreJSON(bkChatHist, ref.DiscussionID, &archive) {
				return p, fmt.Errorf("reference discussion not found or locked")
			}
		}
		if old.Continuity == nil || old.Continuity.WorkingState != p.Continuity.WorkingState {
			p.Continuity.StateUpdatedAt = time.Now().UTC()
		} else {
			p.Continuity.StateUpdatedAt = old.Continuity.StateUpdatedAt
		}
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
		return p, fmt.Errorf("name required (maximum 80 characters), context up to 12000 bytes and maximum 8 capabilities")
	}
	p.Machine = strings.TrimSpace(p.Machine)
	if p.Machine == "local" {
		p.Machine = ""
	}
	if p.Machine != "" && machineByID(p.Machine) == nil {
		return p, fmt.Errorf("unknown machine")
	}
	if p.Directory != "" {
		clean, err := projectDir(p.Machine, p.Directory)
		if err != nil {
			return p, err
		}
		p.Directory = clean
	}
	if len(p.ExtraDirs) > 8 {
		return p, fmt.Errorf("maximum 8 additional directories")
	}
	extra := []string{}
	for _, d := range p.ExtraDirs {
		if strings.TrimSpace(d) == "" {
			continue
		}
		clean, err := projectDir(p.Machine, d)
		if err != nil {
			return p, err
		}
		if clean != p.Directory && !hasName(extra, clean) {
			extra = append(extra, clean)
		}
	}
	p.ExtraDirs = extra
	if p.BrainBudget < 0 || p.BrainBudget > maxProjectBrainBudget || len(p.BrainSources) > 16 {
		return p, fmt.Errorf("Brain budget between 0 and 8000 tokens, maximum 16 sources")
	}
	if len(p.BrainSources) > 0 {
		kinds := brainSourceKinds()
		for _, id := range p.BrainSources {
			if _, ok := kinds[id]; !ok {
				return p, fmt.Errorf("unknown Brain source: %s", id)
			}
		}
	}
	if p.Machine != "" && len(p.ContextFiles) > 0 {
		return p, fmt.Errorf("context files are only read for a directory on this machine")
	}
	files, err := validProjectContextFiles(p.Directory, p.ContextFiles)
	if err != nil {
		return p, err
	}
	p.ContextFiles = files
	p.DefaultChoice = strings.TrimSpace(p.DefaultChoice)
	if len(p.DefaultChoice) > 400 {
		return p, fmt.Errorf("invalid default execution")
	}
	ids := []string{}
	seen := map[string]bool{}
	for _, id := range p.CapabilityIDs {
		if _, ok := getCapability(id); !ok {
			return p, fmt.Errorf("skill not found: refresh the list")
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
	global, _ := globalPreferences(false)
	p, ok := getProject(id)
	if !ok {
		return global
	}
	parts := []string{}
	if global != "" {
		parts = append(parts, global)
	}
	if text := project.Text(p.Continuity); text != "" {
		parts = append(parts, "Project identity: "+p.ID+"\n"+text)
	}
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
