package loom

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/store"
	bolt "go.etcd.io/bbolt"
)

const engineKeysState = "engine_service_keys"

type engineKeyUsage struct {
	Requests   int64  `json:"requests"`
	Prompt     int64  `json:"prompt_tokens"`
	Completion int64  `json:"completion_tokens"`
	LastError  string `json:"last_error"`
}
type engineAPIIdentity struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Created     time.Time      `json:"created_at"`
	LastUsed    time.Time      `json:"last_used_at"`
	Allowed     []string       `json:"allowed_models"`
	Concurrency int            `json:"max_concurrency"`
	RPM         int            `json:"requests_per_minute"`
	Priority    string         `json:"priority"`
	Usage       engineKeyUsage `json:"usage"`
}
type engineKeyRecord struct {
	engineAPIIdentity
	Hash string `json:"hash"`
}
type engineKeyWindow struct {
	active   int
	requests []time.Time
}

var engineKeysMu sync.Mutex
var engineKeyWindows = map[string]*engineKeyWindow{}
var engineKeyNow = func() time.Time { return time.Now().UTC() }

// Capacité privée entre les processus web/serve de la même installation.
// Elle ne figure pas dans les clés clientes et ne porte aucun quota.
func loomInferenceSecret() string {
	if b, err := getBytesErr(bkState, "engine_internal_capability"); err == nil && len(b) > 0 {
		return string(b)
	}
	var secret string
	err := store.Update(dbPath(), bkState, func(b *bolt.Bucket) error {
		secret = string(b.Get([]byte("engine_internal_capability")))
		if secret != "" {
			return nil
		}
		var err error
		secret, err = newEngineSecret()
		if err != nil {
			return err
		}
		return b.Put([]byte("engine_internal_capability"), []byte(secret))
	})
	if err != nil {
		return ""
	}
	return secret
}

func newEngineSecret() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "sk-loom-" + hex.EncodeToString(b), nil
}
func loadEngineKeysLocked() ([]engineKeyRecord, error) {
	b, err := getBytesErr(bkState, engineKeysState)
	if err != nil {
		return nil, err
	}
	keys := []engineKeyRecord{}
	if len(b) > 0 {
		if err := json.Unmarshal(b, &keys); err != nil {
			return nil, err
		}
	}
	return keys, nil
}

func saveEngineKeysLocked(keys []engineKeyRecord, requireNamed bool) error {
	raw, err := json.Marshal(keys)
	if err != nil {
		return err
	}
	return store.Update(dbPath(), bkState, func(b *bolt.Bucket) error {
		if err := b.Put([]byte(engineKeysState), raw); err != nil {
			return err
		}
		if requireNamed {
			return b.Put([]byte("engine_named_keys_required"), []byte("1"))
		}
		return nil
	})
}
func defaultEngineIdentity() engineAPIIdentity {
	return engineAPIIdentity{ID: "default", Name: "default", Allowed: []string{}, Priority: "interactive"}
}
func legacyEngineKey() string {
	if key := readAPIKey(); key != "" {
		return key
	}
	return strings.TrimSpace(ReadConfig()["API_KEY"])
}
func oaiKeyOK(r *http.Request) (engineAPIIdentity, bool) {
	got := oaiBearer(r)
	hash := hashWebKey(got)
	if secret := loomInferenceSecret(); secret != "" && got != "" && subtle.ConstantTimeCompare([]byte(hash), []byte(hashWebKey(secret))) == 1 {
		return engineAPIIdentity{ID: "loom", Name: "Loom", Priority: "interactive"}, true
	}
	engineKeysMu.Lock()
	defer engineKeysMu.Unlock()
	keys, err := loadEngineKeysLocked()
	if err != nil {
		return engineAPIIdentity{}, false
	}
	for _, key := range keys {
		if key.ID != "default" && got != "" && subtle.ConstantTimeCompare([]byte(key.Hash), []byte(hash)) == 1 {
			return key.engineAPIIdentity, true
		}
	}
	legacy := legacyEngineKey()
	if legacy != "" && subtle.ConstantTimeCompare([]byte(hashWebKey(legacy)), []byte(hash)) != 1 {
		return engineAPIIdentity{}, false
	}
	named := false
	for _, key := range keys {
		if key.ID != "default" {
			named = true
		}
	}
	if legacy == "" && (lanExposed() || named || getBool(bkState, "engine_named_keys_required")) {
		return engineAPIIdentity{}, false
	}
	for _, key := range keys {
		if key.ID == "default" {
			return key.engineAPIIdentity, true
		}
	}
	return defaultEngineIdentity(), true
}

func engineAllowed(key engineAPIIdentity, requested, resolved string) bool {
	if len(key.Allowed) == 0 {
		return true
	}
	for _, model := range key.Allowed {
		if strings.EqualFold(model, requested) || model == resolved {
			return true
		}
		if entry, ok := resolveOAIModel(model); ok {
			if entry.Kind == "preset" {
				if want, ok := resolveOAIModel(requested); ok && want.Kind == "preset" && want.Preset == entry.Preset {
					return true
				}
				if oaiKeepCurrent(requested) && strings.TrimSuffix(filepath.Base(entry.Preset), ".env") == getStr(bkState, "active_preset") {
					return true
				}
			} else {
				a, errA := resolveServeModelPath(entry.Model)
				b, errB := resolveServeModelPath(resolved)
				if errA == nil && errB == nil && filepath.Clean(a) == filepath.Clean(b) {
					return true
				}
			}
		}
	}
	return false
}

type engineKeyLimit struct {
	code  string
	retry int
}

func (e *engineKeyLimit) Error() string { return e.code }

func beginEngineKey(key engineAPIIdentity) (func(int64, int64, string), error) {
	if key.ID == "loom" {
		return func(int64, int64, string) {}, nil
	}
	engineKeysMu.Lock()
	defer engineKeysMu.Unlock()
	keys, err := loadEngineKeysLocked()
	if err != nil {
		return nil, err
	}
	index := -1
	for i := range keys {
		if keys[i].ID == key.ID {
			index = i
			key = keys[i].engineAPIIdentity
			break
		}
	}
	if index < 0 {
		if key.ID != "default" {
			return nil, fmt.Errorf("key revoked")
		}
		keys = append(keys, engineKeyRecord{engineAPIIdentity: key})
		index = len(keys) - 1
		keys[index].Created = engineKeyNow()
	}
	now := engineKeyNow()
	windowID := LoomHome() + "/" + key.ID
	window := engineKeyWindows[windowID]
	if window == nil {
		window = &engineKeyWindow{}
		engineKeyWindows[windowID] = window
	}
	cut := 0
	for cut < len(window.requests) && now.Sub(window.requests[cut]) >= time.Minute {
		cut++
	}
	window.requests = window.requests[cut:]
	if key.Concurrency > 0 && window.active >= key.Concurrency {
		return nil, &engineKeyLimit{"concurrency_limit", 1}
	}
	if key.RPM > 0 && len(window.requests) >= key.RPM {
		retry := int((window.requests[0].Add(time.Minute).Sub(now) + time.Second - 1) / time.Second)
		if retry < 1 {
			retry = 1
		}
		return nil, &engineKeyLimit{"rate_limit", retry}
	}
	keys[index].LastUsed = now
	keys[index].Usage.Requests++
	if err := putJSON(bkState, engineKeysState, keys); err != nil {
		return nil, err
	}
	window.active++
	window.requests = append(window.requests, now)
	var once sync.Once
	return func(prompt, completion int64, lastError string) {
		once.Do(func() {
			engineKeysMu.Lock()
			defer engineKeysMu.Unlock()
			window.active--
			keys, err := loadEngineKeysLocked()
			if err != nil {
				return
			}
			for i := range keys {
				if keys[i].ID == key.ID {
					keys[i].Usage.Prompt += prompt
					keys[i].Usage.Completion += completion
					keys[i].Usage.LastError = lastError
					_ = putJSON(bkState, engineKeysState, keys)
					break
				}
			}
		})
	}, nil
}

// Seuls les champs publiés par le moteur sont comptés, sans estimer de tokens.
// Le tap est borné, laisse passer les octets tels quels et ne retarde pas SSE.
type engineUsageTap struct {
	body               io.ReadCloser
	stream             bool
	pending            []byte
	discard            bool
	prompt, completion int64
	lastError          string
	hasUsage           bool
	complete           bool
}

func (t *engineUsageTap) parse(raw []byte) {
	var chunk struct {
		Usage *struct {
			Prompt     int64 `json:"prompt_tokens"`
			Completion int64 `json:"completion_tokens"`
		} `json:"usage"`
		Timings *struct {
			Prompt     int64 `json:"prompt_n"`
			Completion int64 `json:"predicted_n"`
		} `json:"timings"`
		Error *struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &chunk) != nil {
		return
	}
	if chunk.Usage != nil {
		t.hasUsage = true
		t.prompt, t.completion = max(0, chunk.Usage.Prompt), max(0, chunk.Usage.Completion)
	} else if chunk.Timings != nil && !t.hasUsage {
		t.prompt, t.completion = max(0, chunk.Timings.Prompt), max(0, chunk.Timings.Completion)
	}
	if chunk.Error != nil {
		t.lastError = "engine_error"
	}
}
func (t *engineUsageTap) feed(b []byte) {
	if !t.stream {
		if !t.discard && len(t.pending)+len(b) <= 4<<20 {
			t.pending = append(t.pending, b...)
		} else {
			t.discard = true
			t.pending = nil
		}
		return
	}
	for len(b) > 0 {
		i := strings.IndexByte(string(b), '\n')
		piece := b
		if i >= 0 {
			piece = b[:i]
		}
		if !t.discard && len(t.pending)+len(piece) <= 1<<20 {
			t.pending = append(t.pending, piece...)
		} else {
			t.discard = true
			t.pending = nil
		}
		if i < 0 {
			return
		}
		if !t.discard {
			line := strings.TrimSpace(string(t.pending))
			if strings.HasPrefix(line, "data:") {
				t.parse([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))))
			}
		}
		t.pending, t.discard = nil, false
		b = b[i+1:]
	}
}
func (t *engineUsageTap) Read(b []byte) (int, error) {
	n, err := t.body.Read(b)
	t.feed(b[:n])
	if err == io.EOF {
		t.complete = true
		if !t.stream && !t.discard {
			t.parse(t.pending)
		}
		if t.stream && len(t.pending) > 0 && !t.discard {
			t.parse([]byte(strings.TrimSpace(strings.TrimPrefix(string(t.pending), "data:"))))
		}
	} else if err != nil {
		t.lastError = "stream_interrupted"
	}
	return n, err
}
func (t *engineUsageTap) Close() error {
	if !t.complete && t.lastError == "" {
		t.lastError = "stream_interrupted"
	}
	return t.body.Close()
}

// Identité interne non persistée ; jamais envoyée à un fournisseur extérieur.
func loomInferenceHeaders(req *http.Request, priority string) {
	req.Header.Set("X-Loom-Priority", priority)
	if currentEngineNode() == nil && strings.TrimRight(req.URL.Scheme+"://"+req.URL.Host, "/") == engineBase() {
		req.Header.Set("Authorization", "Bearer "+loomInferenceSecret())
	}
}
