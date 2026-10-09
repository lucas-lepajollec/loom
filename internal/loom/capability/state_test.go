package capability

import (
	"testing"
	"time"
)

func TestFeatureDegradedRestoredAndBounded(t *testing.T) {
	var s State
	now := time.Now()
	if !s.Observe("agent:a", "resume", false, "probe_failed", now) {
		t.Fatal("missing degradation")
	}
	if s.Observe("agent:a", "resume", false, "probe_failed", now.Add(time.Minute)) {
		t.Fatal("duplicate degradation")
	}
	if s.Snapshot("agent:a")[0].Since != now.UnixMilli() {
		t.Fatal("since reset")
	}
	caps := s.Filter("agent:a", []string{"chat", "resume", "models"})
	if len(caps) != 2 || caps[0] != "chat" {
		t.Fatal(caps)
	}
	if !s.Observe("agent:a", "resume", true, "", now) || len(s.Snapshot("agent:a")) != 0 {
		t.Fatal("not restored")
	}
	if s.Observe("agent:a", "resume", true, "", now) {
		t.Fatal("duplicate restoration")
	}
}
