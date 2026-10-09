package notify

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"time"
)

// Payload-free Web Push (RFC 8030): the SW reads authenticated pending notices.
// No RFC 8291 encryption is required because no content crosses the push service.
func GenerateVAPID() (priv, pub string, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", err
	}
	return base64.RawURLEncoding.EncodeToString(key.D.FillBytes(make([]byte, 32))), base64.RawURLEncoding.EncodeToString(elliptic.Marshal(elliptic.P256(), key.X, key.Y)), nil
}
func VAPID(endpoint, subject, priv, pub string, now time.Time) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" {
		return "", errors.New("secure push endpoint required")
	}
	sub, err := url.Parse(subject)
	if err != nil || sub.Scheme != "https" || sub.Hostname() == "" || sub.User != nil {
		return "", errors.New("HTTPS public base URL required for Web Push")
	}
	raw, err := base64.RawURLEncoding.DecodeString(priv)
	if err != nil || len(raw) != 32 {
		return "", errors.New("invalid VAPID key")
	}
	d := new(big.Int).SetBytes(raw)
	curve := elliptic.P256()
	if d.Sign() == 0 || d.Cmp(curve.Params().N) >= 0 {
		return "", errors.New("invalid VAPID key")
	}
	x, y := curve.ScalarBaseMult(raw)
	key := &ecdsa.PrivateKey{PublicKey: ecdsa.PublicKey{Curve: curve, X: x, Y: y}, D: d}
	if base64.RawURLEncoding.EncodeToString(elliptic.Marshal(curve, x, y)) != pub {
		return "", errors.New("VAPID key mismatch")
	}
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"typ":"JWT","alg":"ES256"}`))
	claims, _ := json.Marshal(map[string]any{"aud": u.Scheme + "://" + u.Host, "exp": now.Add(12 * time.Hour).Unix(), "sub": subject})
	body := header + "." + base64.RawURLEncoding.EncodeToString(claims)
	hash := sha256.Sum256([]byte(body))
	r, s, err := ecdsa.Sign(rand.Reader, key, hash[:])
	if err != nil {
		return "", err
	}
	sig := append(r.FillBytes(make([]byte, 32)), s.FillBytes(make([]byte, 32))...)
	return "vapid t=" + body + "." + base64.RawURLEncoding.EncodeToString(sig) + ", k=" + pub, nil
}
func SendTickle(ctx context.Context, client *http.Client, endpoint, subject, priv, pub string) (gone bool, err error) {
	auth, err := VAPID(endpoint, subject, priv, pub, time.Now())
	if err != nil {
		return false, err
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, http.NoBody)
	if err != nil {
		return false, errors.New("invalid push endpoint")
	}
	r.Header.Set("Authorization", auth)
	r.Header.Set("TTL", "86400")
	r.Header.Set("Urgency", "high")
	resp, err := client.Do(r)
	if err != nil {
		return false, errors.New("push delivery unavailable")
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode == 404 || resp.StatusCode == 410 {
		return true, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, errors.New("push service rejected delivery")
	}
	return false, nil
}
