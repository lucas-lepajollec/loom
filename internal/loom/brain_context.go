package loom

import (
	"context"
	"strings"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/brain"
)

// Projects inherit connected second brains. The latest user message selects
// bounded cited passages, while conversation scope follows project membership.

const maxProjectBrainBudget = 8000

func lastUserText(messages []Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			if t, ok := messages[i].Content.(string); ok && strings.TrimSpace(t) != "" {
				return t
			}
		}
	}
	return ""
}

// brainSourceKinds maps the Brain's source ids to their kind.
func brainSourceKinds() map[string]string {
	e, err := theBrain().get()
	if err != nil {
		return nil
	}
	out := map[string]string{}
	for _, s := range e.Sources() {
		out[s.ID] = s.Kind
	}
	return out
}

// Projects inherit the connected second brains. Their own discussions and
// reviewed Loom memory are always part of the continuity layer; users should
// not have to wire individual transcripts into a project form.
func effectiveProjectBrainSources(p ChatProject) []string {
	e, err := theBrain().get()
	if err != nil {
		return append([]string{}, p.BrainSources...)
	}
	out := []string{}
	for _, source := range e.Sources() {
		if source.ID == "conversations" || source.ID == "memory" || source.ID == "distilled" || !source.ReadOnly {
			if !hasName(out, source.ID) {
				out = append(out, source.ID)
			}
		}
	}
	return out
}

func primarySecondBrainContext() string {
	e, err := theBrain().get()
	if err != nil {
		return ""
	}
	for _, source := range e.Sources() {
		if source.Primary && !source.ReadOnly && source.Permission == "write" {
			return "Primary second brain: " + source.Label + " at " + source.Path + ". It is a user-owned source of truth. Proactively keep durable decisions, preferences and project facts current there when your available file tools can do so; update existing Markdown instead of duplicating it. Do not write credentials or private conversation transcripts."
		}
	}
	return ""
}

func attachPrimarySecondBrain(s *RuntimeSession, agent acpAgent) {
	if s == nil || agent.Remote {
		return
	}
	_, dir, err := primarySecondBrain()
	if err != nil || dir == "" || dir == s.Workdir || hasName(s.AdditionalDirs, dir) || len(s.AdditionalDirs) >= 16 {
		return
	}
	s.AdditionalDirs = append(s.AdditionalDirs, dir)
}

// projectBrainContext returns the Brain passages for a project and query, and
// the citations, or "" when the project uses no Brain source.
func projectBrainContext(p ChatProject, query string) (string, []string) {
	sources := effectiveProjectBrainSources(p)
	if len(sources) == 0 || strings.TrimSpace(query) == "" {
		return "", nil
	}
	budget := p.BrainBudget
	if budget <= 0 {
		budget = 1500
	}
	if len(query) > 2000 {
		query = query[len(query)-2000:]
	}
	kinds := brainSourceKinds()
	personal := false
	for _, id := range sources {
		personal = personal || kinds[id] == "personal"
	}
	// An unavailable embedding engine must not stall the discussion inspector.
	// SearchContext falls back to the lexical index when embedding times out.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	pack, err := theBrain().PackContext(ctx, brain.PackRequest{Query: query, BudgetTokens: budget, Sources: sources, Personal: personal, PathPrefixes: projectReferenceScope(p)})
	if err != nil || strings.TrimSpace(pack.Text) == "" {
		return "", nil
	}
	cites := []string{}
	for _, c := range pack.Citations {
		cites = append(cites, c.Citation)
	}
	return pack.Text, cites
}

// Conversation continuity follows project membership. Moving a discussion in
// or out of a project changes its scope automatically.
func projectReferenceScope(p ChatProject) map[string][]string {
	paths := []string{}
	add := func(path string) {
		if path != "" && !hasName(paths, path) {
			paths = append(paths, path)
		}
	}
	for _, a := range listArchives() {
		if a.ProjectID == p.ID {
			add("native/" + a.ID)
		}
	}
	for id := range allKV(bkRuntimeSessions) {
		var s RuntimeSession
		if getStoreJSON(bkRuntimeSessions, id, &s) && s.ProjectID == p.ID {
			if s.NativeArchive != "" {
				add("native/" + s.NativeArchive)
			} else {
				add("discussion/" + s.ID)
			}
		}
	}
	// Keep old explicit references readable while older project data migrates.
	if p.Continuity != nil {
		for _, ref := range p.Continuity.References {
			var s RuntimeSession
			if getStoreJSON(bkRuntimeSessions, ref.DiscussionID, &s) {
				if s.NativeArchive != "" {
					add("native/" + s.NativeArchive)
				} else {
					add("discussion/" + s.ID)
				}
			} else {
				var a convArchive
				if getStoreJSON(bkChatHist, ref.DiscussionID, &a) {
					add("native/" + a.ID)
				}
			}
		}
	}
	scope := map[string][]string{"conversations": paths}
	return scope
}
