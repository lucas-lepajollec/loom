package acp

import (
	"strings"
	"testing"
)

func TestLineDiffCountsAndMissingNewline(t *testing.T) {
	for _, tt := range []struct {
		before, after string
		add, del      int
	}{
		{"a\nb\n", "a\nc\n", 1, 1}, {"", "new", 1, 0}, {"old", "", 0, 1}, {"same\n", "same\n", 0, 0},
	} {
		add, del := LineCounts(tt.before, tt.after)
		if add != tt.add || del != tt.del {
			t.Fatalf("%q => %q: %d,%d", tt.before, tt.after, add, del)
		}
	}
	want := "--- file\n+++ file\n@@ -1,1 +1,1 @@\n-old\n\\ No newline at end of file\n+new\n\\ No newline at end of file\n"
	if got := UnifiedDiff("file", "old", "new"); got != want {
		t.Fatalf("%q", got)
	}
	if UnifiedDiff("file", "same", "same") != "" {
		t.Fatal("unchanged diff")
	}
	// Large unrelated middles use the bounded fallback without losing equal ends.
	before := "start\n" + strings.Repeat("old\n", 1001) + "end\n"
	after := "start\n" + strings.Repeat("new\n", 1001) + "end\n"
	if add, del := LineCounts(before, after); add != 1001 || del != 1001 {
		t.Fatal(add, del)
	}
}
