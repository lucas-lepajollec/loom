package loom

import (
	"strings"
	"testing"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/notify"
)

func TestCodemanPushTargetsAndRules(t *testing.T) {
	state := codemanPushState{working: map[string]time.Time{}, pending: map[string]notify.Message{}}
	cfg := notify.Default()
	now := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	state.observe(codemanPushFrame{"approval:pending", `{"item":{"id":"prompt/a","sessionId":"work","kind":"permission","message":"private command"}}`}, cfg, now)
	waiting := state.pending["codeman-approval-prompt/a"]
	if waiting.URL != "/#/inbox/prompt%2Fa" || strings.Contains(waiting.Body, "private") || waiting.Event.Summary != "" {
		t.Fatalf("unexpected approval notice: %#v", waiting)
	}
	state.observe(codemanPushFrame{"approval:resolved", `{"id":"prompt/a"}`}, cfg, now)
	if len(state.pending) != 0 {
		t.Fatal("resolved approval remained queued")
	}

	state.observe(codemanPushFrame{"session:working", `{"id":"work"}`}, cfg, now)
	state.observe(codemanPushFrame{"hook:stop", `{"sessionId":"work"}`}, cfg, now.Add(time.Minute))
	if len(state.pending) != 0 {
		t.Fatal("short turn notified")
	}
	state.observe(codemanPushFrame{"session:working", `{"sessionId":"work"}`}, cfg, now)
	state.observe(codemanPushFrame{"session:completion", `{"sessionId":"work","text":"private answer"}`}, cfg, now.Add(3*time.Minute))
	done := state.pending["codeman-session-work"]
	if done.URL != "/#/agents/s/work" || done.Event.DurationSeconds != 180 || strings.Contains(done.Body, "private") {
		t.Fatalf("unexpected completion notice: %#v", done)
	}
	state.observe(codemanPushFrame{"hook:stop", `{"sessionId":"work"}`}, cfg, now.Add(4*time.Minute))
	if state.pending[done.Tag].Event.At != done.Event.At {
		t.Fatal("duplicate completion replaced the first notice")
	}

	cfg.Rules.QuietHours = notify.QuietHours{Enabled: true, Start: "00:00", End: "00:00", Timezone: "UTC"}
	state.observe(codemanPushFrame{"approval:pending", `{"id":"quiet","sessionId":"work","kind":"idle"}`}, cfg, now)
	if len(state.pending) != 1 {
		t.Fatal("quiet hours did not suppress an approval notice")
	}
}
