package brain

import (
	"reflect"
	"strings"
	"testing"
)

func contextMemoryFixture(t *testing.T, class, scope, text string) MemoryItem {
	t.Helper()
	store, _ := memoryFixture(t)
	req := memoryRequest(text)
	req.Class, req.Scope = class, scope
	return mustRemember(t, store, req)
}
func selectedIDs(pack MemoryPack) []string {
	ids := []string{}
	for _, selected := range pack.Items {
		ids = append(ids, selected.Item.ID)
	}
	return ids
}
func TestMemoryContextReflexAndBudgets(t *testing.T) {
	high := contextMemoryFixture(t, "reflex", "global", "Respect the owner's instructions.")
	high.ID, high.Importance = "high", 1
	low := high
	low.ID, low.Text, low.Importance = "low", "Keep responses concise.", .2
	huge := high
	huge.ID, huge.Text, huge.Importance = "huge", strings.Repeat("x", 1300), .9
	pack := SelectMemory([]MemoryItem{low, huge, high}, "", "", "unrelated", "", DefaultMemoryBudgets())
	if got := selectedIDs(pack); !reflect.DeepEqual(got, []string{"high", "low"}) {
		t.Fatalf("reflex order/cut-off: %v", got)
	}
	if pack.Used["reflex"] > 300 || Tokens(pack.Text) > 1500 || !strings.Contains(pack.Items[0].Reason, "always included (reflex)") {
		t.Fatalf("budgets/reason: %+v", pack)
	}
	// Class ceilings include the first header; a total ceiling also constrains
	// packs whose class ceilings would otherwise add up to more than 1500.
	limits := DefaultMemoryBudgets()
	limits.Total = pack.Items[0].Tokens
	cutoff := SelectMemory([]MemoryItem{low, high}, "", "", "", "", limits)
	if got := selectedIDs(cutoff); !reflect.DeepEqual(got, []string{"high"}) || Tokens(cutoff.Text) != limits.Total {
		t.Fatalf("total cut-off: %+v", cutoff)
	}
	limits.Reflex = pack.Items[0].Tokens - 1
	if got := SelectMemory([]MemoryItem{high}, "", "", "", "", limits); len(got.Items) != 0 {
		t.Fatal("header/labels were not charged")
	}
}
func TestMemoryContextScopeBeforeRanking(t *testing.T) {
	base := contextMemoryFixture(t, "semantic", "global", "quartz global memory")
	items := []MemoryItem{}
	scopes := []string{"global", "project:here", "agent:runner", "project:other", "agent:other", "machine:here", "task:here"}
	for i, scope := range scopes {
		item := base
		item.ID = scope
		item.Scope = scope
		item.Text += " " + scope
		if i >= 3 {
			item.Importance = 1
			item.Confidence = 1
			item.UpdatedAt += 10000000000
			item.Supersedes = []string{"global"}
		}
		items = append(items, item)
	}
	got := SelectMemory(items, "here", "runner", "quartz", "", DefaultMemoryBudgets())
	if len(got.Items) != 3 {
		t.Fatalf("scope leaked or changed scoped ranking: %v", selectedIDs(got))
	}
	for _, item := range got.Items {
		if !contains(scopes[:3], item.Item.Scope) {
			t.Fatal("scope leaked")
		}
	}
	if got := SelectMemory(items, "", "", "quartz", "", DefaultMemoryBudgets()); len(got.Items) != 1 || got.Items[0].Item.Scope != "global" {
		t.Fatalf("unbound: %+v", got)
	}
	// Global/agent working memory and session memory are excluded by default.
	base.Class = "working"
	base.Scope = "project:here"
	global := base
	global.ID, global.Scope, global.Text = "global-working", "global", "unbound working state"
	agent := base
	agent.ID, agent.Scope, agent.Text = "agent-working", "agent:runner", "agent working state"
	session := base
	session.ID, session.Class, session.Text = "session", "session", "quartz session"
	got = SelectMemory([]MemoryItem{base, global, agent, session}, "here", "runner", "", "", DefaultMemoryBudgets())
	if len(got.Items) != 1 || got.Items[0].Item.Scope != "project:here" {
		t.Fatalf("working/session policy: %+v", got)
	}
}
func TestMemoryContextRelevanceAndUncertainty(t *testing.T) {
	base := contextMemoryFixture(t, "semantic", "global", "quartz deployment detailed match")
	base.ID = "best"
	partial := base
	partial.ID, partial.Text, partial.Importance, partial.Confidence = "partial", "quartz partial", 1, 1
	tags := base
	tags.ID, tags.Text, tags.Tags = "tags", "tag-only match", []string{"quartz", "deployment"}
	unrelated := base
	unrelated.ID, unrelated.Text, unrelated.Importance = "unrelated", "bananas", 1
	uncertain := base
	uncertain.ID, uncertain.Text, uncertain.Importance, uncertain.Status = "uncertain", "quartz deployment uncertain", 1, "uncertain"
	expired := base
	expired.ID, expired.Text, expired.Status = "expired", "quartz deployment expired", "expired"
	pack := SelectMemory([]MemoryItem{partial, unrelated, uncertain, tags, base, expired}, "", "", "quartz deployment", "", DefaultMemoryBudgets())
	if got := selectedIDs(pack); !reflect.DeepEqual(got, []string{"best", "tags", "partial", "uncertain"}) {
		t.Fatalf("relevance/uncertainty: %v", got)
	}
	if !strings.Contains(pack.Items[3].Reason, "uncertain") {
		t.Fatal("uncertainty not explained")
	}
	// Equal lexical relevance uses importance, confidence, then a small recency bonus.
	strong := base
	strong.ID, strong.Text, strong.Importance = "strong", "quartz deployment strong", .9
	confident := base
	confident.ID, confident.Text, confident.Confidence = "confident", "quartz deployment confident", .9
	recent := base
	recent.ID, recent.Text, recent.UpdatedAt = "recent", "quartz deployment recent", base.UpdatedAt+86400000
	pack = SelectMemory([]MemoryItem{base, recent, confident, strong}, "", "", "quartz deployment", "", DefaultMemoryBudgets())
	if got := selectedIDs(pack); !reflect.DeepEqual(got, []string{"strong", "confident", "recent", "best"}) {
		t.Fatalf("score factors: %v", got)
	}
	// A long pasted draft uses the same trailing query as passage retrieval.
	pack = SelectMemory([]MemoryItem{base, unrelated}, "", "", "bananas"+strings.Repeat(" ", 2100)+"quartz deployment", "", DefaultMemoryBudgets())
	if got := selectedIDs(pack); !reflect.DeepEqual(got, []string{"best"}) {
		t.Fatalf("query trimming: %v", got)
	}
}
func TestMemoryContextEpisodicMostRecent(t *testing.T) {
	old := contextMemoryFixture(t, "episodic", "global", "quartz deployment old decision")
	old.ID, old.CreatedAt, old.Importance = "old", 1, 1
	latest := old
	latest.ID, latest.Text, latest.CreatedAt, latest.Importance = "latest", "quartz new decision", 2, .1
	got := SelectMemory([]MemoryItem{old, latest}, "", "", "quartz deployment", "", DefaultMemoryBudgets())
	if ids := selectedIDs(got); !reflect.DeepEqual(ids, []string{"latest", "old"}) {
		t.Fatalf("episodic chronology: %v", ids)
	}
}
func TestMemoryContextDedupeSupersessionAndDeterminism(t *testing.T) {
	old := contextMemoryFixture(t, "reflex", "global", "quartz old instruction")
	old.ID = "old"
	middle := old
	middle.ID, middle.Text, middle.Status, middle.Supersedes = "middle", "quartz middle instruction", "superseded", []string{"old"}
	latest := old
	latest.ID, latest.Class, latest.Text, latest.Supersedes = "latest", "semantic", "quartz current instruction", []string{"middle"}
	duplicate := latest
	duplicate.ID, duplicate.Importance, duplicate.Supersedes = "duplicate", .1, nil
	contained := latest
	contained.ID, contained.Text, contained.Importance, contained.Supersedes = "contained", "current instruction", .1, nil
	passage := latest
	passage.ID, passage.Text, passage.Supersedes = "passage", "quartz already retrieved", nil
	items := []MemoryItem{old, middle, latest, duplicate, contained, passage}
	pack := SelectMemory(items, "", "", "quartz current instruction", "QUARTZ  already\nretrieved with more detail", DefaultMemoryBudgets())
	if ids := selectedIDs(pack); !reflect.DeepEqual(ids, []string{"latest"}) {
		t.Fatalf("dedupe/supersession: %v", ids)
	}
	for i := range items {
		items[i].LastUsedAt += 1000000
	}
	// Reverse the storage order as well; neither input order nor touch affects selection.
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
	again := SelectMemory(items, "", "", "quartz current instruction", "QUARTZ  already\nretrieved with more detail", DefaultMemoryBudgets())
	if pack.Text != again.Text || !reflect.DeepEqual(selectedIDs(pack), selectedIDs(again)) {
		t.Fatal("selection changed after touch or storage reordering")
	}
}

func TestMemoryContextTotalCapAcrossClasses(t *testing.T) {
	base := contextMemoryFixture(t, "reflex", "global", "quartz rule")
	items := []MemoryItem{}
	for _, class := range []string{"reflex", "working", "procedural", "semantic", "episodic"} {
		item := base
		item.ID, item.Class = class, class
		size := 1080
		if class == "semantic" {
			size = 1760
		}
		item.Text = class + " quartz " + strings.Repeat("x", size)
		if class == "working" {
			item.Scope = "project:here"
		}
		items = append(items, item)
	}
	limits := DefaultMemoryBudgets()
	unlimitedTotal := limits
	unlimitedTotal.Total = 2000
	all := SelectMemory(items, "here", "runner", "quartz", "", unlimitedTotal)
	if len(all.Items) != 5 || Tokens(all.Text) <= 1500 {
		t.Fatalf("fixture does not exercise total ceiling: %v (%d tokens)", selectedIDs(all), Tokens(all.Text))
	}
	pack := SelectMemory(items, "here", "runner", "quartz", "", limits)
	if ids := selectedIDs(pack); !reflect.DeepEqual(ids, []string{"reflex", "working", "procedural", "semantic"}) || Tokens(pack.Text) > 1500 {
		t.Fatalf("shared cap/class priority: %v (%d tokens)", ids, Tokens(pack.Text))
	}
	total := 0
	for class, used := range pack.Used {
		if used > limits.Classes()[class] {
			t.Fatalf("%s exceeded class cap", class)
		}
		total += used
	}
	if total != Tokens(pack.Text) {
		t.Fatal("class usage does not sum to pack cost")
	}
}
