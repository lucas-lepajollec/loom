package loom

// Human access uses a password and revocable browser sessions. Inference keys,
// legacy automation keys and the encryption vault keep their own credentials.
import (
	"bytes"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/lucas-lepajollec/loom/internal/loom/store"
	"github.com/lucas-lepajollec/loom/internal/loom/web"
	bolt "go.etcd.io/bbolt"
	"golang.org/x/term"
)

const webPasswordKey = "web_password"
const webSessionPrefix = "web_session/"
const webSessionCookie = "loom_session"
const webSessionLife = 30 * 24 * time.Hour

// Version 1 fixes the KDF parameters so future vault defaults cannot change
// how existing access passwords are verified.
const webPasswordTime = 3
const webPasswordMemory = 64 * 1024
const webPasswordThreads = 1

type webPassword struct {
	Version int    `json:"version"`
	Salt    string `json:"salt"`
	Hash    string `json:"hash"`
}

func readWebPassword() (*webPassword, []byte, error) {
	b, err := getBytesErr(bkState, webPasswordKey)
	if err != nil || len(b) == 0 {
		return nil, b, err
	}
	var p webPassword
	if json.Unmarshal(b, &p) != nil || p.Version != 1 {
		return nil, nil, errors.New("invalid password record")
	}
	salt, e1 := base64.StdEncoding.DecodeString(p.Salt)
	hash, e2 := base64.StdEncoding.DecodeString(p.Hash)
	if e1 != nil || e2 != nil || len(salt) != memSaltLen || len(hash) != memDEKLen {
		return nil, nil, errors.New("invalid password record")
	}
	return &p, b, nil
}

func (p *webPassword) matches(password string) bool {
	if p == nil || len(password) > 1024 {
		return false
	}
	salt, _ := base64.StdEncoding.DecodeString(p.Salt)
	want, _ := base64.StdEncoding.DecodeString(p.Hash)
	got := deriveKEK(password, salt, webPasswordTime, webPasswordMemory, webPasswordThreads)
	return subtle.ConstantTimeCompare(got, want) == 1
}

// A password change atomically invalidates every browser session. expected is
// the previously read record; nil is reserved for local CLI recovery.
func saveWebPassword(password string, expected []byte) error {
	_, err := saveWebPasswordRecord(password, expected)
	return err
}

func saveWebPasswordRecord(password string, expected []byte) ([]byte, error) {
	if !utf8.ValidString(password) || utf8.RuneCountInString(password) < 12 || len(password) > 1024 {
		return nil, errors.New("use at least 12 characters (maximum 1024 bytes)")
	}
	salt, err := randBytes(memSaltLen)
	if err != nil {
		return nil, err
	}
	p := webPassword{1, base64.StdEncoding.EncodeToString(salt), base64.StdEncoding.EncodeToString(deriveKEK(password, salt, webPasswordTime, webPasswordMemory, webPasswordThreads))}
	data, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	err = store.Update(dbPath(), bkState, func(b *bolt.Bucket) error {
		if expected != nil && !bytes.Equal(b.Get([]byte(webPasswordKey)), expected) {
			return errors.New("password changed concurrently; try again")
		}
		c := b.Cursor()
		for k, _ := c.Seek([]byte(webSessionPrefix)); bytes.HasPrefix(k, []byte(webSessionPrefix)); k, _ = c.Next() {
			if err := c.Delete(); err != nil {
				return err
			}
		}
		return b.Put([]byte(webPasswordKey), data)
	})
	return data, err
}

type webSession struct {
	Credential string `json:"credential"`
	Expires    int64  `json:"expires"`
}

func sessionRecordKey(r *http.Request) string {
	c, err := r.Cookie(webSessionCookie)
	if err != nil || len(c.Value) != 64 {
		return ""
	}
	if _, err := hex.DecodeString(c.Value); err != nil {
		return ""
	}
	return webSessionPrefix + hashWebKey(c.Value)
}

func webSessionValid(r *http.Request, credential []byte) (bool, error) {
	k := sessionRecordKey(r)
	if k == "" || len(credential) == 0 {
		return false, nil
	}
	b, err := getBytesErr(bkState, k)
	if err != nil {
		return false, err
	}
	var s webSession
	if json.Unmarshal(b, &s) != nil {
		return false, nil
	}
	return s.Expires > time.Now().Unix() && s.Credential == hashWebKey(string(credential)), nil
}

func sessionCookie(r *http.Request, token string, expires time.Time) *http.Cookie {
	maxAge := int(webSessionLife.Seconds())
	if token == "" {
		maxAge = -1
	}
	return &http.Cookie{Name: webSessionCookie, Value: token, Path: "/", HttpOnly: true,
		Secure:   r.TLS != nil || os.Getenv("LOOM_COOKIE_SECURE") == "1",
		SameSite: http.SameSiteStrictMode, MaxAge: maxAge, Expires: expires}
}

func issueWebSession(w http.ResponseWriter, r *http.Request, credential []byte) error {
	random, err := randBytes(32)
	if err != nil {
		return err
	}
	token := hex.EncodeToString(random)
	expires := time.Now().Add(webSessionLife)
	data, _ := json.Marshal(webSession{hashWebKey(string(credential)), expires.Unix()})
	err = store.Update(dbPath(), bkState, func(b *bolt.Bucket) error {
		if !bytes.Equal(b.Get([]byte(webPasswordKey)), credential) {
			return errors.New("password changed; sign in again")
		}
		// Bound stored sessions to 32 devices, remove expired/replaced sessions.
		var keys []string
		var oldest string
		var oldestExpiry int64
		c := b.Cursor()
		for k, v := c.Seek([]byte(webSessionPrefix)); bytes.HasPrefix(k, []byte(webSessionPrefix)); k, v = c.Next() {
			var s webSession
			if json.Unmarshal(v, &s) != nil || s.Expires <= time.Now().Unix() || string(k) == sessionRecordKey(r) {
				if err := c.Delete(); err != nil {
					return err
				}
				continue
			}
			keys = append(keys, string(k))
			if oldest == "" || s.Expires < oldestExpiry {
				oldest, oldestExpiry = string(k), s.Expires
			}
		}
		if len(keys) >= 32 {
			if err := b.Delete([]byte(oldest)); err != nil {
				return err
			}
		}
		return b.Put([]byte(webSessionPrefix+hashWebKey(token)), data)
	})
	if err == nil {
		http.SetCookie(w, sessionCookie(r, token, expires))
	}
	return err
}

// Direct peer only: forwarded headers are never accepted as a rate-limit bypass.
var webLoginLimit = struct {
	sync.Mutex
	peers map[string]struct {
		at    time.Time
		count int
	}
	at    time.Time
	count int
}{peers: make(map[string]struct {
	at    time.Time
	count int
})}
var webPasswordWork = make(chan struct{}, 2)

func allowWebLogin(r *http.Request) bool {
	peer, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		peer = r.RemoteAddr
	}
	now := time.Now()
	webLoginLimit.Lock()
	defer webLoginLimit.Unlock()
	if now.Sub(webLoginLimit.at) >= time.Minute {
		webLoginLimit.at, webLoginLimit.count = now, 0
	}
	for k, v := range webLoginLimit.peers {
		if now.Sub(v.at) >= time.Minute {
			delete(webLoginLimit.peers, k)
		}
	}
	p := webLoginLimit.peers[peer]
	if p.at.IsZero() {
		p.at = now
	}
	if p.count >= 10 || webLoginLimit.count >= 60 {
		return false
	}
	p.count++
	webLoginLimit.count++
	webLoginLimit.peers[peer] = p
	return true
}

func loginWork(w http.ResponseWriter, r *http.Request) (func(), bool) {
	if allowWebLogin(r) {
		select {
		case webPasswordWork <- struct{}{}:
			return func() { <-webPasswordWork }, true
		default:
		}
	}
	w.Header().Set("Retry-After", "60")
	sendJSON(w, 429, map[string]any{"ok": false, "error": "too many attempts; try again in a minute"})
	return nil, false
}

func webAuthUnavailable(w http.ResponseWriter) {
	sendJSON(w, 503, map[string]any{"ok": false, "error": "authentication unavailable; try again later"})
}

func requireBrowserOrWebKey(next http.HandlerFunc) http.HandlerFunc {
	protected := web.ProtectOrigin(next)
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if isE2EAuthed(r) {
			next(w, r)
			return
		}
		p, credential, err := readWebPassword()
		if err != nil {
			webAuthUnavailable(w)
			return
		}
		hash, err := webKeyHashErr()
		if err != nil {
			webAuthUnavailable(w)
			return
		}
		valid, err := webSessionValid(r, credential)
		if err != nil {
			webAuthUnavailable(w)
			return
		}
		// Explicit machine credentials are not ambient browser authentication.
		// Remote-engine proxies keep their existing Bearer contract.
		if hash != "" && checkBearer(r, hash) {
			next(w, r)
			return
		}
		if valid || (p == nil && hash == "") {
			protected.ServeHTTP(w, r)
			return
		}
		w.Header().Set("WWW-Authenticate", `Bearer realm="loom"`)
		sendJSON(w, 401, map[string]any{"ok": false, "error": "sign in required"})
	}
}

func registerWebLogin(mux *http.ServeMux) {
	mux.HandleFunc("/api/auth/status", handleWebAuthStatus)
	mux.Handle("/api/auth/login", web.ProtectOrigin(http.HandlerFunc(handleWebLogin)))
	mux.Handle("/api/auth/logout", web.ProtectOrigin(http.HandlerFunc(handleWebLogout)))
	mux.HandleFunc("/api/auth/password", requireWebAuth(handleWebPassword))
}

func handleWebAuthStatus(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodGet) {
		return
	}
	p, credential, err := readWebPassword()
	if err != nil {
		webAuthUnavailable(w)
		return
	}
	hash, err := webKeyHashErr()
	if err != nil {
		webAuthUnavailable(w)
		return
	}
	valid, err := webSessionValid(r, credential)
	if err != nil {
		webAuthUnavailable(w)
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "password_set": p != nil, "required": p != nil || hash != "",
		"authenticated": valid || isE2EAuthed(r) || (hash != "" && checkBearer(r, hash)) || (p == nil && hash == "")})
}

func handleWebLogin(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	done, ok := loginWork(w, r)
	if !ok {
		return
	}
	defer done()
	p, credential, err := readWebPassword()
	if err != nil {
		webAuthUnavailable(w)
		return
	}
	if p == nil || !p.matches(req.Password) {
		sendJSON(w, 401, map[string]any{"ok": false, "error": "incorrect password"})
		return
	}
	if err := issueWebSession(w, r, credential); err != nil {
		webAuthUnavailable(w)
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true})
}

func handleWebLogout(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	if k := sessionRecordKey(r); k != "" {
		// Delete only: anonymous logout with an unknown cookie must never
		// create persistent records in the database.
		if err := store.Update(dbPath(), bkState, func(b *bolt.Bucket) error {
			return b.Delete([]byte(k))
		}); err != nil {
			webAuthUnavailable(w)
			return
		}
	}
	http.SetCookie(w, sessionCookie(r, "", time.Unix(1, 0)))
	sendJSON(w, 200, map[string]any{"ok": true})
}

func handleWebPassword(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		Password string `json:"password"`
		Current  string `json:"current_password"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	done, ok := loginWork(w, r)
	if !ok {
		return
	}
	defer done()
	p, old, err := readWebPassword()
	if err != nil {
		webAuthUnavailable(w)
		return
	}
	if p != nil && !p.matches(req.Current) {
		sendJSON(w, 401, map[string]any{"ok": false, "error": "incorrect current password"})
		return
	}
	if old == nil {
		old = []byte{}
	} // compare-and-set, never overwrite a concurrent setup
	credential, err := saveWebPasswordRecord(req.Password, old)
	if err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "use at least 12 characters; if already changed, reload and try again"})
		return
	}
	if issueWebSession(w, r, credential) != nil {
		webAuthUnavailable(w)
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true})
}

// Local recovery never takes passwords in argv, shell history or environment.
func cmdPassword(args []string) error {
	var password string
	if len(args) == 1 && args[0] == "--stdin" {
		b, err := io.ReadAll(io.LimitReader(os.Stdin, 1026))
		if err != nil {
			return err
		}
		password = strings.TrimSuffix(strings.TrimSuffix(string(b), "\n"), "\r")
	} else if len(args) == 0 && term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Print("New Loom access password: ")
		first, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			return err
		}
		fmt.Print("Confirm password: ")
		second, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			return err
		}
		if !bytes.Equal(first, second) {
			return errors.New("passwords do not match")
		}
		password = string(first)
	} else {
		return errors.New("use loom password (interactive) or loom password --stdin")
	}
	if err := saveWebPassword(password, nil); err != nil {
		return err
	}
	fmt.Println("Loom access password saved. Browser sessions revoked. No restart required.")
	return nil
}
