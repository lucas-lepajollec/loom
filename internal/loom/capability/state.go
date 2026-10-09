// Package capability holds bounded feature-level health observations. A failed
// feature never changes the provider's installation or account state.
package capability

import (
	"sort"
	"sync"
	"time"
)

type Degraded struct {
	Capability string `json:"capability"`
	Reason     string `json:"reason"`
	Since      int64  `json:"since"`
}
type Probe struct {
	Capability string `json:"capability"`
	OK         bool   `json:"ok"`
	Reason     string `json:"reason,omitempty"`
}
type State struct {
	mu    sync.Mutex
	items map[string]map[string]Degraded
}

// Observe returns a transition only when the feature crosses the boundary.
func (s *State) Observe(owner, feature string, ok bool, reason string, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if owner == "" || feature == "" {
		return false
	}
	if s.items == nil {
		s.items = map[string]map[string]Degraded{}
	}
	old, exists := s.items[owner][feature]
	if ok {
		if !exists {
			return false
		}
		delete(s.items[owner], feature)
		if len(s.items[owner]) == 0 {
			delete(s.items, owner)
		}
		return true
	}
	if s.items[owner] == nil {
		if len(s.items) >= 512 {
			return false
		}
		s.items[owner] = map[string]Degraded{}
	}
	if len(s.items[owner]) >= 64 && !exists {
		return false
	}
	since := now.UnixMilli()
	if exists {
		since = old.Since
	}
	s.items[owner][feature] = Degraded{feature, reason, since}
	return !exists
}
func (s *State) Snapshot(owner string) []Degraded {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []Degraded{}
	for _, d := range s.items[owner] {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Capability < out[j].Capability })
	return out
}
func (s *State) Filter(owner string, features []string) []string {
	failed := s.Snapshot(owner)
	out := []string{}
	for _, feature := range features {
		allowed := true
		for _, d := range failed {
			if d.Capability == feature {
				allowed = false
				break
			}
		}
		if allowed {
			out = append(out, feature)
		}
	}
	return out
}
