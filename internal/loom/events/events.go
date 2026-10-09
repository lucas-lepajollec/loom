// Package events owns bounded, process-local domain observations. It never
// transports a prompt, answer, provider frame or credential.
package events

import (
	"strings"
	"sync"
	"time"
)

type Type string

const (
	TaskStarted        Type = "task.started"
	TaskWaiting        Type = "task.waiting"
	TaskResumed        Type = "task.resumed"
	TaskCompleted      Type = "task.completed"
	TaskFailed         Type = "task.failed"
	NodeOffline        Type = "node.offline"
	NodeOnline         Type = "node.online"
	EngineDown         Type = "engine.down"
	CapabilityDegraded Type = "capability.degraded"
	CapabilityRestored Type = "capability.restored"
)

type Event struct {
	Owner           string  `json:"owner,omitempty"`
	Capability      string  `json:"capability,omitempty"`
	Reason          string  `json:"reason,omitempty"`
	ID              uint64  `json:"id"`
	At              int64   `json:"at"`
	Type            Type    `json:"type"`
	TaskID          string  `json:"task_id,omitempty"`
	DiscussionID    string  `json:"discussion_id,omitempty"`
	MachineID       string  `json:"machine_id,omitempty"`
	Title           string  `json:"title"`
	Status          string  `json:"status"`
	RequestID       string  `json:"request_id,omitempty"`
	RequestKind     string  `json:"request_kind,omitempty"`
	Summary         string  `json:"summary,omitempty"`
	DurationSeconds float64 `json:"duration_seconds,omitempty"`
	Count           int     `json:"count,omitempty"`
}

func Text(s string, limit int) string {
	r := []rune(strings.Join(strings.Fields(s), " "))
	if len(r) > limit {
		r = r[:limit]
	}
	return string(r)
}

type Bus struct {
	mu       sync.Mutex
	ring     []Event
	capacity int
	seq      uint64
	subs     map[chan Event]bool
}

func New(capacity int) *Bus { return &Bus{capacity: max(1, capacity), subs: map[chan Event]bool{}} }
func (b *Bus) Publish(e Event) Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.seq++
	e.ID = b.seq
	e.At = time.Now().UnixMilli()
	e.Title = Text(e.Title, 120)
	e.Summary = Text(e.Summary, 120)
	b.ring = append(b.ring, e)
	if len(b.ring) > b.capacity {
		copy(b.ring, b.ring[1:])
		b.ring = b.ring[:b.capacity]
	}
	for c := range b.subs {
		select {
		case c <- e:
		default:
			close(c)
			delete(b.subs, c)
		}
	}
	return e
}
func (b *Bus) Snapshot(since uint64) (out []Event, latest uint64, gap bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	out = []Event{}
	latest = b.seq
	gap = since > b.seq || (since > 0 && len(b.ring) > 0 && since < b.ring[0].ID-1)
	for _, e := range b.ring {
		if e.ID > since || since > b.seq {
			out = append(out, e)
		}
	}
	return
}
func (b *Bus) Subscribe() (<-chan Event, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	c := make(chan Event, 64)
	b.subs[c] = true
	return c, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if b.subs[c] {
			delete(b.subs, c)
			close(c)
		}
	}
}
