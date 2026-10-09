package loom

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/events"
	"github.com/lucas-lepajollec/loom/internal/loom/notify"
	agent "github.com/lucas-lepajollec/loom/internal/loom/runtime"
)

const notifyConfigKey = "notifications_v1"

var notifySettingsMu sync.Mutex
var notifySecretMu sync.Mutex
var notificationClient = &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

// Subscription endpoints are browser-supplied, unlike explicit administrator
// webhook/ntfy destinations. Resolve at dial time and reject private addresses
// to prevent subscription-based SSRF, including DNS rebinding and redirects.
var notificationPushClient = &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }, Transport: &http.Transport{Proxy: nil, DialContext: publicPushDial}}

func publicPushDial(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	for _, ip := range ips {
		if !ip.IP.IsGlobalUnicast() || ip.IP.IsPrivate() || ip.IP.IsLoopback() || ip.IP.IsLinkLocalUnicast() || (&net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}).Contains(ip.IP) {
			return nil, errors.New("public push destination required")
		}
	}
	for _, ip := range ips {
		conn, err := (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
		if err == nil {
			return conn, nil
		}
	}
	return nil, errors.New("push destination unavailable")
}
func notificationConfig() (notify.Config, error) {
	c := notify.Default()
	raw, err := getBytesErr(bkState, notifyConfigKey)
	if err != nil {
		return c, err
	}
	if len(raw) > 0 {
		raw, err = decodeMemContent(raw)
		if err != nil || json.Unmarshal(raw, &c) != nil {
			return c, errors.New("notification configuration unavailable")
		}
	}
	return c, nil
}
func persistentNotificationSecret(id string, generate func() (string, error)) (string, error) {
	notifySecretMu.Lock()
	defer notifySecretMu.Unlock()
	if _, err := os.Stat(filepath.Join(providerSecretRoot(), id+".sealed")); err == nil {
		return readProviderSecret(id)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	secret, err := generate()
	if err != nil {
		return "", err
	}
	if err := sealProviderSecret(id, secret); err != nil {
		return "", err
	}
	return secret, nil
}
func notificationTokenKey() ([]byte, error) {
	s, err := persistentNotificationSecret("notify-actions", func() (string, error) {
		b := make([]byte, 32)
		_, e := rand.Read(b)
		return base64.RawURLEncoding.EncodeToString(b), e
	})
	if err != nil {
		return nil, err
	}
	return base64.RawURLEncoding.DecodeString(s)
}
func browserSecure(r *http.Request) (bool, string) {
	if r.URL.Query().Get("secure") == "false" {
		return false, "The page reports an insecure browser context"
	}
	// TLS termination follows the existing auth deployment switch. The UI must
	// explicitly report window.isSecureContext; false always wins above.
	if r.URL.Query().Get("secure") == "true" && os.Getenv("LOOM_COOKIE_SECURE") == "1" {
		return true, ""
	}
	u := r.URL
	host := u.Hostname()
	if host == "" {
		host = r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
	}
	ip := net.ParseIP(host)
	if r.TLS != nil || u.Scheme == "https" || host == "localhost" || ip != nil && ip.IsLoopback() {
		return true, ""
	}
	return false, "Web Push requires HTTPS or localhost; plain HTTP on a LAN is a preview"
}
func publicURLWarning(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "Set a public base URL reachable from the phone"
	}
	ip := net.ParseIP(u.Hostname())
	if u.Scheme != "https" || u.Hostname() == "localhost" || strings.HasSuffix(u.Hostname(), ".local") || ip != nil && (ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast()) {
		return "This LAN or HTTP address may be unreachable from your phone; use a reachable HTTPS address for links and actions"
	}
	return ""
}

type notificationService struct {
	manager   *runtimeSessions
	mu        sync.Mutex
	tokens    *notify.Tokens
	started   bool
	workers   sync.WaitGroup
	coalescer notify.Coalescer
	pending   []notify.Message
	lastError map[string]string
}

func (m *runtimeSessions) notifications(ctx context.Context) *notificationService {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.notify == nil {
		m.notify = &notificationService{manager: m, lastError: map[string]string{}}
	}
	s := m.notify
	// A mux without a lifecycle is an inert embedding/test surface. Production
	// web startup always passes its server lifecycle; it owns delivery workers.
	if ctx == nil || s.started {
		return s
	}
	s.started = true
	s.workers.Add(3)
	c, unsubscribe := m.events.Subscribe()
	go func() { defer s.workers.Done(); s.monitorHealth(ctx) }()
	go func() {
		defer s.workers.Done()
		defer func() { unsubscribe() }()
		var cursor uint64
		accept := func(e events.Event) {
			if e.ID <= cursor {
				return
			}
			cursor = e.ID
			if e.Type == events.TaskResumed || e.Type == events.TaskCompleted || e.Type == events.TaskFailed {
				s.coalescer.Drop(e)
			}
			cfg, err := notificationConfig()
			if err == nil && usageVaultAccessStream() && cfg.Rules.Allows(e, time.Now()) {
				s.coalescer.Add(e)
			}
		}
		for {
			select {
			case <-ctx.Done():
				return
			case e, ok := <-c:
				if !ok {
					unsubscribe()
					c, unsubscribe = m.events.Subscribe()
					replay, _, _ := m.events.Snapshot(cursor)
					for _, e := range replay {
						accept(e)
					}
					continue
				}
				accept(e)
			}
		}
	}()
	go func() {
		defer s.workers.Done()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				cfg, err := notificationConfig()
				if err != nil || !usageVaultAccessStream() {
					continue
				}
				batch := s.coalescer.Take(now, time.Duration(cfg.Rules.RateSeconds)*time.Second)
				if len(batch) == 0 {
					continue
				}
				kept := batch[:0]
				for _, e := range batch {
					answerable := true
					if e.Type == events.TaskWaiting {
						answerable = false
						if session, ok := s.manager.get(e.DiscussionID); ok {
							for _, r := range session.PendingRequests {
								if r.ID == e.RequestID {
									answerable = true
								}
							}
						}
					}
					if answerable && cfg.Rules.Allows(e, now) {
						kept = append(kept, e)
					}
				}
				if len(kept) == 0 {
					continue
				}
				e := kept[0]
				if len(kept) > 1 {
					e = events.Event{Type: e.Type, At: now.UnixMilli(), Title: "Loom", Status: fmt.Sprintf("%d task updates", len(kept)), Count: len(kept)}
				}
				s.deliver(ctx, cfg, e, "")
			}
		}
	}()
	return s
}
func (s *notificationService) actionTokens() (*notify.Tokens, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tokens != nil {
		return s.tokens, nil
	}
	key, err := notificationTokenKey()
	if err != nil {
		return nil, err
	}
	s.tokens = notify.NewTokens(key)
	return s.tokens, nil
}
func requestChoices(r agent.AgentRequest) ([]string, []agent.RequestAnswer) {
	labels := []string{}
	answers := []agent.RequestAnswer{}
	if r.Kind == "approval" {
		for _, o := range r.Options {
			labels = append(labels, events.Text(o.Label, 120))
			answers = append(answers, agent.RequestAnswer{Decision: o.ID})
		}
	}
	if r.Kind == "user_input" && len(r.Questions) == 1 {
		q := r.Questions[0]
		if !q.MultiSelect && !q.Secret && len(q.Options) > 0 && len(q.Options) <= 3 {
			for _, o := range q.Options {
				labels = append(labels, events.Text(o.Label, 120))
				answers = append(answers, agent.RequestAnswer{Answers: map[string][]string{q.ID: {o.ID}}})
			}
		}
	}
	return labels, answers
}
func (s *notificationService) message(c notify.Config, e events.Event) notify.Message {
	e = notify.Redact(e, c.Rules.IncludeSummaries)
	base := strings.TrimRight(c.PublicBaseURL, "/")
	link := base + "/#/tasks"
	if e.DiscussionID != "" {
		link = base + "/#/chat/" + url.PathEscape(e.DiscussionID)
	}
	body := e.Status
	if e.Summary != "" {
		body += " · " + e.Summary
	}
	msg := notify.Message{Title: e.Title, Body: body, URL: link, Tag: e.TaskID, Event: e}
	if msg.Title == "" {
		msg.Title = "Loom"
	}
	if msg.Tag == "" {
		msg.Tag = "loom-tasks"
	}
	msg.Actions = []notify.Action{{Action: "view", Label: "Open", URL: link}}
	if e.Type == events.TaskWaiting && e.RequestID != "" {
		if session, ok := s.manager.get(e.DiscussionID); ok {
			for _, r := range session.PendingRequests {
				if r.ID != e.RequestID {
					continue
				}
				labels, answers := requestChoices(r)
				if len(answers) == 0 {
					break
				}
				tokens, err := s.actionTokens()
				if err != nil {
					break
				}
				actions := []notify.Action{}
				for i, a := range answers {
					token, err := tokens.Issue(session.ID, r.ID, a, time.Now(), time.Duration(c.TokenTTLSeconds)*time.Second)
					if err != nil {
						continue
					}
					actions = append(actions, notify.Action{Action: "http", Label: labels[i], URL: base + "/api/notify/answer/" + token, Method: "POST", Clear: true})
				}
				if len(actions) > 0 {
					msg.Actions = actions
				}
				break
			}
		}
	}
	return msg
}
func (s *notificationService) recordError(channel string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.lastError, channel)
	if err != nil {
		s.lastError[channel] = err.Error()
	}
}
func (s *notificationService) deliver(ctx context.Context, c notify.Config, e events.Event, only string) error {
	if only == "" && !c.Ntfy.Enabled && !c.Webhook.Enabled && !c.Push.Enabled {
		return nil
	}
	m := s.message(c, e)
	var failures []error
	send := func(channel string, fn func() error) {
		err := fn()
		s.recordError(channel, err)
		if err != nil {
			failures = append(failures, err)
		}
	}
	if only == "ntfy" || only == "" && c.Ntfy.Enabled {
		send("ntfy", func() error {
			token := ""
			if c.Ntfy.TokenSet {
				var err error
				token, err = readProviderSecret("notify-ntfy")
				if err != nil {
					return errors.New("ntfy credential unavailable")
				}
			}
			if c.Ntfy.Topic == "" {
				return errors.New("configure an ntfy topic")
			}
			return notify.SendNtfy(ctx, notificationClient, c.Ntfy, token, m)
		})
	}
	if only == "webhook" || only == "" && c.Webhook.Enabled {
		send("webhook", func() error {
			secret := ""
			if c.Webhook.SecretSet {
				var err error
				secret, err = readProviderSecret("notify-webhook")
				if err != nil {
					return errors.New("webhook credential unavailable")
				}
			}
			return notify.SendWebhook(ctx, notificationClient, c.Webhook, secret, m.Event)
		})
	}
	if only == "push" || only == "" && c.Push.Enabled {
		send("push", func() error {
			s.mu.Lock()
			s.pending = append(s.pending, m)
			if len(s.pending) > 64 {
				s.pending = s.pending[len(s.pending)-64:]
			}
			s.mu.Unlock()
			subs := loadSubs()
			if len(subs) == 0 {
				return errors.New("subscribe a secure browser before testing Web Push")
			}
			priv, pub, err := vapidKeys()
			if err != nil {
				return errors.New("VAPID keys unavailable")
			}
			var errs []error
			for _, sub := range subs {
				gone, err := notify.SendTickle(ctx, notificationPushClient, sub.Endpoint, c.PublicBaseURL, priv, pub)
				if gone {
					removeSub(sub.Endpoint)
				}
				if err != nil {
					errs = append(errs, err)
				}
			}
			return errors.Join(errs...)
		})
	}
	return errors.Join(failures...)
}
func notificationConfigEnvelope(s *notificationService, c notify.Config, r *http.Request) (map[string]any, error) {
	secure, reason := browserSecure(r)
	pub := ""
	if secure {
		_, key, err := vapidKeys()
		if err != nil {
			return nil, errors.New("notification keys unavailable")
		}
		pub = key
	}
	s.mu.Lock()
	errs := map[string]string{}
	for k, v := range s.lastError {
		errs[k] = v
	}
	s.mu.Unlock()
	return map[string]any{"ok": true, "config": c, "secure": secure, "reason": reason, "public_url_warning": publicURLWarning(c.PublicBaseURL), "push_public_key": pub, "push_subscription_count": len(loadSubs()), "last_errors": errs}, nil
}
func registerNotifications(mux *http.ServeMux, api func(string, http.HandlerFunc), ctx context.Context) {
	s := workspaceSessions.notifications(ctx)
	api("/api/notify", func(w http.ResponseWriter, r *http.Request) { s.handleConfig(w, r) })
	api("/api/notify/test", func(w http.ResponseWriter, r *http.Request) { s.handleTest(w, r) })
	api("/api/notify/pending", func(w http.ResponseWriter, r *http.Request) { s.handlePending(w, r) })
	api("/api/notify/push/subscribe", handlePushSubscribe)
	api("/api/notify/push/unsubscribe", handlePushUnsubscribe)
	// This route alone accepts an action capability, without cookie/Bearer auth.
	// The token is never interpreted by the ordinary API authentication layer.
	mux.HandleFunc("/api/notify/answer/{token}", s.handleAnswer)
}
func (s *notificationService) handleConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet && !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	if !usageVaultAccess(w) {
		return
	}
	notifySettingsMu.Lock()
	defer notifySettingsMu.Unlock()
	c, err := notificationConfig()
	if err != nil {
		webAuthUnavailable(w)
		return
	}
	if r.Method == http.MethodPost {
		var body struct {
			Config        notify.Config `json:"config"`
			NtfyToken     *string       `json:"ntfy_token,omitempty"`
			WebhookSecret *string       `json:"webhook_secret,omitempty"`
		}
		if !workspaceDecode(w, r, &body) {
			return
		}
		next := body.Config
		next.Ntfy.TokenSet = c.Ntfy.TokenSet
		next.Webhook.SecretSet = c.Webhook.SecretSet
		if next.PublicBaseURL == "" {
			scheme := "http"
			if r.TLS != nil {
				scheme = "https"
			}
			next.PublicBaseURL = scheme + "://" + r.Host
		}
		next.PublicBaseURL = strings.TrimRight(next.PublicBaseURL, "/")
		if err := next.Validate(); err != nil {
			sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		if next.Push.Enabled {
			if secure, _ := browserSecure(r); !secure {
				sendJSON(w, 400, map[string]any{"ok": false, "error": "Web Push requires a secure browser context"})
				return
			}
			if u, _ := url.Parse(next.PublicBaseURL); u.Scheme != "https" {
				sendJSON(w, 400, map[string]any{"ok": false, "error": "Web Push requires an HTTPS public base URL"})
				return
			}
		}
		for _, value := range []*string{body.NtfyToken, body.WebhookSecret} {
			if value != nil && (len(*value) > 4096 || strings.ContainsAny(*value, "\r\n\x00")) {
				sendJSON(w, 400, map[string]any{"ok": false, "error": "invalid notification secret"})
				return
			}
		}
		for _, secret := range []struct {
			id    string
			value *string
			set   *bool
		}{{"notify-ntfy", body.NtfyToken, &next.Ntfy.TokenSet}, {"notify-webhook", body.WebhookSecret, &next.Webhook.SecretSet}} {
			if secret.value != nil {
				if *secret.value == "" {
					err = deleteProviderSecret(secret.id)
				} else {
					err = sealProviderSecret(secret.id, *secret.value)
				}
				if err != nil {
					webAuthUnavailable(w)
					return
				}
				*secret.set = *secret.value != ""
			}
		}
		if err = putStoreJSON(bkState, notifyConfigKey, next); err != nil {
			webAuthUnavailable(w)
			return
		}
		c = next
	}
	envelope, err := notificationConfigEnvelope(s, c, r)
	if err != nil {
		webAuthUnavailable(w)
		return
	}
	sendJSON(w, 200, envelope)
}
func (s *notificationService) handleTest(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) || !usageVaultAccess(w) {
		return
	}
	var body struct {
		Channel string `json:"channel"`
	}
	if !workspaceDecode(w, r, &body) {
		return
	}
	if body.Channel != "ntfy" && body.Channel != "webhook" && body.Channel != "push" {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "unknown notification channel"})
		return
	}
	if body.Channel == "push" {
		if secure, _ := browserSecure(r); !secure {
			sendJSON(w, 400, map[string]any{"ok": false, "error": "Web Push requires a secure context"})
			return
		}
	}
	c, err := notificationConfig()
	if err != nil {
		webAuthUnavailable(w)
		return
	}
	if err = s.deliver(r.Context(), c, events.Event{Type: events.TaskCompleted, At: time.Now().UnixMilli(), Title: "Loom test", Status: "Notification test"}, body.Channel); err != nil {
		sendJSON(w, 502, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true})
}
func (s *notificationService) handleAnswer(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	w.Header().Set("Referrer-Policy", "no-referrer")
	// No body is consumed: the only accepted answer is bound into the signature.
	// An action cannot become a generic request-writing API.
	s.mu.Lock()
	tokens := s.tokens
	s.mu.Unlock()
	if tokens == nil {
		sendJSON(w, 409, map[string]any{"ok": false, "error": "invalid, used or expired action"})
		return
	}
	err := tokens.Resolve(r.PathValue("token"), time.Now(), func(c notify.Claim) error { return s.manager.answerRequest(c.SessionID, c.RequestID, c.Answer) })
	if err != nil {
		sendJSON(w, 409, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true})
}
func (s *notificationService) handlePending(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodGet) || !usageVaultAccess(w) {
		return
	}
	cfg, err := notificationConfig()
	if err != nil {
		webAuthUnavailable(w)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []notify.Message{}
	for _, m := range s.pending {
		if m.Event.At > time.Now().Add(-24*time.Hour).UnixMilli() {
			m.Actions = nil
			if !cfg.Rules.IncludeSummaries {
				m.Event.Summary = ""
				m.Body = m.Event.Status
			}
			out = append(out, m)
		}
	}
	sendJSON(w, 200, map[string]any{"ok": true, "notifications": out})
}
