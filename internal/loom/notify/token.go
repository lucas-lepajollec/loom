package notify

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	agent "github.com/lucas-lepajollec/loom/internal/loom/runtime"
)

type Claim struct {
	SessionID string              `json:"session_id"`
	RequestID string              `json:"request_id"`
	Answer    agent.RequestAnswer `json:"answer"`
	Expires   int64               `json:"expires"`
	Nonce     string              `json:"nonce"`
}

// Capabilities are issued only for offered answers. In-memory issuance and the
// live request broker both enforce single use; a restart revokes all of them.
type Tokens struct {
	mu     sync.Mutex
	key    []byte
	issued map[string]int64
}

func NewTokens(key []byte) *Tokens {
	return &Tokens{key: append([]byte(nil), key...), issued: map[string]int64{}}
}
func (t *Tokens) Issue(session, request string, a agent.RequestAnswer, now time.Time, ttl time.Duration) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for n, expiry := range t.issued {
		if expiry <= now.Unix() {
			delete(t.issued, n)
		}
	}
	if len(t.key) < 32 || len(t.issued) >= 4096 {
		return "", errors.New("action tokens unavailable")
	}
	nonce := make([]byte, 24)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	c := Claim{session, request, a, now.Add(ttl).Unix(), base64.RawURLEncoding.EncodeToString(nonce)}
	raw, _ := json.Marshal(c)
	body := base64.RawURLEncoding.EncodeToString(raw)
	mac := hmac.New(sha256.New, t.key)
	mac.Write([]byte(body))
	t.issued[c.Nonce] = c.Expires
	return body + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}
func (t *Tokens) Resolve(token string, now time.Time, answer func(Claim) error) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(token) > 8192 {
		return errors.New("invalid or expired action")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return errors.New("invalid or expired action")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return errors.New("invalid or expired action")
	}
	mac := hmac.New(sha256.New, t.key)
	mac.Write([]byte(parts[0]))
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return errors.New("invalid or expired action")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return errors.New("invalid or expired action")
	}
	var c Claim
	if json.Unmarshal(raw, &c) != nil || c.Expires <= now.Unix() || t.issued[c.Nonce] != c.Expires {
		return errors.New("invalid, used or expired action")
	}
	if err := answer(c); err != nil {
		return errors.New("request no longer answerable")
	}
	delete(t.issued, c.Nonce)
	return nil
}
