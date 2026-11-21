package loom

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/events"
	"github.com/lucas-lepajollec/loom/internal/loom/notify"
)

type codemanPushFrame struct {
	name string
	data string
}

// Use the gateway's configured account and HTTP client. The public SSE filter
// excludes the terminal output stream.
func readCodemanPush(ctx context.Context, base, auth string, frames chan<- codemanPushFrame) {
	defer close(frames)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	watchdog := time.AfterFunc(45*time.Second, cancel)
	defer watchdog.Stop()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/v1/events?sessions=loom-no-terminal-stream", nil)
	if err != nil {
		return
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", auth)
	res, err := codemanHTTP(base).Do(req)
	if err != nil {
		return
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/event-stream") {
		return
	}
	var name string
	var data strings.Builder
	scanner := bufio.NewScanner(res.Body)
	scanner.Buffer(make([]byte, 8192), 512<<10)
	for scanner.Scan() {
		watchdog.Reset(45 * time.Second)
		line := scanner.Text()
		if line == "" {
			if name != "" && data.Len() > 0 {
				select {
				case frames <- codemanPushFrame{name, data.String()}:
				case <-ctx.Done():
					return
				}
			}
			name = ""
			data.Reset()
		} else if v, ok := strings.CutPrefix(line, "event:"); ok {
			switch v = strings.TrimSpace(v); v {
			case "approval:pending", "approval:resolved", "session:working", "session:deleted", "session:exit", "session:completion", "hook:stop", "transcript:complete":
				name = v
			}
		} else if v, ok := strings.CutPrefix(line, "data:"); ok && name != "" {
			if data.Len()+len(v) > 512<<10 {
				return
			}
			data.WriteString(strings.TrimPrefix(v, " "))
			data.WriteByte('\n')
		}
	}
}

type codemanPushItem struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionId"`
	Kind      string `json:"kind"`
}

type codemanPushState struct {
	working map[string]time.Time
	pending map[string]notify.Message
}

func (s *codemanPushState) observe(frame codemanPushFrame, cfg notify.Config, now time.Time) {
	var data struct {
		codemanPushItem
		Item *codemanPushItem `json:"item"`
	}
	if json.Unmarshal([]byte(frame.data), &data) != nil {
		return
	}
	item := data.codemanPushItem
	if data.Item != nil && item.ID == "" {
		item = *data.Item
	}
	id := item.SessionID
	if id == "" && !strings.HasPrefix(frame.name, "approval:") {
		id = item.ID
	}
	if frame.name == "approval:resolved" {
		delete(s.pending, "codeman-approval-"+item.ID)
		return
	}
	if id == "" || len(id) > 256 {
		return
	}
	switch frame.name {
	case "session:working":
		if s.working[id].IsZero() && len(s.working) < 512 {
			s.working[id] = now
		}
		return
	case "session:exit", "session:deleted":
		delete(s.working, id)
		delete(s.pending, "codeman-session-"+id)
		return
	}
	e := events.Event{At: now.UnixMilli(), Title: "Loom"}
	var route string
	switch frame.name {
	case "approval:pending":
		if item.ID == "" || len(item.ID) > 256 || (item.Kind != "permission" && item.Kind != "question" && item.Kind != "idle") {
			return
		}
		e.Type, e.Status = events.TaskWaiting, "An agent needs your attention"
		e.TaskID = "codeman-approval-" + item.ID
		route = "/#/inbox/" + url.PathEscape(item.ID)
	case "session:completion", "hook:stop", "transcript:complete":
		start, observed := s.working[id]
		delete(s.working, id)
		if !observed {
			return
		}
		e.Type, e.Status = events.TaskCompleted, "An agent finished its turn"
		e.TaskID = "codeman-session-" + id
		e.DurationSeconds = now.Sub(start).Seconds()
		route = "/#/agents/s/" + url.PathEscape(id)
	default:
		return
	}
	if (len(s.pending) < 64 || s.pending[e.TaskID].Tag != "") && cfg.Rules.Allows(e, now) {
		s.pending[e.TaskID] = notify.Message{Title: e.Title, Body: e.Status, URL: route, Tag: e.TaskID, Event: e}
	}
}

func (s *notificationService) monitorCodemanPush(ctx context.Context) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	var frames <-chan codemanPushFrame
	var cancel context.CancelFunc
	var server, auth string
	var reconnect, next time.Time
	cfg := notify.Default()
	state := codemanPushState{working: map[string]time.Time{}, pending: map[string]notify.Message{}}
	defer func() {
		if cancel != nil {
			cancel()
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case frame, ok := <-frames:
			if !ok {
				cancel()
				frames = nil
				clear(state.working)
				continue
			}
			state.observe(frame, cfg, time.Now())
		case now := <-tick.C:
			var desired, credential string
			nextCfg, err := notificationConfig()
			if err == nil && nextCfg.Push.Enabled && usageVaultAccessStream() && len(loadSubs()) > 0 {
				c := loadCodemanConfig()
				credential = codemanAuthHeader(c)
				if credential != "" {
					desired = c.URL
				}
			}
			cfg = nextCfg
			if server != desired || auth != credential {
				if cancel != nil {
					cancel()
				}
				server, auth, frames = desired, credential, nil
				clear(state.working)
				clear(state.pending)
				reconnect = time.Time{}
			}
			if server == "" {
				continue
			}
			if frames == nil && !now.Before(reconnect) {
				var streamCtx context.Context
				streamCtx, cancel = context.WithCancel(ctx)
				stream := make(chan codemanPushFrame, 16)
				frames, reconnect = stream, now.Add(5*time.Second)
				go readCodemanPush(streamCtx, server, auth, stream)
			}
			if now.Before(next) || len(state.pending) == 0 {
				continue
			}
			batch := make([]notify.Message, 0, len(state.pending))
			for _, m := range state.pending {
				if cfg.Rules.Allows(m.Event, now) {
					batch = append(batch, m)
				}
			}
			clear(state.pending)
			if len(batch) > 0 {
				next = now.Add(time.Duration(cfg.Rules.RateSeconds) * time.Second)
				s.recordError("push", s.deliverPush(ctx, cfg, batch))
			}
		}
	}
}
