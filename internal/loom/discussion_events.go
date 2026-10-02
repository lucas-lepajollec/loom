package loom

import (
	"context"
	"strings"
)

type discussionSubscriber struct{ events chan DiscussionEvent }

// A slow reader reconnects from a snapshot. It never blocks generation.
func (m *runtimeSessions) publishLocked(id string, events ...DiscussionEvent) {
	for sub := range m.subscribers[id] {
		for _, event := range events {
			select {
			case sub.events <- event:
			default:
				close(sub.events)
				delete(m.subscribers[id], sub)
				break
			}
			if _, ok := m.subscribers[id][sub]; !ok {
				break
			}
		}
	}
}

func (m *runtimeSessions) subscribeDiscussion(ctx context.Context, id string, emit func(DiscussionEvent) bool) bool {
	m.mu.Lock()
	s, ok := m.getLocked(id)
	if !ok {
		m.mu.Unlock()
		return false
	}
	sub := &discussionSubscriber{events: make(chan DiscussionEvent, 256)}
	if m.subscribers[id] == nil {
		m.subscribers[id] = map[*discussionSubscriber]bool{}
	}
	m.subscribers[id][sub] = true
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.subscribers[id], sub)
		if len(m.subscribers[id]) == 0 {
			delete(m.subscribers, id)
		}
		m.mu.Unlock()
	}()
	if !emit(DiscussionEvent{"pad": strings.Repeat("·", 2048)}) {
		return true
	}
	for _, e := range runtimeReplay(s) {
		if ctx.Err() != nil || !emit(e) {
			return true
		}
	}
	for {
		select {
		case <-ctx.Done():
			return true
		case event, open := <-sub.events:
			if !open || !emit(event) {
				return true
			}
		}
	}
}
