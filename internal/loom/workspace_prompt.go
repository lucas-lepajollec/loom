package loom

import (
	"errors"
	"reflect"
	"strings"
	"time"
)

func discussionContext(s RuntimeSession) DiscussionContext {
	c := DiscussionContext{MCPServers: s.MCPServers, ProjectID: s.ProjectID, Discussion: s.Instructions, Skills: []Capability{}}
	parts := []string{}
	if s.ProjectID != "" {
		p, ok := getProject(s.ProjectID)
		if !ok {
			c.Problem = "The project is missing or locked. Choose an accessible project or detach this thread."
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
					c.Warning = "A project skill is no longer available and will not be sent."
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
	s, ok := m.getLocked(id)
	if !ok {
		m.mu.Unlock()
		return s, errors.New("discussion not found or locked")
	}
	if m.runs[id] != nil || m.nativeRunning(s) {
		m.mu.Unlock()
		return s, errors.New("wait for or stop the response before editing this thread")
	}
	m.mu.Unlock()
	original := s
	currentContext := discussionContext(s)
	if revision == "" || revision != currentContext.Revision {
		return s, errors.New("the thread or its context changed; reopen the configuration before saving")
	}
	prospective := s
	prospective.ProjectID, prospective.Instructions = projectID, strings.TrimSpace(instructions)
	nextContext := currentContext
	if prospective.ProjectID != s.ProjectID || prospective.Instructions != s.Instructions {
		nextContext = discussionContext(prospective)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	current, ok := m.getLocked(id)
	if !ok || !reflect.DeepEqual(current, original) || m.runs[id] != nil || m.nativeRunning(s) {
		return s, errors.New("the thread changed while preparing; reopen the configuration before saving")
	}
	title, instructions = strings.TrimSpace(title), strings.TrimSpace(instructions)
	if title == "" || len([]rune(title)) > 100 || len(instructions) > maxDiscussionInstructions {
		return s, errors.New("title required (maximum 100 characters) and instruction up to 12000 bytes")
	}
	if projectID != "" {
		if _, ok := getProject(projectID); !ok {
			return s, errors.New("project not found or locked")
		}
	}
	if s.RuntimeID != "llama.cpp" && (s.ProjectID != projectID || s.Instructions != instructions) && !consent {
		return s, errors.New("confirm sharing the new context with the selected provider")
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
	m.publishLocked(id, DiscussionEvent{"session": cloneRuntimeSession(s), "context": turnContext(s, nextContext)})
	return s, nil
}
