package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/lucas-lepajollec/loom/internal/loom/events"
)

type Action struct {
	Action string `json:"action"`
	Label  string `json:"label"`
	URL    string `json:"url"`
	Method string `json:"method,omitempty"`
	Clear  bool   `json:"clear,omitempty"`
}
type Message struct {
	Title   string       `json:"title"`
	Body    string       `json:"body"`
	URL     string       `json:"url"`
	Tag     string       `json:"tag"`
	Event   events.Event `json:"event"`
	Actions []Action     `json:"actions,omitempty"`
}
type NtfyPayload struct {
	Topic    string   `json:"topic"`
	Title    string   `json:"title"`
	Message  string   `json:"message"`
	Priority int      `json:"priority"`
	Click    string   `json:"click"`
	Actions  []Action `json:"actions,omitempty"`
}

func SendNtfy(ctx context.Context, client *http.Client, c Ntfy, token string, m Message) error {
	priority := 3
	if m.Event.Type == events.TaskWaiting || m.Event.Type == events.TaskFailed {
		priority = 4
	}
	// ntfy accepts at most three actions per publish. Split approvals with
	// more options so every offered decision remains available.
	actions := m.Actions
	for {
		group := actions
		if len(group) > 3 {
			group = group[:3]
		}
		err := post(ctx, client, c.ServerURL, NtfyPayload{c.Topic, m.Title, m.Body, priority, m.URL, group}, func(r *http.Request) {
			if token != "" {
				r.Header.Set("Authorization", "Bearer "+token)
			}
		})
		if err != nil {
			return err
		}
		if len(actions) <= 3 {
			return nil
		}
		actions = actions[3:]
	}
}

// The shared secret is carried in X-Loom-Secret for existing webhook receivers;
// X-Loom-Signature is HMAC-SHA256 over the exact transmitted JSON bytes.
func SendWebhook(ctx context.Context, client *http.Client, c Webhook, secret string, e events.Event) error {
	raw, _ := json.Marshal(e)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(raw)
	return postRaw(ctx, client, c.URL, raw, func(r *http.Request) {
		if secret != "" {
			r.Header.Set("X-Loom-Secret", secret)
		}
		r.Header.Set("X-Loom-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	})
}
func post(ctx context.Context, client *http.Client, dest string, value any, headers func(*http.Request)) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return postRaw(ctx, client, dest, b, headers)
}
func postRaw(ctx context.Context, client *http.Client, dest string, raw []byte, headers func(*http.Request)) error {
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, dest, bytes.NewReader(raw))
	if err != nil {
		return errors.New("invalid notification destination")
	}
	r.Header.Set("Content-Type", "application/json")
	headers(r)
	resp, err := client.Do(r)
	if err != nil {
		return errors.New("notification destination unavailable")
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errors.New("notification destination rejected delivery")
	}
	return nil
}
