// Package notify contains notification policy and outbound protocols; Loom
// supplies configuration, secrets, live requests and persistence.
package notify

import (
	"errors"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/events"
)

type Ntfy struct {
	Enabled   bool   `json:"enabled"`
	ServerURL string `json:"server_url"`
	Topic     string `json:"topic"`
	TokenSet  bool   `json:"token_set"`
}
type Webhook struct {
	Enabled   bool   `json:"enabled"`
	URL       string `json:"url"`
	SecretSet bool   `json:"secret_set"`
}
type Push struct {
	Enabled bool `json:"enabled"`
}
type QuietHours struct {
	Enabled  bool   `json:"enabled"`
	Start    string `json:"start"`
	End      string `json:"end"`
	Timezone string `json:"timezone"`
}
type Rules struct {
	Events                []events.Type `json:"events"`
	CompletedAfterSeconds int           `json:"completed_after_seconds"`
	IncludeSummaries      bool          `json:"include_summaries"`
	RateSeconds           int           `json:"rate_seconds"`
	QuietHours            QuietHours    `json:"quiet_hours"`
}
type Config struct {
	PublicBaseURL   string  `json:"public_base_url"`
	TokenTTLSeconds int     `json:"token_ttl_seconds"`
	Ntfy            Ntfy    `json:"ntfy"`
	Webhook         Webhook `json:"webhook"`
	Push            Push    `json:"push"`
	Rules           Rules   `json:"rules"`
}

func Default() Config {
	return Config{TokenTTLSeconds: 86400, Ntfy: Ntfy{ServerURL: "https://ntfy.sh"}, Rules: Rules{Events: []events.Type{events.TaskWaiting, events.TaskCompleted, events.TaskFailed, events.NodeOffline}, CompletedAfterSeconds: 120, RateSeconds: 10, QuietHours: QuietHours{Start: "22:00", End: "07:00", Timezone: "UTC"}}}
}
func HTTPURL(raw string) error {
	u, e := url.Parse(raw)
	if e != nil || u.Hostname() == "" || u.User != nil || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") || len(raw) > 2048 {
		return errors.New("valid http(s) URL required, without credentials or fragment")
	}
	return nil
}
func (c Config) Validate() error {
	if HTTPURL(c.PublicBaseURL) != nil {
		return errors.New("invalid public base URL")
	}
	u, _ := url.Parse(c.PublicBaseURL)
	if u.RawQuery != "" {
		return errors.New("public base URL must not have a query")
	}
	if HTTPURL(c.Ntfy.ServerURL) != nil || strings.ContainsAny(c.Ntfy.Topic, "/ ?#\r\n") || len(c.Ntfy.Topic) > 120 || (c.Ntfy.Enabled && c.Ntfy.Topic == "") {
		return errors.New("invalid ntfy server or topic")
	}
	if c.Webhook.URL != "" && HTTPURL(c.Webhook.URL) != nil || c.Webhook.Enabled && c.Webhook.URL == "" {
		return errors.New("invalid webhook URL")
	}
	if c.TokenTTLSeconds < 60 || c.TokenTTLSeconds > 86400 || c.Rules.RateSeconds < 1 || c.Rules.RateSeconds > 3600 || c.Rules.CompletedAfterSeconds < 0 || c.Rules.CompletedAfterSeconds > 86400 {
		return errors.New("invalid notification limits")
	}
	for _, e := range c.Rules.Events {
		switch e {
		case events.TaskStarted, events.TaskWaiting, events.TaskResumed, events.TaskCompleted, events.TaskFailed, events.NodeOnline, events.NodeOffline, events.EngineDown:
		default:
			return errors.New("unknown notification event")
		}
	}
	q := c.Rules.QuietHours
	if _, err := time.Parse("15:04", q.Start); err != nil {
		return errors.New("invalid quiet hours start")
	}
	if _, err := time.Parse("15:04", q.End); err != nil {
		return errors.New("invalid quiet hours end")
	}
	if _, err := time.LoadLocation(q.Timezone); err != nil {
		return errors.New("invalid quiet hours timezone")
	}
	return nil
}
func (r Rules) Allows(e events.Event, now time.Time) bool {
	found := false
	for _, t := range r.Events {
		if t == e.Type {
			found = true
		}
	}
	if !found {
		return false
	}
	if e.Type == events.TaskCompleted && e.DurationSeconds <= float64(r.CompletedAfterSeconds) {
		return false
	}
	q := r.QuietHours
	if q.Enabled {
		loc, err := time.LoadLocation(q.Timezone)
		if err != nil {
			return false
		}
		clock := now.In(loc).Format("15:04")
		if q.Start == q.End || (q.Start < q.End && clock >= q.Start && clock < q.End) || (q.Start > q.End && (clock >= q.Start || clock < q.End)) {
			return false
		}
	}
	return true
}

// Coalescer keeps at most one update per task, bounded even during a burst.
// Take returns one batch per rate window; callers send a digest for a burst.
type Coalescer struct {
	mu      sync.Mutex
	pending map[string]events.Event
	next    time.Time
}

func (c *Coalescer) Add(e events.Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pending == nil {
		c.pending = map[string]events.Event{}
	}
	key := e.TaskID + "/" + e.MachineID
	if key == "/" {
		key = string(e.Type)
	}
	if len(c.pending) < 64 || c.pending[key].ID != 0 {
		c.pending[key] = e
	}
}
func (c *Coalescer) Take(now time.Time, interval time.Duration) []events.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	if now.Before(c.next) || len(c.pending) == 0 {
		return nil
	}
	out := make([]events.Event, 0, len(c.pending))
	for _, e := range c.pending {
		out = append(out, e)
	}
	clear(c.pending)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	c.next = now.Add(interval)
	return out
}
func Redact(e events.Event, summaries bool) events.Event {
	e.Title = events.Text(e.Title, 120)
	if summaries {
		e.Summary = events.Text(e.Summary, 120)
	} else {
		e.Summary = ""
	}
	return e
}

func (c *Coalescer) Drop(e events.Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.pending, e.TaskID+"/"+e.MachineID)
}
