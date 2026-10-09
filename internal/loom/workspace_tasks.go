package loom

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/events"
	agent "github.com/lucas-lepajollec/loom/internal/loom/runtime"
	"github.com/lucas-lepajollec/loom/internal/loom/web"
)

type taskRequest struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Summary string `json:"summary"`
}
type taskView struct {
	ID               string        `json:"id"`
	DiscussionID     string        `json:"discussion_id"`
	Title            string        `json:"title"`
	ProjectID        string        `json:"project_id"`
	Executor         string        `json:"executor"`
	Model            string        `json:"model"`
	Status           string        `json:"status"`
	StartedAt        int64         `json:"started_at"`
	FinishedAt       int64         `json:"finished_at,omitempty"`
	ElapsedSeconds   float64       `json:"elapsed_seconds"`
	LastActivityAt   int64         `json:"last_activity_at"`
	LastActivityText string        `json:"last_activity_text"`
	StepsStarted     *int          `json:"steps_started"`
	StepsCompleted   *int          `json:"steps_completed"`
	Requests         []taskRequest `json:"requests"`
}

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
func (m *runtimeSessions) tasks(now time.Time) []taskView {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []taskView{}
	sessions := []RuntimeSession{}
	nativeOwned := map[string]bool{}
	for id := range allKV(bkRuntimeSessions) {
		s, ok := m.getLocked(id)
		if !ok {
			continue
		}
		sessions = append(sessions, s)
		if s.NativeArchive != "" {
			nativeOwned[s.NativeArchive] = true
		}
	}
	// The original Conversation/archive pipeline also owns discussions that
	// have not yet been bound to a workspace session. Project them once.
	for _, meta := range listArchives() {
		if nativeOwned[meta.ID] || meta.SavedAt < now.Add(-24*time.Hour).UnixMilli() {
			continue
		}
		a, ok := loadArchive(meta.ID)
		if !ok {
			continue
		}
		sessions = append(sessions, RuntimeSession{ID: a.ID, NativeArchive: a.ID, RuntimeID: "llama.cpp", Model: ReadConfig()["MODEL"], ProjectID: a.ProjectID, Title: a.Title, Status: "idle", UpdatedAt: a.SavedAt, Turns: nativeTurnRecords(a)})
	}
	for _, s := range sessions {
		id := s.ID
		if s.NativeArchive != "" && s.RuntimeID == "llama.cpp" {
			// The native Conversation is server-owned too. Snapshot outside its lock
			// before projecting it; native records retain the original executor/model.
			conv.mu.Lock()
			live := conv.ID == s.NativeArchive
			running := live && conv.Generating
			start := conv.genStart.UnixMilli()
			conv.mu.Unlock()
			if live {
				if a := conv.snapshotForSession(); a != nil {
					s.Turns = nativeTurnRecords(a)
					if running {
						s.Status = "running"
						s.Turns = append(s.Turns, nativeTaskProgress(a, start, s.Model))
					}
				}
			}
		}
		for i, t := range s.Turns {
			active := i == len(s.Turns)-1 && s.Status == "running"
			if t.StartedAt == 0 {
				continue
			} // historical unknown time stays unknown
			status := t.Outcome
			if i == len(s.Turns)-1 && s.Status == "unsaved" {
				status = "failed"
			}
			finished := t.FinishedAt
			if active {
				status = "running"
				finished = 0
			} else if status == "" && i == len(s.Turns)-1 {
				status = taskStatus(s.Status)
				finished = s.UpdatedAt
			}
			if !active && (finished < now.Add(-24*time.Hour).UnixMilli() || finished == 0) {
				continue
			}
			row := taskView{ID: taskID(id, t.StartedAt), DiscussionID: id, Title: events.Text(s.Title, 120), ProjectID: s.ProjectID, Executor: t.RuntimeID, Model: t.Model, Status: status, StartedAt: t.StartedAt, FinishedAt: finished, LastActivityAt: t.ActivityAt, LastActivityText: t.ActivityText, Requests: []taskRequest{}}
			if t.ActivityAt > 0 || t.Outcome != "" {
				started, completed := t.StepsStarted, t.StepsCompleted
				row.StepsStarted, row.StepsCompleted = &started, &completed
			}
			end := finished
			if active {
				end = now.UnixMilli()
			}
			row.ElapsedSeconds = float64(max(0, end-t.StartedAt)) / 1000
			if active {
				for _, r := range s.PendingRequests {
					row.Requests = append(row.Requests, taskRequest{r.ID, r.Kind, requestSummary(r)})
					row.Status = "waiting_input"
					if r.Kind == "approval" {
						row.Status = "waiting_approval"
					}
				}
			}
			if row.LastActivityText == "" {
				row.LastActivityText = row.Status
				row.LastActivityAt = s.UpdatedAt
			}
			out = append(out, row)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a := out[i].FinishedAt == 0
		b := out[j].FinishedAt == 0
		if a != b {
			return a
		}
		if a {
			return out[i].StartedAt > out[j].StartedAt
		}
		return out[i].FinishedAt > out[j].FinishedAt
	})
	active := 0
	for _, t := range out {
		if t.FinishedAt == 0 {
			active++
		}
	}
	if len(out) > active+100 {
		out = out[:active+100]
	}
	return out
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
func tasksEnvelope(m *runtimeSessions) map[string]any {
	return map[string]any{"ok": true, "tasks": m.tasks(time.Now()), "recent_window_seconds": 86400, "recent_limit": 100}
}
func handleTasks(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodGet) || !usageVaultAccess(w) {
		return
	}
	sendJSON(w, 200, tasksEnvelope(workspaceSessions))
}
func handleTasksStream(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodGet) || !usageVaultAccess(w) {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		sendJSON(w, 500, map[string]any{"ok": false, "error": "stream unavailable"})
		return
	}
	web.SSEHeaders(w, "no-store")
	mu, stop := sseHeartbeat(w, flusher)
	defer stop()
	m := workspaceSessions
	c, unsubscribe := m.events.Subscribe()
	defer unsubscribe()
	send := func() bool {
		if !usageVaultAccessStream() {
			return false
		}
		b, _ := json.Marshal(tasksEnvelope(m))
		return web.WriteSSE(w, flusher, mu, b) == nil
	}
	if !send() {
		return
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case _, open := <-c:
			if !open || !send() {
				return
			}
		case <-ticker.C:
			if !send() {
				return
			}
		}
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

func nativeTaskProgress(a *convArchive, started int64, model string) RuntimeTurnRecord {
	t := RuntimeTurnRecord{RuntimeID: "llama.cpp", Model: model, StartedAt: started, ActivityAt: started, ActivityText: "Working"}
	pending := false
	for _, e := range a.Log {
		if e.TS < started {
			continue
		}
		t.ActivityAt = e.TS
		if e.Delta["content"] != nil {
			t.ActivityText = "Writing response"
		}
		if tool, ok := e.Delta["tool_used"].(map[string]any); ok && tool["typing"] != true {
			done := tool["done"] == true
			if !done || !pending {
				t.StepsStarted++
			}
			if done {
				t.StepsCompleted++
			}
			pending = !done
			t.ActivityText = "Using a tool"
		}
	}
	return t
}
