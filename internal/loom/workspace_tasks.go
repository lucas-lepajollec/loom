package loom

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/events"
	agent "github.com/lucas-lepajollec/loom/internal/loom/runtime"
)

func taskID(id string, started int64) string { return fmt.Sprintf("%s:%d", id, started) }
func requestSummary(r agent.AgentRequest) string {
	for _, q := range r.Questions {
		if q.Secret {
			return "Input required"
		}
	}
	s := r.Message
	if s == "" && len(r.Questions) > 0 {
		s = r.Questions[0].Question
	}
	if s == "" {
		s = r.Kind + " required"
	}
	return events.Text(s, 120)
}
func (m *runtimeSessions) taskEventLocked(s RuntimeSession, kind events.Type, r *agent.AgentRequest) {
	if len(s.Turns) == 0 {
		return
	}
	t := s.Turns[len(s.Turns)-1]
	e := events.Event{Type: kind, TaskID: taskID(s.ID, t.StartedAt), DiscussionID: s.ID, Title: s.Title, Status: s.Status, DurationSeconds: t.DurationSeconds}
	if r != nil {
		e.Status = "waiting_input"
		if r.Kind == "approval" {
			e.Status = "waiting_approval"
		}
		e.RequestID = r.ID
		e.RequestKind = r.Kind
		e.Summary = requestSummary(*r)
	}
	m.events.Publish(e)
}
func taskStatus(s string) string {
	switch s {
	case "complete", "idle":
		return "done"
	case "cancelled":
		return "cancelled"
	default:
		return "failed"
	}
}
func usageVaultAccessStream() bool { // same guard used by read APIs; never send data after vault lock
	return !memEncActive() || memUnlocked()
}
func handleDomainEvents(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodGet) || !usageVaultAccess(w) {
		return
	}
	since := uint64(0)
	if raw := r.URL.Query().Get("since"); raw != "" {
		n, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			sendJSON(w, 400, map[string]any{"ok": false, "error": "invalid event cursor"})
			return
		}
		since = n
	}
	list, latest, gap := workspaceSessions.events.Snapshot(since)
	sendJSON(w, 200, map[string]any{"ok": true, "events": list, "latest": latest, "gap": gap})
}

func (c *Conversation) publishTask(kind events.Type, started time.Time, status string, turn *RuntimeTurnRecord) {
	a := c.snapshotForSession()
	if a == nil {
		return
	}
	s := nativeContextSession(a.ID, a.ProjectID)
	s.Title = a.Title
	s.Status = status
	if turn == nil {
		turn = &RuntimeTurnRecord{RuntimeID: "llama.cpp", Model: s.Model, StartedAt: started.UnixMilli()}
	}
	s.Turns = []RuntimeTurnRecord{*turn}
	workspaceSessions.taskEventLocked(s, kind, nil)
}
