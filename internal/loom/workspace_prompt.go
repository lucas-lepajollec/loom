package loom

import (
	"errors"
	"strings"
	"time"
)

func discussionContext(s RuntimeSession) DiscussionContext {
	workspaceMu.Lock()
	defer workspaceMu.Unlock()
	c := DiscussionContext{MCPServers: s.MCPServers, ProjectID: s.ProjectID, Discussion: s.Instructions, Skills: []Capability{}}
	parts := []string{}
	if s.ProjectID != "" {
		p, ok := getProject(s.ProjectID)
		if !ok {
			c.Problem = "Le projet est absent ou verrouillé. Choisissez un projet accessible ou détachez ce fil."
		} else {
			c.ProjectName, c.Instructions = p.Name, p.Instructions
			if c.MCPServers == nil {
				c.MCPServers = p.MCPServers
			}
			if p.Instructions != "" {
				parts = append(parts, "Project instructions:\n"+p.Instructions)
			}
			// Files the user chose in the project folder (explicit opt-in).
			files, warning := projectContextFiles(p)
			parts = append(parts, files...)
			if warning != "" {
				c.Warning = warning
			}
			// Brain passages relevant to the latest message.
			if text, cites := projectBrainContext(p, lastUserText(s.Messages)); text != "" {
				parts = append(parts, text)
				c.BrainCitations = cites
			}
			for _, id := range p.CapabilityIDs {
				if skill, ok := getCapability(id); ok && skill.Instructions != "" {
					c.Skills = append(c.Skills, skill)
					parts = append(parts, "Skill: "+skill.Name+"\n"+skill.Instructions)
				} else {
					c.Warning = "Un skill du projet n’est plus disponible et ne sera pas envoyé."
				}
			}
		}
	}
	if s.Instructions != "" {
		parts = append(parts, "Discussion instructions:\n"+s.Instructions)
	}
	c.System = strings.Join(parts, "\n\n")
	c.Revision = discussionContextRevision(s, c)
	return c
}

func (m *runtimeSessions) configureDiscussion(id, title, projectID, instructions, revision string, consent bool, harness ...acpConfiguration) (RuntimeSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.getLocked(id)
	if !ok {
		return s, errors.New("discussion introuvable ou verrouillée")
	}
	if m.runs[id] != nil || m.nativeRunning(s) {
		return s, errors.New("attendez ou arrêtez la réponse avant de modifier ce fil")
	}
	if revision == "" || revision != discussionContext(s).Revision {
		return s, errors.New("le fil ou son contexte a changé ; rouvrez la configuration avant d’enregistrer")
	}
	title, instructions = strings.TrimSpace(title), strings.TrimSpace(instructions)
	if title == "" || len([]rune(title)) > 100 || len(instructions) > maxDiscussionInstructions {
		return s, errors.New("titre requis (100 caractères maximum) et consigne de 12000 octets maximum")
	}
	if projectID != "" {
		if _, ok := getProject(projectID); !ok {
			return s, errors.New("projet introuvable ou verrouillé")
		}
	}
	if s.RuntimeID != "llama.cpp" && (s.ProjectID != projectID || s.Instructions != instructions) && !consent {
		return s, errors.New("confirmez le partage du nouveau contexte avec le provider sélectionné")
	}
	if len(harness) > 0 && harness[0].present() {
		if err := m.configureACPLocked(&s, harness[0], consent); err != nil {
			return s, err
		}
	}
	if s.ProjectID != projectID || s.Instructions != instructions {
		m.closeACP(id)
		s.NativeSessionID, s.NativeRuntimeID, s.NativeContext = "", "", ""
	}
	// Only an actual rename freezes the title; harness/context edits keep the
	// automatic title taken from the first message.
	if title != s.Title {
		s.CustomTitle = true
	}
	s.Title, s.ProjectID, s.Instructions = title, projectID, instructions
	s.UpdatedAt = time.Now().UnixMilli()
	if err := putStoreJSON(bkRuntimeSessions, id, s); err != nil {
		m.closeACP(id)
		return s, err
	}
	m.publishLocked(id, DiscussionEvent{"session": cloneRuntimeSession(s), "context": discussionContext(s)})
	return s, nil
}
