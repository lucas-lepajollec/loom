package loom

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const maxDiscussionInstructions = 12000
const maxPortableBytes = 128 << 10
const maxPortableMessages = 200

// DiscussionContext is a fresh read model, not a second memory store. Revision
// binds the route, portable history and instructions seen by the client.
type DiscussionContext struct {
	MCPServers   *[]string    `json:"mcp_servers,omitempty"`
	ProjectID    string       `json:"project_id"`
	ProjectName  string       `json:"project_name"`
	Instructions string       `json:"project_instructions"`
	Skills       []Capability `json:"skills"`
	Discussion   string       `json:"discussion_instructions"`
	System       string       `json:"system"`
	Revision     string       `json:"revision"`
	Warning      string       `json:"warning,omitempty"`
	Problem      string       `json:"problem,omitempty"`
}

type DiscussionPreview struct {
	Context      DiscussionContext `json:"context"`
	Messages     []Message         `json:"messages"`
	TextBytes    int               `json:"text_bytes"`
	HistoryCount int               `json:"history_count"`
	DraftAdded   bool              `json:"draft_added"`
	MaxBytes     int               `json:"max_bytes"`
	MaxMessages  int               `json:"max_messages"`
	Problem      string            `json:"problem,omitempty"`
}

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
			for _, id := range p.CapabilityIDs {
				var skill Capability
				if getStoreJSON(bkCapabilities, id, &skill) && skill.Instructions != "" {
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
	// No credentials, unchosen folder contents, global/local-only prompt or hidden state.
	encoded, _ := json.Marshal([]any{s.ID, s.Title, s.ProjectID, s.RuntimeID, s.ProviderID, s.Endpoint, s.Model, s.ReasoningEffort, s.Workdir, s.AdditionalDirs, s.Permission, s.Mode, s.ConfigOptions, c.MCPServers, s.Messages, c.System, c.Problem, c.Warning})
	digest := sha256.Sum256(encoded)
	c.Revision = hex.EncodeToString(digest[:])
	return c
}

// prepareDiscussion is shared by the read-only preview and execution. It never
// truncates, summarizes, calls a model or mutates the stored history.
func prepareDiscussion(s RuntimeSession, draft string) DiscussionPreview {
	c := discussionContext(s)
	p := DiscussionPreview{Context: c, Messages: []Message{}, MaxBytes: maxPortableBytes, MaxMessages: maxPortableMessages, Problem: c.Problem}
	if c.System != "" {
		p.Messages = append(p.Messages, Message{Role: "system", Content: c.System})
	}
	for _, msg := range s.Messages {
		content, ok := msg.Content.(string)
		if !ok || (msg.Role != "user" && msg.Role != "assistant") || len(msg.ToolCalls) > 0 || msg.ToolCallID != "" {
			p.Problem = "Ce fil contient un format non portable ; aucun envoi automatique."
			continue
		}
		if content != "" {
			p.Messages = append(p.Messages, Message{Role: msg.Role, Content: content})
			p.HistoryCount++
		}
	}
	draft = strings.TrimSpace(draft)
	if draft != "" {
		p.Messages = append(p.Messages, Message{Role: "user", Content: draft})
		p.DraftAdded = true
	}
	for _, msg := range p.Messages {
		p.TextBytes += len(msg.Content.(string))
	}
	if len(draft) > 24000 {
		p.Problem = "Message trop long (24000 octets maximum)."
	} else if p.TextBytes > maxPortableBytes || len(p.Messages) > maxPortableMessages {
		p.Problem = "Contexte trop long : 128 Kio de texte et 200 messages maximum, instructions comprises. Aucun texte n’a été tronqué."
	}
	return p
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
