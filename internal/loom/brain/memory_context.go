package brain

import (
	"fmt"
	"sort"
	"strings"
)

// MemoryBudgets are ceilings, including rendered labels and the pack header.
// The total ceiling can be smaller than the sum of the class ceilings.
type MemoryBudgets struct {
	Total      int `json:"total"`
	Reflex     int `json:"reflex"`
	Working    int `json:"working"`
	Procedural int `json:"procedural"`
	Semantic   int `json:"semantic"`
	Episodic   int `json:"episodic"`
	Session    int `json:"session"`
}

func DefaultMemoryBudgets() MemoryBudgets {
	return MemoryBudgets{Total: 1500, Reflex: 300, Working: 300, Procedural: 300, Semantic: 500, Episodic: 300}
}
func (b MemoryBudgets) Classes() map[string]int {
	return map[string]int{"reflex": max(0, b.Reflex), "working": max(0, b.Working), "procedural": max(0, b.Procedural), "semantic": max(0, b.Semantic), "episodic": max(0, b.Episodic), "session": max(0, b.Session)}
}

type SelectedMemory struct {
	Item    MemoryItem
	Section string
	Reason  string
	Tokens  int
}
type MemoryPack struct {
	Text  string
	Items []SelectedMemory
	Used  map[string]int
}

// ContextQuery is shared with passage retrieval: keep the latest 2000 bytes.
func ContextQuery(query string) string {
	query = strings.TrimSpace(query)
	if len(query) > 2000 {
		query = query[len(query)-2000:]
	}
	return query
}

// SelectMemory is a read-only, deterministic lexical baseline. It filters
// scope before relevance, and never ranks on last_used_at or the wall clock.
// Continuity state is pinned to its project or discussion binding.
// ProfileTag marks the single global item describing the user.
const ProfileTag = "user-profile"

func SelectMemory(items []MemoryItem, projectID, runtimeID, query, passages string, budgets MemoryBudgets, discussionIDs ...string) MemoryPack {
	discussionID := ""
	if len(discussionIDs) > 0 {
		discussionID = discussionIDs[0]
	}
	isState := func(item MemoryItem) bool {
		return item.Class == "working" && (projectID != "" && item.Scope == "project:"+projectID && contains(item.Tags, "project-state") || discussionID != "" && item.Scope == "task:"+discussionID && contains(item.Tags, "discussion-state"))
	}
	// The user's profile (who they are, how they work) is core memory: global,
	// always included first, truncated rather than dropped.
	isProfile := func(item MemoryItem) bool {
		return item.Class == "semantic" && item.Scope == "global" && contains(item.Tags, ProfileTag)
	}
	isSummary := func(item MemoryItem) bool {
		return projectID != "" && item.Class == "episodic" && item.Scope == "project:"+projectID && contains(item.Tags, "session-summary")
	}
	out := MemoryPack{Items: []SelectedMemory{}, Used: map[string]int{}}
	limits := budgets.Classes()
	querySet := queryTerms(ContextQuery(query))
	type candidate struct {
		item    MemoryItem
		score   float64
		overlap int
	}
	candidates := []candidate{}
	newest := int64(0)
	ancestors := map[string][]string{}
	for _, item := range items {
		if item.Scope != "global" && (projectID == "" || item.Scope != "project:"+projectID) && (runtimeID == "" || item.Scope != "agent:"+runtimeID) && !isState(item) {
			continue
		}
		ancestors[item.ID] = item.Supersedes
		if item.Status != "active" && item.Status != "uncertain" {
			continue
		}
		if limits[item.Class] <= 0 || item.Class == "working" && !isState(item) && (projectID == "" || item.Scope != "project:"+projectID) {
			continue
		}
		overlap := 0
		textTerms := map[string]bool{}
		for _, term := range terms(item.Text + " " + strings.Join(item.Tags, " ")) {
			textTerms[term] = true
		}
		for _, term := range querySet {
			if textTerms[term] {
				overlap++
			}
		}
		if overlap == 0 && item.Class != "reflex" && item.Class != "working" && !isSummary(item) && !isProfile(item) {
			continue
		}
		relevance := 0.0
		if len(querySet) > 0 {
			relevance = float64(overlap) / float64(len(querySet))
		}
		candidates = append(candidates, candidate{item, relevance*2 + item.Importance*.5 + item.Confidence*.3, overlap})
		newest = max(newest, max(item.CreatedAt, item.UpdatedAt))
	}
	// Prefer a relevant successor over its retained predecessor, including across
	// classes. Out-of-scope successors cannot affect this discussion's ranking.
	superseded := map[string]bool{}
	pending := []string{}
	for _, c := range candidates {
		pending = append(pending, c.item.Supersedes...)
	}
	for len(pending) > 0 {
		id := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if superseded[id] {
			continue
		}
		superseded[id] = true
		pending = append(pending, ancestors[id]...)
	}
	recent := []MemoryItem{}
	pinnedIDs := map[string]string{}
	pinnedAt := map[string]int64{}
	profileID := ""
	var profileAt int64
	isPinned := func(item MemoryItem) bool { return pinnedIDs[item.Scope] == item.ID }
	for _, c := range candidates {
		if superseded[c.item.ID] {
			continue
		}
		if isState(c.item) && (pinnedIDs[c.item.Scope] == "" || c.item.UpdatedAt > pinnedAt[c.item.Scope] || c.item.UpdatedAt == pinnedAt[c.item.Scope] && c.item.ID < pinnedIDs[c.item.Scope]) {
			pinnedIDs[c.item.Scope], pinnedAt[c.item.Scope] = c.item.ID, c.item.UpdatedAt
		}
		if isProfile(c.item) && (profileID == "" || c.item.UpdatedAt > profileAt || c.item.UpdatedAt == profileAt && c.item.ID < profileID) {
			profileID, profileAt = c.item.ID, c.item.UpdatedAt
		}
		if isSummary(c.item) {
			recent = append(recent, c.item)
		}
	}
	sort.Slice(recent, func(i, j int) bool {
		a, b := recent[i], recent[j]
		if a.UpdatedAt != b.UpdatedAt {
			return a.UpdatedAt > b.UpdatedAt
		}
		return a.ID < b.ID
	})
	preferred := map[string]int{}
	for i, item := range recent[:min(2, len(recent))] {
		preferred[item.ID] = i + 1
	}
	order := map[string]int{"reflex": 0, "working": 1, "procedural": 2, "semantic": 3, "episodic": 4, "session": 5}
	for i := range candidates {
		age := max(int64(0), newest-max(candidates[i].item.CreatedAt, candidates[i].item.UpdatedAt))
		candidates[i].score += .05 / (1 + float64(age)/float64(30*24*60*60*1000))
	}
	sort.Slice(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if (a.item.ID == profileID) != (b.item.ID == profileID) {
			return a.item.ID == profileID
		}
		if (isPinned(a.item)) != (isPinned(b.item)) {
			return isPinned(a.item)
		}
		if order[a.item.Class] != order[b.item.Class] {
			return order[a.item.Class] < order[b.item.Class]
		}
		if a.item.Class == "episodic" && (preferred[a.item.ID] != 0 || preferred[b.item.ID] != 0) {
			if preferred[a.item.ID] == 0 {
				return false
			}
			if preferred[b.item.ID] == 0 {
				return true
			}
			return preferred[a.item.ID] < preferred[b.item.ID]
		}
		if a.item.Status != b.item.Status {
			return a.item.Status == "active"
		}
		if a.item.Class == "reflex" || a.item.Class == "working" {
			if a.item.Importance != b.item.Importance {
				return a.item.Importance > b.item.Importance
			}
		} else if a.item.Class == "episodic" {
			if a.item.CreatedAt != b.item.CreatedAt {
				return a.item.CreatedAt > b.item.CreatedAt
			}
		}
		if a.score != b.score {
			return a.score > b.score
		}
		if a.item.UpdatedAt != b.item.UpdatedAt {
			return a.item.UpdatedAt > b.item.UpdatedAt
		}
		return a.item.ID < b.item.ID
	})
	seen := []string{normalizedMemoryText(passages)}
	for _, c := range candidates {
		item := c.item
		if superseded[item.ID] || isSummary(item) && preferred[item.ID] == 0 && c.overlap == 0 || isState(item) && !isPinned(item) || isProfile(item) && item.ID != profileID {
			continue
		}
		normalized := normalizedMemoryText(item.Text)
		duplicate := false
		for _, text := range seen {
			if strings.Contains(text, normalized) {
				duplicate = true
				break
			}
		}
		pinned := isPinned(item) || item.ID == profileID
		itemLimit := limits[item.Class]
		if isPinned(item) && len(pinnedIDs) > 1 {
			itemLimit = out.Used[item.Class] + limits[item.Class]/len(pinnedIDs)
		}
		if duplicate && !pinned {
			continue
		}
		section := "\n- [" + item.Class + " / " + item.Scope + "] " + strings.Join(strings.Fields(item.Text), " ")
		if out.Text == "" {
			section = "Loom memory (why: class/scope):" + section
		}
		if pinned && (Tokens(out.Text+section)-Tokens(out.Text) > itemLimit-out.Used[item.Class] || Tokens(out.Text+section) > max(0, budgets.Total)) {
			prefix := "\n- [" + item.Class + " / " + item.Scope + "] "
			if out.Text == "" {
				prefix = "Loom memory (why: class/scope):" + prefix
			}
			runes := []rune(strings.Join(strings.Fields(item.Text), " "))
			lo, hi := 0, len(runes)
			for lo < hi {
				mid := (lo + hi + 1) / 2
				cut := prefix + string(runes[:mid]) + "…"
				if Tokens(out.Text+cut)-Tokens(out.Text) <= itemLimit-out.Used[item.Class] && Tokens(out.Text+cut) <= max(0, budgets.Total) {
					lo = mid
				} else {
					hi = mid - 1
				}
			}
			section = prefix + string(runes[:lo]) + "…"
		}
		cost := Tokens(out.Text+section) - Tokens(out.Text)
		if out.Used[item.Class]+cost > limits[item.Class] || Tokens(out.Text+section) > max(0, budgets.Total) {
			continue
		}
		reason := "matches your message"
		if item.Class == "reflex" {
			reason = "always included (reflex)"
		}
		if item.Class == "working" {
			reason = "current project working memory"
			if isPinned(item) {
				reason = "pinned continuity state"
			}
		}
		if item.Class == "episodic" {
			reason = "matches your message; most recent first"
			if preferred[item.ID] != 0 {
				reason = "recent project session summary"
			}
		}
		if item.ID == profileID {
			reason = "your profile (always included)"
		}
		if item.Status == "uncertain" {
			reason += "; uncertain, ranked lower"
		}
		out.Text += section
		out.Used[item.Class] += cost
		out.Items = append(out.Items, SelectedMemory{Item: item, Section: section, Reason: reason, Tokens: cost})
		seen = append(seen, normalized)
	}
	for i := range out.Items {
		out.Items[i].Reason += fmt.Sprintf("; budget %d tokens, %d used", max(0, budgets.Total), Tokens(out.Text))
	}
	return out
}
