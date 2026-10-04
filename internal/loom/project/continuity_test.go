package project

import (
	"strings"
	"testing"
	"time"
)

func TestMinimumContextKeepsRationaleStateAndReviewedCapsules(t *testing.T) {
	c := Continuity{Core: Core{Purpose: "A portable workspace", Rationale: "Keep decisions across executors", Constraints: "No hidden state", Decisions: "Sources remain canonical"}, WorkingState: "Next: verify mobile", StateUpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), References: []Reference{{DiscussionID: "reference", Capsule: "Review accepted"}}}
	if err := Validate(c); err != nil {
		t.Fatal(err)
	}
	text := Text(&c)
	for _, part := range []string{c.Core.Purpose, c.Core.Rationale, c.Core.Constraints, c.Core.Decisions, c.WorkingState, c.References[0].Capsule, "2026-01-01"} {
		if !strings.Contains(text, part) {
			t.Fatalf("minimum context lost %s", part)
		}
	}
	c.References = append(c.References, c.References[0])
	if Validate(c) == nil {
		t.Fatal("duplicate reference accepted")
	}
	c.References = nil
	c.WorkingState = strings.Repeat("x", 4001)
	if Validate(c) == nil {
		t.Fatal("unbounded working state")
	}
}
