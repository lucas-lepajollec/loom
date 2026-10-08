package brain

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

func TestCandidatesFromMessage(t *testing.T) {
	for _, tc := range []struct{ text, class string }{
		{"RETIENS que je travaille sous Linux.", "semantic"},
		{"Souviens-toi de notre choix de stockage.", "semantic"},
		{"N'oublie pas que notre dépôt est public.", "semantic"},
		{"Remember that our repository is public.", "semantic"},
		{"Keep in mind that releases need review.", "semantic"},
		{"Note that our server runs Linux.", "semantic"},
		{"Toujours vérifier les sauvegardes.", "reflex"},
		{"Ne jamais publier sans validation.", "reflex"},
		{"Désormais utiliser les tests courts.", "reflex"},
		{"À partir de maintenant, tester avant de publier.", "reflex"},
		{"Always verify the backups.", "reflex"},
		{"Never publish without review.", "reflex"},
		{"Tu dois toujours répondre en français.", "reflex"},
		{"« Toujours » relire avant de publier.", "reflex"},
		{"From now on, verify every release.", "reflex"},
		{"Je préfère les réponses courtes.", "semantic"},
		{"J’aime pas les longues réponses.", "semantic"},
		{"Je veux pas de messages trop longs.", "semantic"},
		{"I PREFER concise responses.", "semantic"},
		{"I don't like lengthy responses.", "semantic"},
		{"Pour publier, lancer les tests.", "procedural"},
		{"La procédure commence par les tests.", "procedural"},
		{"Les étapes sont documentées ici.", "procedural"},
		{"Steps to publish include validation.", "procedural"},
		{"The way to publish is through CI.", "procedural"},
	} {
		t.Run(tc.text, func(t *testing.T) {
			items := CandidatesFromMessage(tc.text)
			if len(items) != 1 || items[0].Text != tc.text || items[0].Class != tc.class {
				t.Fatalf("%+v", items)
			}
		})
	}
	for _, text := range []string{
		"Always?", "remember me", "Remember our preference?", "Remember our preference?!", "A normal message without durable signals.",
		"The remembrance is interesting; the nevermore story is long.", "préremember suffix", "remembering our preferences is useful.",
		"```go\nAlways verify the backups.\n```", "~~~\nRemember our preference.\n~~~", "```\nRemember our preference.",
		"je suis toujours pas fan de l'ui de brain", "ça marche jamais avec pi et deepseek", "I always forget where the logs are.",
		"    Always verify the backups.", "\tRemember our preference.", "Remember " + strings.Repeat("x", 4000),
	} {
		if items := CandidatesFromMessage(text); len(items) != 0 {
			t.Errorf("%q: %+v", text, items)
		}
	}
	text := "  Je préfère les réponses courtes. Always test releases!\n```\nNever use this code.\n```\nSteps to publish start here. Note that this is fourth."
	if items := CandidatesFromMessage(text); len(items) != 3 || items[0].Text != "Je préfère les réponses courtes." || items[2].Class != "procedural" {
		t.Fatalf("sentences/cap: %+v", items)
	}
	items := CandidatesFromMessage("Remember " + strings.Repeat("é", 350))
	if len(items) != 1 || len(items[0].Text) > 500 || !utf8.ValidString(items[0].Text) {
		t.Fatalf("UTF-8 limit: %+v", items)
	}
}
func TestMemoryCandidatesReviewAndPersistence(t *testing.T) {
	s, opts := memoryFixture(t)
	req := memoryRequest("Release backups require review.")
	req.Status, req.Class = "candidate", "reflex"
	item := mustRemember(t, s, req)
	s = NewMemoryStore(opts)
	if list := mustList(t, s, MemoryFilter{}); len(list.Items) != 0 {
		t.Fatal("candidate in default list")
	}
	for _, status := range []string{"all", "candidate"} {
		list := mustList(t, s, MemoryFilter{Status: status})
		if len(list.Items) != 1 || list.Items[0].Status != "candidate" {
			t.Fatalf("candidate filter %s: %+v", status, list)
		}
		if pack := SelectMemory(list.Items, "", "", "release", "", DefaultMemoryBudgets()); len(pack.Items) != 0 || pack.Text != "" {
			t.Fatal("candidate in context")
		}
	}
	text := "Release backups always require human review."
	edited, err := s.Update(UpdateMemoryRequest{ID: item.ID, Supersede: true, Patch: MemoryPatch{Text: &text}})
	if err != nil || edited.Status != "candidate" {
		t.Fatalf("editing implicitly accepted: %+v %v", edited, err)
	}
	active := "active"
	accepted, err := s.Update(UpdateMemoryRequest{ID: edited.ID, Patch: MemoryPatch{Status: &active}})
	if err != nil || accepted.Status != "active" || len(mustList(t, s, MemoryFilter{}).Items) != 1 {
		t.Fatalf("accept: %+v %v", accepted, err)
	}
	if pack := SelectMemory([]MemoryItem{accepted}, "", "", "", "", DefaultMemoryBudgets()); len(pack.Items) != 1 {
		t.Fatal("accepted reflex unavailable")
	}
	rejected, err := s.Forget(accepted.ID)
	if err != nil || rejected.Status != "expired" {
		t.Fatalf("reject: %+v %v", rejected, err)
	}
}
func TestRememberCandidatePreservesExistingActive(t *testing.T) {
	s, _ := memoryFixture(t)
	req := memoryRequest("Remember the public repository.")
	active := mustRemember(t, s, req)
	req.Status = "candidate"
	pending := mustRemember(t, s, req)
	if pending.ID == active.ID || pending.Status != "candidate" {
		t.Fatalf("candidate request returned active memory: %+v", pending)
	}
	items := mustList(t, s, MemoryFilter{}).Items
	if len(items) != 1 || items[0].ID != active.ID || items[0].UpdatedAt != active.UpdatedAt {
		t.Fatal("candidate request mutated active memory")
	}
}
func TestCandidateDedupeAcrossClassesScopesAndStatuses(t *testing.T) {
	for _, status := range []string{"active", "uncertain", "candidate", "superseded", "expired"} {
		t.Run(status, func(t *testing.T) {
			s, _ := memoryFixture(t)
			req := memoryRequest("  REMEMBER   our preference! ")
			req.Status = status
			original := mustRemember(t, s, req)
			index := 2
			items, err := s.AddCandidates([]CandidateDraft{{Class: "reflex", Text: "remember our preference."}, {Class: "semantic", Text: "Remember our preference!"}}, "project:elsewhere", MemoryProvenance{Kind: "discussion", DiscussionID: "discussion", MessageIndex: &index, Agent: "runner"})
			want := 0
			if status == "superseded" || status == "expired" {
				want = 1
			}
			if err != nil || len(items) != want {
				t.Fatalf("%+v %v", items, err)
			}
			if want == 1 && (items[0].Confidence != .4 || items[0].Importance != .5 || items[0].Tags[0] != "auto" || items[0].Provenance.Agent != "runner" || items[0].Scope != "project:elsewhere") {
				t.Fatalf("metadata: %+v", items[0])
			}
			for _, item := range mustList(t, s, MemoryFilter{Status: "all"}).Items {
				if item.ID == original.ID && (item.Status != status || item.UpdatedAt != original.UpdatedAt) {
					t.Fatal("dedupe mutated existing memory")
				}
			}
		})
	}
}
func TestCandidatePendingCapConcurrent(t *testing.T) {
	s, opts := memoryFixture(t)
	var wg sync.WaitGroup
	for i := 0; i < 60; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := s.AddCandidates([]CandidateDraft{{Class: "semantic", Text: fmt.Sprintf("Remember unique preference %d.", i)}}, "global", MemoryProvenance{Kind: "discussion"})
			if err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	items := mustList(t, NewMemoryStore(opts), MemoryFilter{Status: "candidate"}).Items
	if len(items) != 50 {
		t.Fatalf("cap: %d", len(items))
	}
	if _, err := s.Forget(items[0].ID); err != nil {
		t.Fatal(err)
	}
	added, err := s.AddCandidates([]CandidateDraft{{Class: "semantic", Text: "Remember a new available slot."}}, "global", MemoryProvenance{Kind: "discussion"})
	if err != nil || len(added) != 1 || len(mustList(t, s, MemoryFilter{Status: "candidate"}).Items) != 50 {
		t.Fatalf("freed slot: %+v %v", added, err)
	}
}

func TestExplicitMemoryRequestsAreKeptDirectly(t *testing.T) {
	cases := []struct{ text, class string }{
		{"Sache que je suis un esprit logique : pour mes futures demandes, réponds de façon structurée.", "reflex"},
		{"Je voudrais que tu saches que j'habite à Lyon.", "semantic"},
		{"À l'avenir, utilise des tableaux pour comparer.", "reflex"},
	}
	for _, c := range cases {
		items := CandidatesFromMessage(c.text)
		if len(items) != 1 || items[0].Class != c.class || !items[0].Explicit {
			t.Fatalf("%q: %+v", c.text, items)
		}
	}
	if items := CandidatesFromMessage("Je préfère les réponses courtes."); len(items) != 1 || items[0].Explicit {
		t.Fatalf("preferences stay reviewed suggestions: %+v", items)
	}
	s := NewMemoryStore(MemoryStoreOptions{Dir: t.TempDir(), Base: ".loom", Available: func() error { return nil }})
	saved, err := s.AddCandidates([]CandidateDraft{
		{Class: "reflex", Text: "Pour mes futures demandes, réponds de façon structurée.", Explicit: true},
		{Class: "semantic", Text: "Retiens que ce projet publie avec un tag annoté.", Explicit: true},
		{Class: "semantic", Text: "Je préfère les réponses courtes."},
	}, "project:p", MemoryProvenance{Kind: "discussion"})
	if err != nil || len(saved) != 3 {
		t.Fatal(saved, err)
	}
	if saved[0].Status != "active" || saved[0].Scope != "global" || saved[1].Scope != "project:p" || saved[1].Status != "active" || saved[2].Status != "candidate" {
		t.Fatalf("%+v", saved)
	}
}
