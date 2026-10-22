package loom

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/lucas-lepajollec/loom/internal/loom/policy"
)

// Faux serveur Codeman : enregistre ce qu'il reçoit, répond comme l'API v1.
type fakeCodeman struct {
	mu   sync.Mutex
	seen []*http.Request
	auth []string
}

func (f *fakeCodeman) handler(t *testing.T) http.Handler {
	mux := http.NewServeMux()
	record := func(r *http.Request) {
		f.mu.Lock()
		f.seen = append(f.seen, r.Clone(context.Background()))
		f.auth = append(f.auth, r.Header.Get("Authorization"))
		f.mu.Unlock()
	}
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("admin:synthetic-codeman"))
	mux.HandleFunc("/api/v1/", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		if r.Header.Get("Authorization") != want {
			w.Header().Set("WWW-Authenticate", `Basic realm="Codeman"`)
			http.Error(w, "Unauthorized", 401)
			return
		}
		switch r.URL.Path {
		case "/api/v1/me":
			io.WriteString(w, `{"success":true,"data":{"username":"admin","role":"admin","multiUser":false}}`)
		case "/api/v1/status":
			io.WriteString(w, `{"success":true,"data":{"version":"1.43.0","sessions":[]}}`)
		case "/api/v1/events":
			w.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(w, "event: sse:heartbeat\ndata: {\"t\":1}\n\n")
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		case "/api/v1/sessions/abc/shell":
			if len(r.TransferEncoding) > 0 || r.ContentLength != 0 {
				http.Error(w, `{"success":false,"error":"Unsupported Media Type"}`, 415)
				return
			}
			io.WriteString(w, `{"success":true,"data":{}}`)
		default:
			body, _ := io.ReadAll(r.Body)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"method": r.Method, "path": r.URL.Path, "query": r.URL.RawQuery, "body": string(body)}})
		}
	})
	mux.HandleFunc("/ws/sessions/{id}/terminal", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		if r.Header.Get("Authorization") != want || r.Header.Get("Origin") != "" {
			http.Error(w, "Unauthorized", 401)
			return
		}
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		ctx := r.Context()
		_ = c.Write(ctx, websocket.MessageText, []byte(`{"t":"o","d":"ready `+r.PathValue("id")+`"}`))
		for {
			_, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			_ = c.Write(ctx, websocket.MessageText, append([]byte("echo:"), data...))
			if strings.Contains(string(data), "bye") {
				c.Close(4009, "Session terminated")
				return
			}
		}
	})
	return mux
}

func codemanTestServers(t *testing.T) (*fakeCodeman, *httptest.Server) {
	t.Helper()
	testHome(t)
	fake := &fakeCodeman{}
	upstream := httptest.NewServer(fake.handler(t))
	t.Cleanup(upstream.Close)
	mux := http.NewServeMux()
	registerCodemanRoutes(mux, webAPI(mux))
	loom := httptest.NewServer(mux)
	t.Cleanup(loom.Close)
	if err := saveCodemanConfig(codemanConfig{URL: upstream.URL, Username: "admin"}); err != nil {
		t.Fatal(err)
	}
	return fake, loom
}

func codemanDo(t *testing.T, method, url, body string, headers map[string]string) (*http.Response, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(method, url, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp, out
}

func TestCodemanConfigNeverReturnsThePassword(t *testing.T) {
	_, loom := codemanTestServers(t)
	resp, out := codemanDo(t, "GET", loom.URL+"/api/codeman/config", "", nil)
	if resp.StatusCode != 200 || out["password_set"] != false || out["username"] != "admin" {
		t.Fatal(resp.StatusCode, out)
	}
	resp, out = codemanDo(t, "POST", loom.URL+"/api/codeman/config", `{"url":"http://127.0.0.1:1/","username":"admin","password":"synthetic-codeman"}`, nil)
	if resp.StatusCode != 200 || out["password_set"] != true || out["url"] != "http://127.0.0.1:1" {
		t.Fatal(resp.StatusCode, out)
	}
	raw, _ := json.Marshal(out)
	cfg, _ := os.ReadFile(codemanConfigPath())
	if strings.Contains(string(raw), "synthetic-codeman") || strings.Contains(string(cfg), "synthetic-codeman") {
		t.Fatal("password leaked into the response or the plain configuration")
	}
	if info, err := os.Stat(codemanConfigPath()); err != nil || info.Mode().Perm()&0077 != 0 {
		t.Fatal("configuration file must be private", err)
	}
	for _, bad := range []string{`{"url":"ftp://x"}`, `{"url":"http://user:pw@host:3000"}`, `{"url":"http://host:3000/api"}`, `{"url":"http://host","username":"a b"}`} {
		if resp, _ := codemanDo(t, "POST", loom.URL+"/api/codeman/config", bad, nil); resp.StatusCode != 400 {
			t.Fatal(bad, resp.StatusCode)
		}
	}
	if _, out = codemanDo(t, "POST", loom.URL+"/api/codeman/config", `{"url":"http://127.0.0.1:1","clear_password":true}`, nil); out["password_set"] != false {
		t.Fatal(out)
	}
}

func TestCodemanProxyInjectsCredentialsAndKeepsLoomAuthSeparate(t *testing.T) {
	fake, loom := codemanTestServers(t)
	// Sans mot de passe : Codeman refuse, Loom répond 502 (jamais 401, qui
	// déclencherait la reconnexion à Loom).
	resp, out := codemanDo(t, "GET", loom.URL+"/api/codeman/v1/sessions", "", nil)
	if resp.StatusCode != 502 || out["errorCode"] != "CODEMAN_AUTH" {
		t.Fatal(resp.StatusCode, out)
	}
	if err := sealProviderSecret(codemanSecretID, "synthetic-codeman"); err != nil {
		t.Fatal(err)
	}
	resp, out = codemanDo(t, "PUT", loom.URL+"/api/codeman/v1/sessions/abc/name?x=1", `{"name":"w1"}`, map[string]string{"Cookie": "codeman_session=browser", "Authorization": "Bearer browser"})
	if resp.StatusCode != 200 {
		t.Fatal(resp.StatusCode, out)
	}
	data := out["data"].(map[string]any)
	if data["method"] != "PUT" || data["path"] != "/api/v1/sessions/abc/name" || data["query"] != "x=1" || data["body"] != `{"name":"w1"}` {
		t.Fatal(data)
	}
	fake.mu.Lock()
	last := fake.seen[len(fake.seen)-1]
	fake.mu.Unlock()
	if last.Header.Get("Cookie") != "" || last.Header.Get("Origin") != "" || !strings.HasPrefix(last.Header.Get("Authorization"), "Basic ") {
		t.Fatal("browser credentials crossed the gateway", last.Header)
	}
	if resp, out := codemanDo(t, "POST", loom.URL+"/api/codeman/v1/sessions/abc/shell", "", nil); resp.StatusCode != 200 {
		t.Fatal("empty POST body was not forwarded as empty", resp.StatusCode, out)
	}
	fake.mu.Lock()
	before := len(fake.seen)
	fake.mu.Unlock()
	for _, p := range []string{"auth/revoke", "logout", "hook-event", "../status", "sessions/%2e%2e/x"} {
		if resp, _ := codemanDo(t, "POST", loom.URL+"/api/codeman/v1/"+p, `{}`, nil); resp.StatusCode < 400 {
			t.Fatal(p, resp.StatusCode)
		}
	}
	fake.mu.Lock()
	if len(fake.seen) != before {
		t.Fatal("blocked route reached Codeman", fake.seen[len(fake.seen)-1].URL)
	}
	fake.mu.Unlock()
	// Une écriture venant d'une autre origine est refusée avant tout relais.
	resp, _ = codemanDo(t, "DELETE", loom.URL+"/api/codeman/v1/sessions/abc", "", map[string]string{"Origin": "http://evil.example", "Sec-Fetch-Site": "cross-site"})
	if resp.StatusCode != 403 {
		t.Fatal("cross-origin write reached Codeman", resp.StatusCode)
	}
}

func TestCodemanLaunchFollowsLoomPolicy(t *testing.T) {
	fake, loom := codemanTestServers(t)
	if err := sealProviderSecret(codemanSecretID, "synthetic-codeman"); err != nil {
		t.Fatal(err)
	}
	if resp, out := codemanDo(t, "POST", loom.URL+"/api/codeman/v1/sessions", `{"workingDir":"/work","mode":"codex"}`, nil); resp.StatusCode != 200 || out["data"].(map[string]any)["body"] != `{"workingDir":"/work","mode":"codex"}` {
		t.Fatal("default policy must allow and forward the exact body", resp.StatusCode, out)
	}
	savePolicyRules(t, policy.Rule{ID: "deny-codeman", Scope: "global", Subject: "codeman.session", Decision: policy.Deny})
	fake.mu.Lock()
	before := len(fake.seen)
	fake.mu.Unlock()
	for _, p := range []string{"sessions", "quick-start", "sessions/abc/interactive", "cron/jobs"} {
		if resp, _ := codemanDo(t, "POST", loom.URL+"/api/codeman/v1/"+p, `{"workingDir":"/work","mode":"claude"}`, nil); resp.StatusCode != 403 {
			t.Fatal(p, "launch not denied", resp.StatusCode)
		}
	}
	// Lire et écrire dans une session existante restent possibles.
	if resp, _ := codemanDo(t, "GET", loom.URL+"/api/codeman/v1/sessions", "", nil); resp.StatusCode != 200 {
		t.Fatal(resp.StatusCode)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.seen) != before+1 {
		t.Fatal("a denied launch reached Codeman")
	}
}

func TestCodemanStatusAndEventStream(t *testing.T) {
	_, loom := codemanTestServers(t)
	if _, out := codemanDo(t, "GET", loom.URL+"/api/codeman/status", "", nil); out["reachable"] != true || out["auth"] != "rejected" {
		t.Fatal(out)
	}
	if err := sealProviderSecret(codemanSecretID, "synthetic-codeman"); err != nil {
		t.Fatal(err)
	}
	if _, out := codemanDo(t, "GET", loom.URL+"/api/codeman/status", "", nil); out["auth"] != "ok" || out["version"] != "1.43.0" {
		t.Fatal(out)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", loom.URL+"/api/codeman/v1/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatal(resp.Header)
	}
	line, err := bufio.NewReader(resp.Body).ReadString('\n')
	if err != nil || line != "event: sse:heartbeat\n" {
		t.Fatal("event stream was buffered", line, err)
	}
}

func TestCodemanUnreachable(t *testing.T) {
	_, loom := codemanTestServers(t)
	if err := saveCodemanConfig(codemanConfig{URL: "http://127.0.0.1:1", Username: "admin"}); err != nil {
		t.Fatal(err)
	}
	resp, out := codemanDo(t, "GET", loom.URL+"/api/codeman/v1/sessions", "", nil)
	if resp.StatusCode != 502 || out["errorCode"] != "CODEMAN_UNREACHABLE" {
		t.Fatal(resp.StatusCode, out)
	}
	if _, out := codemanDo(t, "GET", loom.URL+"/api/codeman/status", "", nil); out["reachable"] != false {
		t.Fatal(out)
	}
}

func TestCodemanTerminalBridge(t *testing.T) {
	_, loom := codemanTestServers(t)
	if err := sealProviderSecret(codemanSecretID, "synthetic-codeman"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws := "ws" + strings.TrimPrefix(loom.URL, "http") + "/api/codeman/ws/abc?cid=loom-1"
	c, _, err := websocket.Dial(ctx, ws, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	_, first, err := c.Read(ctx)
	if err != nil || !strings.Contains(string(first), "ready abc") {
		t.Fatal(string(first), err)
	}
	_ = c.Write(ctx, websocket.MessageText, []byte(`{"t":"i","d":"ls\r"}`))
	_, echo, err := c.Read(ctx)
	if err != nil || string(echo) != `echo:{"t":"i","d":"ls\r"}` {
		t.Fatal(string(echo), err)
	}
	_ = c.Write(ctx, websocket.MessageText, []byte(`{"t":"i","d":"bye"}`))
	_, _, _ = c.Read(ctx)
	_, _, err = c.Read(ctx)
	if websocket.CloseStatus(err) != 4009 {
		t.Fatal("upstream close code not forwarded", err)
	}
	if _, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(loom.URL, "http")+"/api/codeman/ws/..%2fx", nil); err == nil {
		t.Fatal("invalid session id accepted")
	}
}
