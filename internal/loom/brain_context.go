package loom

import (
	"context"
	"strings"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/brain"
	"github.com/lucas-lepajollec/loom/internal/loom/discussion"
)

// Projects inherit connected second brains. The latest user message selects
// bounded cited passages, while conversation scope follows project membership.

const maxProjectBrainBudget = 8000

func lastUserText(messages []Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			if text := msgText(messages[i]); strings.TrimSpace(text) != "" {
				return text
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
		if source.ID == "conversations" || source.ID == "distilled" || !source.ReadOnly {
			if !hasName(out, source.ID) {
				out = append(out, source.ID)
			}
		}
	}
	return out
}

// autoContextBrainSources are the sources whose passages Loom may add to a
// request on its own: every source except secondary brains.
func autoContextBrainSources(p ChatProject) []string {
	secondary := map[string]bool{}
	if e, err := theBrain().get(); err == nil {
		for _, source := range e.Sources() {
			secondary[source.ID] = source.Secondary && !source.Primary
		}
	}
	out := []string{}
	for _, id := range effectiveProjectBrainSources(p) {
		if !secondary[id] {
			out = append(out, id)
		}
	}
	return out
}

// primarySecondBrainContext tells the model about the primary brain (its
// working memory) and the secondary brains it may consult on demand.
func primarySecondBrainContext() string {
	text := ""
	for _, part := range primarySecondBrainParts() {
		if text != "" {
			text += part.separator
		}
		text += part.text
	}
	return text
}

func primarySecondBrainParts() []contextPart {
	e, err := theBrain().get()
	if err != nil {
		return nil
	}
	parts := []contextPart{}
	secondary := []contextPart{}
	for _, source := range e.Sources() {
		if source.Primary && !source.ReadOnly && source.Permission == "write" {
			parts = append(parts, contextPart{
				text:      "Primary second brain: " + source.Label + " at " + source.Path + ". It is a user-owned source of truth. Proactively keep durable decisions, preferences and project facts current there when your available file tools can do so; update existing Markdown instead of duplicating it. Do not write credentials or private conversation transcripts.",
				separator: "\n\n",
				item:      discussion.ContextItem{Kind: "primary_brain", Label: source.Label, Source: source.ID, Reason: "primary vault instructions"},
			})
		} else if source.Secondary && !source.ReadOnly {
			access := "read-only"
			if source.Permission == "write" {
				access = "writable when the user asks"
			} else if source.Permission == "ask" {
				access = "ask before any change"
			}
			secondary = append(secondary, contextPart{
				text:      "- " + source.Label + " (source id " + source.ID + ", " + access + ")",
				separator: "\n",
				item:      discussion.ContextItem{Kind: "secondary_brains", Label: source.Label, Source: source.ID, Reason: "available for on-demand search"},
			})
		}
	}
	if len(secondary) > 0 {
		secondary[0].text = "Secondary brains, consulted only when the request needs them (search them with Loom's brain search using their source id; never assume their content):\n" + secondary[0].text
		secondary[0].separator = "\n\n"
		parts = append(parts, secondary...)
	}
	return parts
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
	pack := projectBrainPack(p, query)
	cites := []string{}
	for _, c := range pack.Citations {
		cites = append(cites, c.Citation)
	}
	return pack.Text, cites
}

func projectBrainPack(p ChatProject, query string) brain.Pack {
	sources := autoContextBrainSources(p)
	if len(sources) == 0 || strings.TrimSpace(query) == "" {
		return brain.Pack{}
	}
	budget := p.BrainBudget
	if budget <= 0 {
		budget = 1500
	}
	query = brain.ContextQuery(query)
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
		return brain.Pack{}
	}
	return pack
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
	if store, err := theBrain().memoryStore(); err == nil {
		references := map[string]bool{}
		if p.Continuity != nil {
			for _, ref := range p.Continuity.References {
				references[ref.DiscussionID] = true
				if path, err := store.DiscussionPath(ref.DiscussionID); err == nil {
					add(path)
				}
			}
		}
		for id := range allKV(bkRuntimeSessions) {
			var session RuntimeSession
			if getStoreJSON(bkRuntimeSessions, id, &session) && (session.ProjectID == p.ID || references[id] || references[session.NativeArchive]) {
				if path, err := store.DiscussionPath(id); err == nil {
					add(path)
				}
			}
		}
		for _, archive := range listArchives() {
			if archive.ProjectID == p.ID || references[archive.ID] {
				if path, err := store.DiscussionPath(archive.ID); err == nil {
					add(path)
				}
			}
		}
	}
	scope := map[string][]string{"conversations": paths}
	return scope
}
