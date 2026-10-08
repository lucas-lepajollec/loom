package loom

import (
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/brain"
	"github.com/lucas-lepajollec/loom/internal/loom/discussion"
	"github.com/lucas-lepajollec/loom/internal/loom/project"
)

type contextPart struct {
	text      string
	separator string
	item      discussion.ContextItem
}

func discussionContext(s RuntimeSession) DiscussionContext {
	return discussionContextFor(s, lastUserText(s.Messages))
}

// appendContextPart attributes headers, separators and rounding to the next
// included item, so item and per-kind costs sum to EstimatedTokens exactly.
func appendContextPart(c *DiscussionContext, part contextPart) {
	before := brain.Tokens(c.System)
	if c.System != "" {
		c.System += part.separator
	}
	c.System += part.text
	part.item.Tokens = brain.Tokens(c.System) - before
	c.Items = append(c.Items, part.item)
	c.Budget.ByKind[part.item.Kind] += part.item.Tokens
}

func discussionContextFor(s RuntimeSession, query string) DiscussionContext {
	c := DiscussionContext{MCPServers: s.MCPServers, ProjectID: s.ProjectID, Discussion: s.Instructions, Skills: []Capability{}, Items: []discussion.ContextItem{}}
	c.Budget.ByKind = map[string]int{}
	add := func(kind, label, source, reason, text string) {
		appendContextPart(&c, contextPart{text: text, separator: "\n\n", item: discussion.ContextItem{Kind: kind, Label: label, Source: source, Reason: reason}})
	}
	if text, err := globalPreferences(s.RuntimeID != "llama.cpp"); err != nil {
		c.Problem = err.Error()
	} else if text != "" {
		var selected sharedPreferences
		getStoreJSON(bkState, sharedPreferencesKey, &selected)
		add("global_preferences", selected.Page, selected.Page, "selected shared preferences", text)
		c.GlobalPreferences = text
	}
	passageText := ""
	skillParts := []contextPart{}
	if s.ProjectID != "" {
		p, ok := getProject(s.ProjectID)
		if !ok {
			c.Problem = "The project is missing or locked. Choose an accessible project or detach this thread."
		} else {
			if text := project.Text(p.Continuity); text != "" {
				text = "Project identity: " + p.ID + "\n" + text
				add("project", p.Name, p.ID, "project continuity", "Project continuity:\n"+text)
				c.Minimum = text
			}
			if p.Continuity != nil {
				for _, ref := range p.Continuity.References {
					c.ReferenceIDs = append(c.ReferenceIDs, ref.DiscussionID)
				}
			}
			c.ProjectName, c.Instructions = p.Name, p.Instructions
			if c.MCPServers == nil {
				c.MCPServers = p.MCPServers
			}
			if p.Instructions != "" {
				add("project", p.Name, p.ID, "project instructions", "Project instructions:\n"+p.Instructions)
			}
			files, warning := projectContextFiles(p)
			for _, text := range files {
				path, _, _ := strings.Cut(strings.TrimPrefix(text, "Project file "), ":\n")
				add("project_files", path, path, "selected project file", text)
			}
			if warning != "" {
				c.Warning = warning
			}
			pack := projectBrainPack(p, query)
			passageText = pack.Text
			for i, chunk := range pack.Chunks {
				cite := pack.Citations[i]
				text := "\n[" + cite.Source + ": " + cite.Citation + "]\n" + chunk.Text + "\n"
				separator := ""
				if i == 0 {
					text = "Context from the user's Brain:\n" + text
					separator = "\n\n"
				}
				appendContextPart(&c, contextPart{text: text, separator: separator, item: discussion.ContextItem{Kind: "brain_passage", Label: cite.Citation, Source: cite.Source, Reason: "matches your message"}})
				c.BrainCitations = append(c.BrainCitations, cite.Citation)
			}
			for _, id := range p.CapabilityIDs {
				if skill, ok := getCapability(id); ok && skill.Instructions != "" {
					c.Skills = append(c.Skills, skill)
					skillParts = append(skillParts, contextPart{text: "Skill: " + skill.Name + "\n" + skill.Instructions, separator: "\n\n", item: discussion.ContextItem{Kind: "skill", Label: skill.Name, Source: skill.ID, Reason: "selected project skill"}})
				} else {
					c.Warning = "A project skill is no longer available and will not be sent."
				}
			}
		}
	}
	budgets := brain.DefaultMemoryBudgets()
	c.Budget.Memory = discussion.MemoryBudget{TokenBudget: discussion.TokenBudget{Available: budgets.Total}, Classes: map[string]discussion.TokenBudget{}}
	for class, limit := range budgets.Classes() {
		c.Budget.Memory.Classes[class] = discussion.TokenBudget{Available: limit}
	}
	scopes := []string{"global"}
	if s.ID != "" {
		scopes = append(scopes, "task:"+s.ID)
	}
	if s.ContinuedFrom != "" {
		scopes = append(scopes, "task:"+s.ContinuedFrom)
	}
	if s.ProjectID != "" {
		scopes = append(scopes, "project:"+s.ProjectID)
	}
	if s.RuntimeID != "" {
		scopes = append(scopes, "agent:"+s.RuntimeID)
	}
	if list, err := theBrain().ListMemory(brain.MemoryFilter{Scopes: scopes, Status: "all"}); err != nil {
		if c.Problem == "" {
			c.Problem = "Loom memory is unavailable: " + err.Error()
		}
	} else {
		pack := brain.SelectMemory(list.Items, s.ProjectID, s.RuntimeID, query, passageText, budgets, s.ID, s.ContinuedFrom)
		c.Budget.Memory.Used = brain.Tokens(pack.Text)
		for class, used := range pack.Used {
			limit := c.Budget.Memory.Classes[class]
			limit.Used = used
			c.Budget.Memory.Classes[class] = limit
		}
		for i, selected := range pack.Items {
			item := selected.Item
			words := strings.Fields(item.Text)
			label := strings.Join(words[:min(len(words), 8)], " ")
			separator := ""
			if i == 0 {
				separator = "\n\n"
			}
			appendContextPart(&c, contextPart{text: selected.Section, separator: separator, item: discussion.ContextItem{Kind: "memory", Label: label, Source: item.ID, Class: item.Class, Scope: item.Scope, Reason: selected.Reason}})
		}
	}
	for _, part := range skillParts {
		appendContextPart(&c, part)
	}
	for _, part := range primarySecondBrainParts() {
		appendContextPart(&c, part)
	}
	if text := memoryProtocol(s); text != "" && c.Problem == "" {
		add("memory_protocol", "Loom memory", s.ID, "how to keep memory current", text)
	}
	if s.Instructions != "" {
		add("discussion_instructions", "Discussion instructions", s.ID, "discussion instructions", "Discussion instructions:\n"+s.Instructions)
	}
	c.EstimatedTokens = brain.Tokens(c.System)
	c.Revision = discussionContextRevision(s, c)
	return c
}

// Usage is best-effort bookkeeping on an accepted execution, never preparation.
// A write failure must not turn a successfully accepted message into a resend.
func touchContextMemory(c DiscussionContext) {
	ids := []string{}
	for _, item := range c.Items {
		if item.Kind == "memory" {
			ids = append(ids, item.Source)
		}
	}
	if len(ids) > 0 {
		_ = theBrain().TouchMemory(ids)
	}
}

func (m *runtimeSessions) configureDiscussion(id, title, projectID, instructions, revision string, consent bool, harness ...acpConfiguration) (RuntimeSession, error) {
	m.mu.Lock()
	s, ok := m.getLocked(id)
	if !ok {
		m.mu.Unlock()
		return s, errors.New("discussion not found or locked")
	}
	if m.preparing[id] || m.runs[id] != nil || m.nativeRunning(s) {
		m.mu.Unlock()
		return s, errors.New("wait for or stop the response before editing this thread")
	}
	m.preparing[id] = true
	m.mu.Unlock()
	defer func() { m.mu.Lock(); delete(m.preparing, id); m.mu.Unlock() }()
	original := s
	s = cloneRuntimeSession(s)
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
	m.mu.Lock()
	defer m.mu.Unlock()
	current, ok := m.getLocked(id)
	if !ok || !reflect.DeepEqual(current, original) || m.runs[id] != nil || m.nativeRunning(current) {
		m.closeACP(id)
		return s, errors.New("the discussion changed during configuration; try again")
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
	// ACP configuration can change the MCP selection without changing text.
	// Publish that selection with the prepared context, without another retrieval.
	nextContext.MCPServers = s.MCPServers
	if nextContext.MCPServers == nil && projectID != "" {
		if p, ok := getProject(projectID); ok {
			nextContext.MCPServers = p.MCPServers
		}
	}
	s.UpdatedAt = time.Now().UnixMilli()
	if err := putStoreJSON(bkRuntimeSessions, id, s); err != nil {
		m.closeACP(id)
		return s, err
	}
	m.publishLocked(id, DiscussionEvent{"session": cloneRuntimeSession(s), "context": turnContext(s, nextContext)})
	return s, nil
}

// rewindLast removes the last user message and everything after it, and
// returns that message so it can be edited and sent again. The agent's own
// session also holds the removed exchange: it is dropped, and the next turn
// hands the remaining history over as text.
func (m *runtimeSessions) rewindLast(id string) (RuntimeSession, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.getLocked(id)
	if !ok {
		return s, "", errors.New("discussion not found or locked")
	}
	if m.preparing[id] || m.runs[id] != nil || m.nativeRunning(s) {
		return s, "", errors.New("wait for or stop the response before editing the last message")
	}
	last := -1
	for i := len(s.Messages) - 1; i >= 0; i-- {
		if s.Messages[i].Role == "user" {
			last = i
			break
		}
	}
	if last < 0 {
		return s, "", errors.New("no message to edit")
	}
	s = cloneRuntimeSession(s)
	text, _ := s.Messages[last].Content.(string)
	s.Messages = s.Messages[:last]
	s.PortableMessages = nil
	s.Compactions = nil
	kept := []RuntimeTurnRecord{}
	for _, turn := range s.Turns {
		if turn.MessageIndex < last {
			kept = append(kept, turn)
		}
	}
	s.Turns = kept
	m.closeACP(id)
	s.NativeSessionID, s.NativeRuntimeID, s.NativeContext = "", "", ""
	s.Status, s.Error = "idle", ""
	s.UpdatedAt = time.Now().UnixMilli()
	if err := putStoreJSON(bkRuntimeSessions, id, s); err != nil {
		return s, "", err
	}
	m.publishLocked(id, DiscussionEvent{"session": cloneRuntimeSession(s), "context": discussionContext(s)})
	return s, text, nil
}

// POST /api/runtime/sessions/rewind {id}: remove the last exchange and return
// the user's message for editing.
func handleRuntimeSessionRewind(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	s, text, err := workspaceSessions.rewindLast(req.ID)
	if err != nil {
		sendJSON(w, 409, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "text": text, "session": clientSession(s), "context": discussionContext(s)})
}

// memoryProtocol is Hermes' habit for any agent Loom launches: it keeps the
// small core notes current itself, with the loom MCP tools it already has, so
// continuity costs no extra model call and needs no local engine.
func memoryProtocol(s RuntimeSession) string {
	registered, ok := registeredRuntimes.lookup(s.RuntimeID)
	if !ok {
		return ""
	}
	if _, acp := registered.(*acpAdapter); !acp {
		return ""
	}
	project := "this discussion has no project, so skip project notes"
	if s.ProjectID != "" {
		project = fmt.Sprintf("the project notes: the semantic item tagged %s with scope project:%s (at most %d characters; its conventions, decisions and environment)", brain.ProjectNotesTag, s.ProjectID, brain.ProjectNotesLimit)
	}
	return fmt.Sprintf("Loom memory (tools on the MCP server \"loom\"): keep two short core notes current yourself, with update_memory (or remember when absent): the user profile, the global semantic item tagged %s (at most %d characters; who the user is, preferences, how they work), and %s. Update them when you learn something durable or finish meaningful work; condense instead of growing when a write reports the note is full. Use search_discussions to recall earlier discussions before asking the user to repeat. Never store secrets.", brain.ProfileTag, brain.ProfileLimit, project)
}
