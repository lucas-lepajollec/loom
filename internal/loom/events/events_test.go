package events

import (
	"strings"
	"testing"
)

func TestRingCursorBoundsAndSlowSubscriber(t *testing.T) {
	b := New(3)
	c, stop := b.Subscribe()
	defer stop()
	for i := 0; i < 100; i++ {
		b.Publish(Event{Type: TaskWaiting, Title: strings.Repeat("🙂", 200), Summary: strings.Repeat("s", 1000)})
	}
	list, latest, gap := b.Snapshot(1)
	if len(list) != 3 || latest != 100 || !gap || list[0].ID != 98 {
		t.Fatal(list, latest, gap)
	}
	for _, e := range list {
		if len([]rune(e.Title)) != 120 || len(e.Summary) != 120 {
			t.Fatal("unbounded payload")
		}
	}
	_, _, gap = b.Snapshot(100)
	if gap {
		t.Fatal("current cursor gap")
	}
	_, _, gap = b.Snapshot(999)
	if !gap {
		t.Fatal("restart cursor not detected")
	}
	n := 0
	for range c {
		n++
	}
	if n != 64 {
		t.Fatal("slow consumer was not disconnected", n)
	}
	b.Publish(Event{Type: TaskCompleted})
	stop()
}
