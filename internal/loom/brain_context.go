package loom

import (
	"strings"
)

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
