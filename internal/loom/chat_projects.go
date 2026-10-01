package loom

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// ChatProject owns shared context while preserving the original chat grouping.
type ChatProject struct {
	MCPServers    *[]string `json:"mcp_servers,omitempty"`
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	CreatedAt     int64     `json:"created_at"`
	Directory     string    `json:"directory,omitempty"`
	Instructions  string    `json:"instructions,omitempty"`
	CapabilityIDs []string  `json:"capability_ids,omitempty"`
	// ContextFiles: files of Directory added to the context (relative paths).
	ContextFiles []string `json:"context_files,omitempty"`
	// DefaultChoice: selector choice a new discussion of the project starts with.
	DefaultChoice string `json:"default_choice,omitempty"`
}

func listProjects() []ChatProject {
	raw := allKV(bkProjects)
	out := make([]ChatProject, 0, len(raw))
	for id := range raw {
		var p ChatProject
		if getStoreJSON(bkProjects, id, &p) && p.ID != "" {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt != out[j].CreatedAt {
			return out[i].CreatedAt > out[j].CreatedAt
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func getProject(id string) (ChatProject, bool) {
	var p ChatProject
	if !getStoreJSON(bkProjects, id, &p) || p.ID == "" {
		return ChatProject{}, false
	}
	return p, true
}

func createProject(name string) (ChatProject, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Nouveau projet"
	}
	if len([]rune(name)) > 80 {
		name = string([]rune(name)[:80])
	}
	p := ChatProject{ID: newSessionID(), Name: name, CreatedAt: time.Now().UnixMilli()}
	if err := putStoreJSON(bkProjects, p.ID, p); err != nil {
		return ChatProject{}, err
	}
	return p, nil
}

func renameProject(id, name string) error {
	workspaceMu.Lock()
	defer workspaceMu.Unlock()
	p, ok := getProject(id)
	if !ok {
		return fmt.Errorf("projet introuvable")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("nom vide")
	}
	if len([]rune(name)) > 80 {
		name = string([]rune(name)[:80])
	}
	p.Name = name
	return putStoreJSON(bkProjects, p.ID, p)
}

func deleteProject(id string) error {
	workspaceMu.Lock()
	defer workspaceMu.Unlock()
	if _, ok := getProject(id); !ok {
		return fmt.Errorf("projet introuvable")
	}
	for _, m := range listArchives() {
		if m.ProjectID == id {
			_ = setArchiveProject(m.ID, "")
		}
	}
	if conv.currentProject() == id {
		conv.setActiveProject("")
	}
	return putBytes(bkProjects, id, nil)
}

func setArchiveProject(id, projectID string) error {
	if projectID != "" {
		if _, ok := getProject(projectID); !ok {
			return fmt.Errorf("projet introuvable")
		}
	}
	a, ok := loadArchive(id)
	if !ok {
		if conv.currentID() == id {
			conv.setActiveProject(projectID)
			conv.upsertSession()
			return nil
		}
		return fmt.Errorf("conversation introuvable")
	}
	a.ProjectID = projectID
	if err := saveArchive(a); err != nil {
		return err
	}
	conv.setActiveProjectIfMatch(id, projectID)
	return nil
}
