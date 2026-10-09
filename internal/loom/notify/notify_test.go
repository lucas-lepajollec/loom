package notify

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/events"
	agent "github.com/lucas-lepajollec/loom/internal/loom/runtime"
)

func TestCapabilitySecurity(t *testing.T) {
	now := time.Unix(100000, 0)
	key := []byte(strings.Repeat("k", 32))
	tokens := NewTokens(key)
	resolve := func(c Claim) error {
		if c.SessionID != "discussion" || c.RequestID != "request" || c.Answer.Decision != "once" {
			return errors.New("wrong request")
		}
		return nil
	}
	issue := func(s, r string) string {
		v, err := tokens.Issue(s, r, agent.RequestAnswer{Decision: "once"}, now, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	good := issue("discussion", "request")
	for _, v := range []string{good + "x", strings.Replace(good, ".", "x.", 1), issue("other", "request"), issue("discussion", "other")} {
		if err := tokens.Resolve(v, now, resolve); err == nil {
			t.Fatal("wrong binding/tamper accepted")
		}
	}
	if err := tokens.Resolve(good, now.Add(time.Hour), resolve); err == nil {
		t.Fatal("expiry accepted")
	}
	if err := tokens.Resolve(good, now, resolve); err != nil {
		t.Fatal(err)
	}
	if err := tokens.Resolve(good, now, resolve); err == nil {
		t.Fatal("replay accepted")
	}
	if err := NewTokens(key).Resolve(issue("discussion", "request"), now, resolve); err == nil {
		t.Fatal("unissued/restarted token accepted")
	}
	good = issue("discussion", "request")
	var successes atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if tokens.Resolve(good, now, resolve) == nil {
				successes.Add(1)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatal("concurrent reuse", successes.Load())
	}
}

type handlerTransport struct{ handler http.Handler }

func (h handlerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	w := httptest.NewRecorder()
	h.handler.ServeHTTP(w, r)
	return w.Result(), nil
}
func TestNtfyAndWebhookWire(t *testing.T) {
	e := Redact(events.Event{Type: events.TaskWaiting, Title: "Discussion", Status: "waiting_approval", Summary: "private prompt", RequestID: "r"}, false)
	m := Message{Title: e.Title, Body: e.Status, URL: "https://loom.test/#/chat/d", Event: e, Actions: []Action{{Action: "http", Label: "Allow", URL: "https://loom.test/api/notify/answer/token", Method: "POST", Clear: true}}}
	var ntfy NtfyPayload
	client := &http.Client{Transport: handlerTransport{http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer ntfy-secret" || r.Header.Get("Content-Type") != "application/json" {
			t.Error("ntfy headers", r.Header)
		}
		if json.NewDecoder(r.Body).Decode(&ntfy) != nil {
			t.Error("bad payload")
		}
		w.WriteHeader(200)
	})}}
	if err := SendNtfy(context.Background(), client, Ntfy{ServerURL: "https://ntfy.test", Topic: "topic"}, "ntfy-secret", m); err != nil {
		t.Fatal(err)
	}
	if ntfy.Priority != 4 || ntfy.Topic != "topic" || ntfy.Click != m.URL || ntfy.Message != e.Status || len(ntfy.Actions) != 1 || ntfy.Actions[0].Method != "POST" {
		t.Fatal(ntfy)
	}
	client.Transport = handlerTransport{http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mac := hmac.New(sha256.New, []byte("webhook-secret"))
		mac.Write(raw)
		if r.Header.Get("X-Loom-Signature") != "sha256="+hex.EncodeToString(mac.Sum(nil)) || r.Header.Get("X-Loom-Secret") != "webhook-secret" {
			t.Error("signature mismatch")
		}
		if strings.Contains(string(raw), "private prompt") {
			t.Error("summary leaked")
		}
		var got events.Event
		if json.Unmarshal(raw, &got) != nil || got.Type != events.TaskWaiting {
			t.Error("bad event")
		}
		w.WriteHeader(204)
	})}
	if err := SendWebhook(context.Background(), client, Webhook{URL: "https://webhook.test"}, "webhook-secret", e); err != nil {
		t.Fatal(err)
	}
	client.Transport = handlerTransport{http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) })}
	if SendWebhook(context.Background(), client, Webhook{URL: "https://webhook.test"}, "", e) == nil {
		t.Fatal("delivery failure hidden")
	}
}
func TestPolicyAndBurstCoalescing(t *testing.T) {
	c := Default()
	c.PublicBaseURL = "https://loom.test"
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 9, 23, 0, 0, 0, time.UTC)
	if c.Ntfy.Enabled || c.Webhook.Enabled || c.Push.Enabled || c.Rules.IncludeSummaries {
		t.Fatal("notifications enabled by default")
	}
	r := c.Rules
	e := events.Event{ID: 1, Type: events.TaskCompleted, TaskID: "a", DurationSeconds: 120}
	if r.Allows(e, now) {
		t.Fatal("short turn notified")
	}
	e.DurationSeconds = 121
	if !r.Allows(e, now) {
		t.Fatal("completion automation missing")
	}
	r.QuietHours.Enabled = true
	if r.Allows(e, now) {
		t.Fatal("quiet hours ignored")
	}
	if !r.Allows(e, now.Add(-12*time.Hour)) {
		t.Fatal("quiet hours blocked daytime")
	}
	var q Coalescer
	q.Add(e)
	e.ID = 2
	q.Add(e)
	e.TaskID = "b"
	e.ID = 3
	q.Add(e)
	batch := q.Take(now, 10*time.Second)
	if len(batch) != 2 {
		t.Fatal("burst not coalesced", batch)
	}
	e.ID = 4
	q.Add(e)
	if q.Take(now.Add(time.Second), 10*time.Second) != nil {
		t.Fatal("rate limit ignored")
	}
	if len(q.Take(now.Add(10*time.Second), 10*time.Second)) != 1 {
		t.Fatal("rate window lost pending update")
	}
	q.Add(e)
	q.Drop(e)
	if q.Take(now.Add(time.Minute), time.Second) != nil {
		t.Fatal("resolved wait remains queued")
	}
	for i := 0; i < 100; i++ {
		e.TaskID = string(rune(100 + i))
		e.ID = uint64(i + 1)
		q.Add(e)
	}
	if len(q.Take(now.Add(2*time.Minute), time.Second)) != 64 {
		t.Fatal("unbounded queue")
	}
	c.Rules.Events = append(c.Rules.Events, "invented")
	if c.Validate() == nil {
		t.Fatal("invalid rule accepted")
	}
}
func TestVAPIDJWTAndPayloadFreePush(t *testing.T) {
	priv, pub, err := GenerateVAPID()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(123456, 0)
	auth, err := VAPID("https://push.test/subscription", "https://loom.test", priv, pub, now)
	if err != nil {
		t.Fatal(err)
	}
	jwt := strings.Split(strings.TrimPrefix(auth, "vapid t="), ", k=")[0]
	parts := strings.Split(jwt, ".")
	sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
	raw, _ := base64.RawURLEncoding.DecodeString(pub)
	x, y := elliptic.Unmarshal(elliptic.P256(), raw)
	hash := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if len(sig) != 64 || !ecdsa.Verify(&ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y}, hash[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
		t.Fatal("bad ES256 signature")
	}
	claimsRaw, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var claims struct {
		Aud string
		Exp int64
		Sub string
	}
	json.Unmarshal(claimsRaw, &claims)
	if claims.Aud != "https://push.test" || claims.Exp != now.Add(12*time.Hour).Unix() || claims.Sub != "https://loom.test" {
		t.Fatal(claims)
	}
	if _, err := VAPID("http://push.test", "https://loom.test", priv, pub, now); err == nil {
		t.Fatal("insecure push accepted")
	}
	client := &http.Client{Transport: handlerTransport{http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if len(body) != 0 || r.Header.Get("TTL") != "86400" || !strings.HasPrefix(r.Header.Get("Authorization"), "vapid t=") {
			t.Error("invalid tickle")
		}
		w.WriteHeader(201)
	})}}
	if gone, err := SendTickle(context.Background(), client, "https://push.test/subscription", "https://loom.test", priv, pub); gone || err != nil {
		t.Fatal(gone, err)
	}
	client.Transport = handlerTransport{http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(410) })}
	if gone, err := SendTickle(context.Background(), client, "https://push.test/subscription", "https://loom.test", priv, pub); !gone || err != nil {
		t.Fatal(gone, err)
	}
}
