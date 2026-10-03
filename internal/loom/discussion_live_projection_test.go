package loom

import (
	"fmt"
	"strings"
	"testing"
)

func TestLiveProjectionRetainsImmutableUsageAndNativeProvenance(t *testing.T) {
	turn := RuntimeTurnRecord{RuntimeID: "codex", Usage: &RuntimeUsage{Input: 7}, Events: []HarnessEvent{{Index: 1, State: "ACTIVE"}}}
	stats := &StatsEvent{}
	events := liveRuntimeDiscussionEvents(StreamEvent{Stats: stats}, turn)
	turn.Usage.Input = 99
	turn.Events[0].State = "DONE"
	snapshot := events[0]["provenance"].(RuntimeTurnRecord)
	if snapshot.Usage.Input != 7 || snapshot.Events[0].State != "ACTIVE" {
		t.Fatal("live provenance mutated after publication")
	}
	text := liveRuntimeDiscussionEvents(StreamEvent{Content: "delta"}, turn)
	if len(text) != 1 || text[0]["text"] != "delta" || text[0]["provenance"] != nil {
		t.Fatal("text projection changed")
	}
}

func BenchmarkRuntimeDeltaProjection(b *testing.B) {
	s := RuntimeSession{Turns: []RuntimeTurnRecord{{RuntimeID: "codex"}}}
	for i := 0; i < 2000; i++ {
		s.Turns[0].ACPEvents = append(s.Turns[0].ACPEvents, DiscussionEvent{"type": "tool_delta", "text": strings.Repeat("x", 1000), "id": fmt.Sprint(i)})
	}
	e := StreamEvent{ACPEvent: DiscussionEvent{"type": "text_delta", "text": "new text"}}
	b.Run("history-snapshot", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			c := cloneRuntimeSession(s)
			_ = runtimeDiscussionEvents(e, c.Turns[0])
		}
	})
	b.Run("live-projection", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = liveRuntimeDiscussionEvents(e, s.Turns[0])
		}
	})
}
