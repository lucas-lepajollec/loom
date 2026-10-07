package loom

import "testing"

func TestRewindLastRemovesTheLastExchange(t *testing.T) {
	testHome(t)
	m := newRuntimeSessions()
	s := RuntimeSession{ID: "rw", Title: "t", RuntimeID: "codex", Status: "idle",
		Messages: []Message{{Role: "user", Content: "first"}, {Role: "assistant", Content: "a1"}, {Role: "user", Content: "second"}, {Role: "assistant", Content: "a2"}},
		Turns:    []RuntimeTurnRecord{{MessageIndex: 1}, {MessageIndex: 3}}}
	s.NativeSessionID, s.NativeContext = "native", "hash"
	if err := putStoreJSON(bkRuntimeSessions, s.ID, s); err != nil {
		t.Fatal(err)
	}
	got, text, err := m.rewindLast("rw")
	if err != nil || text != "second" {
		t.Fatalf("%q %v", text, err)
	}
	if len(got.Messages) != 2 || len(got.Turns) != 1 || got.NativeSessionID != "" || got.NativeContext != "" {
		t.Fatalf("%+v", got)
	}
	stored, _ := m.get("rw")
	if len(stored.Messages) != 2 {
		t.Fatal("rewind not saved")
	}
}
