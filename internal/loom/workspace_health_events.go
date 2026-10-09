package loom

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/events"
)

type domainHealth struct {
	mu    sync.Mutex
	known map[string]bool
}

// First observations establish a baseline; only changes publish transitions.
// An unloaded model is not an engine outage. Observations never start engines.
func (m *runtimeSessions) observeHealth(key, title, machine string, online bool, engine bool) {
	m.health.mu.Lock()
	if m.health.known == nil {
		m.health.known = map[string]bool{}
	}
	old, known := m.health.known[key]
	m.health.known[key] = online
	m.health.mu.Unlock()
	if !known || old == online {
		return
	}
	kind := events.NodeOnline
	status := "online"
	if !online {
		kind = events.NodeOffline
		status = "offline"
	}
	if engine {
		if online {
			return
		}
		kind = events.EngineDown
		status = "down"
	}
	m.events.Publish(events.Event{Type: kind, MachineID: machine, Title: title, Status: status})
}
func (s *notificationService) monitorHealth(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	sample := func() {
		if !usageVaultAccessStream() {
			return
		}
		for _, m := range loadRemoteMachines() {
			if ctx.Err() != nil {
				return
			}
			n := savedMachineNode(m)
			if n == nil || n.Direct || n.Role != "engine-node" {
				continue
			}
			check, cancel := context.WithTimeout(ctx, 5*time.Second)
			r, err := http.NewRequestWithContext(check, http.MethodGet, n.URL+"/api/ping", nil)
			online := false
			if err == nil {
				r.Header.Set("Authorization", "Bearer "+n.WebKey)
				if resp, err := nodeClient.Do(r); err == nil {
					online = resp.StatusCode == 200
					resp.Body.Close()
				}
			}
			cancel()
			if ctx.Err() == nil {
				s.manager.observeHealth("node:"+m.ID, m.Name, m.ID, online, false)
			}
		}
	}
	sample()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sample()
		}
	}
}
