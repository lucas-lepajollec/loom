package loom

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
)

const maxCapabilityInstructions = 8000

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
	workspaceMu.Lock()
	defer workspaceMu.Unlock()
	c, ok := getCapability(id)
	if !ok {
		return fmt.Errorf("skill not found")
	}
	if c.ReadOnly {
		return errReadOnlySkill
	}
	root, err := openOwnedSkillsRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	return root.RemoveAll(filepath.Base(c.Dir))
}

func withProjectContext(messages []Message, context string) []Message {
	if context == "" {
		return messages
	}
	return normalizeSystemMessages(append([]Message{{Role: "system", Content: context}}, messages...))
}
