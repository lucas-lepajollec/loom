package loom

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/policy"
)

type policyConsentRequest struct {
	SessionID string `json:"session_id"`
	RequestID string `json:"request_id"`
	ready     chan struct{}
	done      chan struct{}
	cancel    context.CancelFunc
	err       error
}
type policyConsentRequired struct{ request *policyConsentRequest }

func (e *policyConsentRequired) Error() string {
	return "confirm this capability (consent:true or answer the policy request and retry)"
}

type policyConsents struct {
	mu      sync.Mutex
	closed  bool
	pending map[string]*policyConsentRequest
	grants  map[string]time.Time
}

// Legacy consent APIs retain their immediate refusal without consent. The
// refusal now opens a canonical metadata-only request; its one-shot answer is
// consumed by an explicit retry of exactly that operation, never an auto-send.
// No prompt or command is retained in this registry (the key is a digest).
func (m *runtimeSessions) authorizeLegacyConsent(in policy.Input, consent bool) error {
	raw, _ := json.Marshal(in)
	key := hashWebKey(string(raw) + "|" + in.ConsentKey)
	c := &m.policyConsents
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return errors.New("policy consent service stopped")
	}
	if c.pending == nil {
		c.pending = map[string]*policyConsentRequest{}
		c.grants = map[string]time.Time{}
	}
	for k, expiry := range c.grants {
		if time.Now().After(expiry) {
			delete(c.grants, k)
		}
	}
	if consent {
		pending := c.pending[key]
		delete(c.grants, key)
		c.mu.Unlock()
		if pending != nil {
			pending.cancel()
			<-pending.done
			c.mu.Lock()
			delete(c.grants, key)
			c.mu.Unlock()
		}
		policyAudit.Record(in, policy.Result{Decision: policy.Allow, Reason: "explicit_consent"}, false)
		return nil
	}
	if !c.grants[key].IsZero() {
		delete(c.grants, key)
		c.mu.Unlock()
		policyAudit.Record(in, policy.Result{Decision: policy.Allow, Reason: "interaction_grant"}, false)
		return nil
	}
	if pending := c.pending[key]; pending != nil {
		c.mu.Unlock()
		<-pending.ready
		if pending.err != nil {
			return pending.err
		}
		return &policyConsentRequired{pending}
	}
	if len(c.pending)+len(c.grants) >= 128 {
		c.mu.Unlock()
		return errors.New("too many policy consent requests")
	}
	ctx, cancel := context.WithCancel(context.Background())
	pending := &policyConsentRequest{ready: make(chan struct{}), done: make(chan struct{}), cancel: cancel}
	c.pending[key] = pending
	m.policyWorkers.Add(1)
	c.mu.Unlock()
	go func() {
		defer m.policyWorkers.Done()
		defer close(pending.done)
		defer cancel()
		opened := false
		err := m.confirmPolicy(ctx, in, func(session, request string) {
			pending.SessionID, pending.RequestID = session, request
			opened = true
			close(pending.ready)
		})
		if !opened {
			pending.err = err
			close(pending.ready)
		}
		c.mu.Lock()
		delete(c.pending, key)
		if err == nil {
			c.grants[key] = time.Now().Add(5 * time.Minute)
		}
		c.mu.Unlock()
	}()
	<-pending.ready
	if pending.err != nil {
		return pending.err
	}
	return &policyConsentRequired{pending}
}
func policyErrorEnvelope(err error) map[string]any {
	out := map[string]any{"ok": false, "error": err.Error()}
	var pending *policyConsentRequired
	if errors.As(err, &pending) {
		out["code"] = "policy_confirmation_required"
		out["interaction"] = map[string]string{"session_id": pending.request.SessionID, "request_id": pending.request.RequestID}
	}
	return out
}
