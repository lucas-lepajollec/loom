package loom

import (
	"context"
	"strings"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/brain"
)

// A project can draw on the Brain: its chosen sources are searched with the
// latest user message and the best passages, within the project's token
// budget, join the discussion context with their citations. Personal sources
// are used only because the user picked them for this project.

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

// projectBrainContext returns the Brain passages for a project and query, and
// the citations, or "" when the project uses no Brain source.
func projectBrainContext(p ChatProject, query string) (string, []string) {
	if len(p.BrainSources) == 0 || p.BrainBudget <= 0 || strings.TrimSpace(query) == "" {
		return "", nil
	}
	if len(query) > 2000 {
		query = query[len(query)-2000:]
	}
	kinds := brainSourceKinds()
	personal := false
	for _, id := range p.BrainSources {
		personal = personal || kinds[id] == "personal"
	}
	// An unavailable embedding engine must not stall the discussion inspector.
	// SearchContext falls back to the lexical index when embedding times out.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	pack, err := theBrain().PackContext(ctx, brain.PackRequest{Query: query, BudgetTokens: p.BrainBudget, Sources: p.BrainSources, Personal: personal, PathPrefixes: projectReferenceScope(p)})
	if err != nil || strings.TrimSpace(pack.Text) == "" {
		return "", nil
	}
	cites := []string{}
	for _, c := range pack.Citations {
		cites = append(cites, c.Citation)
	}
	return pack.Text, cites
}

// Restrict conversation retrieval to explicit references, even when the whole
// conversation source was selected in an older project configuration.
func projectReferenceScope(p ChatProject) map[string][]string {
	scope := map[string][]string{"conversations": {}}
	if p.Continuity == nil {
		return scope
	}
	for _, ref := range p.Continuity.References {
		var s RuntimeSession
		if getStoreJSON(bkRuntimeSessions, ref.DiscussionID, &s) {
			if s.NativeArchive != "" {
				scope["conversations"] = append(scope["conversations"], "native/"+s.NativeArchive)
			} else {
				scope["conversations"] = append(scope["conversations"], "discussion/"+s.ID)
			}
		} else {
			var a convArchive
			if getStoreJSON(bkChatHist, ref.DiscussionID, &a) {
				scope["conversations"] = append(scope["conversations"], "native/"+a.ID)
			}
		}
	}
	return scope
}
