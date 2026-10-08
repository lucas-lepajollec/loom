package loom

import (
	"fmt"
	"sort"
	"strings"

	"github.com/lucas-lepajollec/loom/internal/loom/brain"
)

func handoffText(s RuntimeSession, previous, summary string) string {
	goal, request, recap := "", "", ""
	for _, msg := range s.Messages {
		text := msgText(msg)
		if msg.Role == "user" && !strings.HasPrefix(text, "/") {
			if goal == "" {
				goal = text
			}
			request = text
		}
		if msg.Role == "assistant" && text != "" {
			recap = text
		}
	}
	compacted := ""
	if _, rest, ok := strings.Cut(previous, compactSummaryPrefix); ok {
		compacted, _, _ = strings.Cut(rest, "\n[/CONTEXT COMPACTED]")
	}
	summary = strings.TrimPrefix(summary, compactSummaryPrefix)
	if summary != "" && !strings.Contains(compacted, summary) {
		compacted += "\n" + summary
	}
	text := fmt.Sprintf("## Goal\n%s\n\n## Recap\n", acpClip(goal, 300))
	if compacted != "" {
		text += compactSummaryPrefix + "\n" + handoffRecap(compacted) + "\n[/CONTEXT COMPACTED]\n\n"
	}
	text += acpClip(recap, 2200) + "\n\n## Current request\n" + acpClip(request, 1000)
	if len(s.Turns) > 0 {
		plan, commands := "", ""
		for _, e := range s.Turns[len(s.Turns)-1].ACPEvents {
			if e["type"] == "plan" {
				plan = ""
				b := acpCloneMap(e)
				entries, _ := b["entries"].([]any)
				for _, raw := range entries {
					entry, _ := raw.(map[string]any)
					plan += fmt.Sprintf("- %v: %v\n", entry["status"], entry["content"])
				}
			}
			if e["type"] == "tool_end" {
				tool, _ := e["tool"].(map[string]any)
				if tool["kind"] == "execute" {
					title, _ := tool["title"].(string)
					commands += "- " + acpClip(title, 240) + "\n"
				}
			}
		}
		if plan != "" {
			text += "\n\n## Plan\n" + acpClip(plan, 1000)
		}
		if commands != "" {
			text += "\n\n## Commands reported\n" + acpClip(commands, 600)
		}
	}
	if len(s.Files) > 0 {
		files := ""
		for _, file := range s.Files {
			files += "- " + file.Op + ": " + file.Path + "\n"
		}
		text += "\n\n## Files reported\n" + acpClip(files, 600)
	}
	return acpClip(text, 8192)
}
func saveDiscussionHandoff(s RuntimeSession, summary string) error {
	service := theBrain()
	service.handoffMu.Lock()
	defer service.handoffMu.Unlock()
	store, err := service.memoryStore()
	if err != nil {
		return err
	}
	list, err := store.List(brain.MemoryFilter{Status: "all"})
	if err != nil {
		return err
	}
	scope := "task:" + s.ID
	old := continuityMemory(list.Items, "working", scope, "discussion-state", s.ID)
	text := handoffText(s, old.Text, summary)
	p := brain.MemoryProvenance{Kind: "discussion", DiscussionID: s.ID, Agent: s.RuntimeID}
	if old.ID != "" {
		if _, err = store.Update(brain.UpdateMemoryRequest{ID: old.ID, Patch: brain.MemoryPatch{Text: &text}}); err != nil {
			return err
		}
	} else if _, err = store.Remember(brain.RememberRequest{Class: "working", Scope: scope, Tags: []string{"discussion-state"}, Text: text, Provenance: p}); err != nil {
		return err
	}
	if s.ProjectID == "" {
		return nil
	}
	list, err = store.List(brain.MemoryFilter{})
	if err != nil {
		return err
	}
	members := map[string]bool{s.ID: true}
	for id := range allKV(bkRuntimeSessions) {
		var member RuntimeSession
		if getStoreJSON(bkRuntimeSessions, id, &member) && member.ProjectID == s.ProjectID {
			members[id] = true
		}
	}
	states := []brain.MemoryItem{}
	for _, item := range list.Items {
		if item.Class == "working" && hasName(item.Tags, "discussion-state") && members[item.Provenance.DiscussionID] {
			states = append(states, item)
		}
	}
	sort.Slice(states, func(i, j int) bool {
		if states[i].UpdatedAt != states[j].UpdatedAt {
			return states[i].UpdatedAt > states[j].UpdatedAt
		}
		return states[i].ID < states[j].ID
	})
	aggregate := ""
	for _, item := range states[:min(len(states), 4)] {
		aggregate += "## Discussion " + item.Provenance.DiscussionID + "\n" + acpClip(item.Text, 1800) + "\n\n"
	}
	old = continuityMemory(list.Items, "working", "project:"+s.ProjectID, "project-state", "")
	if old.ID != "" {
		_, err = store.Update(brain.UpdateMemoryRequest{ID: old.ID, Patch: brain.MemoryPatch{Text: &aggregate}})
	} else {
		_, err = store.Remember(brain.RememberRequest{Class: "working", Scope: "project:" + s.ProjectID, Tags: []string{"project-state"}, Text: aggregate, Provenance: p})
	}
	return err
}

func handoffRecap(text string) string {
	text = strings.TrimSpace(text)
	if len(text) > 2400 {
		return "…\n" + validUTF8Tail(text, 2400)
	}
	return text
}
