package loom

import (
	"context"
	"github.com/coder/websocket"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestPreviewAuthenticatesIsolatesCredentialsAssetsAndWebSockets(t *testing.T) {
	testHome(t)
	t.Setenv("LOOM_PREVIEW_BIND", "127.0.0.1:0")
	t.Setenv("LOOM_PREVIEW_ORIGIN", "")
	t.Setenv("LOOM_COOKIE_SECURE", "")
	_ = storeWebKey("synthetic-preview-owner")
	leaked := make(chan bool, 8)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" || (strings.Contains(r.Header.Get("Cookie"), "synthetic-main-cookie") || strings.Contains(r.Header.Get("Cookie"), "synthetic-other-preview") || strings.Contains(r.Header.Get("Cookie"), "loom_preview_")) {
			leaked <- true
		}
		if r.URL.Path == "/hmr" {
			c, err := websocket.Accept(w, r, nil)
			if err != nil {
				return
			}
			defer c.CloseNow()
			for {
				typ, data, err := c.Read(r.Context())
				if err != nil {
					return
				}
				if err = c.Write(r.Context(), typ, data); err != nil {
					return
				}
			}
		}
		if r.URL.Path == "/asset.js" {
			w.Header().Set("Content-Type", "text/javascript")
			io.WriteString(w, "console.log('fixture asset')")
			return
		}
		if r.URL.Path == "/cookie" {
			io.WriteString(w, r.Header.Get("Cookie"))
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "loom_session", Value: "app-owned", Path: "/"})
		w.Header().Set("Content-Type", "text/html")
		io.WriteString(w, `<script src="/asset.js"></script>`)
	}))
	defer upstream.Close()
	u, _ := url.Parse(upstream.URL)
	port, _ := strconv.Atoi(u.Port())
	manager := newDevPreviewManager()
	defer manager.shutdown()
	owner := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:2590/api/previews", nil)
	owner.Header.Set("Authorization", "Bearer synthetic-preview-owner")
	p, err := manager.start(context.Background(), "local", port, owner)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.Get(p.Origin + "/asset.js")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatal("preview accessible without a grant")
	}
	link, err := p.ticket(owner)
	if err != nil {
		t.Fatal(err)
	}
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 3 * time.Second}
	previewURL, _ := url.Parse(p.Origin)
	jar.SetCookies(previewURL, []*http.Cookie{{Name: "loom_session", Value: "synthetic-main-cookie"}, {Name: "loom_preview_other", Value: "synthetic-other-preview"}})
	response, err = client.Get(link)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 200 || !strings.Contains(string(data), "/asset.js") {
		t.Fatal("authenticated app document unavailable")
	}
	if response.Request.URL.RawQuery != "" {
		t.Fatal("one-time ticket left in application URL")
	}
	response, _ = client.Get(link)
	response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatal("bootstrap ticket reused")
	}
	request, _ := http.NewRequest(http.MethodGet, p.Origin+"/asset.js", nil)
	request.Header.Set("Authorization", "Bearer synthetic-main-access")
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	data, _ = io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 200 || !strings.Contains(string(data), "fixture asset") {
		t.Fatal("root asset not proxied")
	}
	response, _ = client.Get(p.Origin + "/cookie")
	data, _ = io.ReadAll(response.Body)
	response.Body.Close()
	if string(data) != "loom_session=app-owned" {
		t.Fatal("application cookies lost their namespace")
	}
	select {
	case <-leaked:
		t.Fatal("Loom credentials forwarded to the application")
	default:
	}
	request, _ = http.NewRequest(http.MethodPost, p.Origin+"/", nil)
	request.Header.Set("Origin", "http://foreign.example")
	response, _ = client.Do(request)
	response.Body.Close()
	if response.StatusCode != 403 {
		t.Fatal("foreign origin accepted")
	}
	headers := http.Header{}
	headers.Set("Origin", p.Origin)
	for _, c := range jar.Cookies(previewURL) {
		if c.Name == "loom_preview_"+p.ID {
			headers.Set("Cookie", c.Name+"="+c.Value)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, strings.Replace(p.Origin, "http:", "ws:", 1)+"/hmr", &websocket.DialOptions{HTTPHeader: headers})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	if err := ws.Write(ctx, websocket.MessageText, []byte("hot reload")); err != nil {
		t.Fatal(err)
	}
	_, data, err = ws.Read(ctx)
	if err != nil || string(data) != "hot reload" {
		t.Fatal("HMR websocket not forwarded")
	}
	p.close()
	if _, _, err = ws.Read(ctx); err == nil {
		t.Fatal("closing preview left its upgraded connection open")
	}
}

func TestPreviewUpgradedConnectionRevokesWhenOwnerKeyChanges(t *testing.T) {
	testHome(t)
	t.Setenv("LOOM_PREVIEW_BIND", "127.0.0.1:0")
	t.Setenv("LOOM_PREVIEW_ORIGIN", "")
	t.Setenv("LOOM_COOKIE_SECURE", "")
	_ = storeWebKey("synthetic-live-preview")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/hmr" {
			w.WriteHeader(200)
			return
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		for {
			typ, data, err := conn.Read(r.Context())
			if err != nil {
				return
			}
			if conn.Write(r.Context(), typ, data) != nil {
				return
			}
		}
	}))
	defer upstream.Close()
	u, _ := url.Parse(upstream.URL)
	port, _ := strconv.Atoi(u.Port())
	manager := newDevPreviewManager()
	defer manager.shutdown()
	owner := localTestRequest("POST", "http://127.0.0.1:2590/api/previews", nil)
	owner.Header.Set("Authorization", "Bearer synthetic-live-preview")
	p, err := manager.start(context.Background(), "local", port, owner)
	if err != nil {
		t.Fatal(err)
	}
	link, err := p.ticket(owner)
	if err != nil {
		t.Fatal(err)
	}
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 3 * time.Second}
	resp, err := client.Get(link)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(p.Origin, "http")+"/hmr", &websocket.DialOptions{HTTPClient: client})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	_ = storeWebKey("synthetic-live-replacement")
	if _, _, err = ws.Read(ctx); err == nil || ctx.Err() != nil {
		t.Fatal("revoked preview WebSocket remained open")
	}
}
func TestPreviewRefusesControlPlaneAndInvalidatesOnOwnerCredentialChange(t *testing.T) {
	testHome(t)
	t.Setenv("LOOM_PREVIEW_BIND", "127.0.0.1:0")
	t.Setenv("LOOM_PREVIEW_ORIGIN", "")
	t.Setenv("LOOM_COOKIE_SECURE", "")
	manager := newDevPreviewManager()
	defer manager.shutdown()
	owner := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:2590/api/previews", nil)
	for _, port := range []int{22, defaultWebPort, 2511, 65536} {
		if _, err := manager.start(context.Background(), "local", port, owner); err == nil {
			t.Fatal("invalid/control-plane target accepted")
		}
	}
	_ = storeWebKey("synthetic-first")
	owner.Header.Set("Authorization", "Bearer synthetic-first")
	p, err := manager.start(context.Background(), "local", 5173, owner)
	if err != nil {
		t.Fatal(err)
	}
	link, err := p.ticket(owner)
	if err != nil {
		t.Fatal(err)
	}
	_ = storeWebKey("synthetic-replacement")
	response, err := http.Get(link)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatal("credential rotation did not invalidate grant")
	}
	t.Setenv("LOOM_COOKIE_SECURE", "1")
	if _, err := manager.start(context.Background(), "local", 8080, owner); err == nil {
		t.Fatal("HTTPS main interface silently downgraded preview transport")
	}
}

func TestPreviewFramingAndTicketPath(t *testing.T) {
	testHome(t)
	t.Setenv("LOOM_PREVIEW_BIND", "127.0.0.1:0")
	t.Setenv("LOOM_PREVIEW_ORIGIN", "")
	t.Setenv("LOOM_COOKIE_SECURE", "")
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'; script-src 'self'")
		io.WriteString(w, r.URL.RequestURI())
	}))
	defer up.Close()
	u, _ := url.Parse(up.URL)
	port, _ := strconv.Atoi(u.Port())
	m := newDevPreviewManager()
	defer m.shutdown()
	req := httptest.NewRequest("POST", "http://127.0.0.1:2594/api/previews", nil)
	p, err := m.start(t.Context(), "local", port, req)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"//outside.invalid", "/%5cwrong", "https://other.test", "/__loom_preview/open"} {
		if _, err := p.ticketFor(req, path); err == nil {
			t.Errorf("accepted path %q", path)
		}
	}
	if _, err := p.ticketFor(httptest.NewRequest("GET", "http://localhost/", nil), "/"); err == nil {
		t.Fatal("ticket from another parent")
	}
	link, err := p.ticketFor(req, "/dashboard?a=1")
	if err != nil {
		t.Fatal(err)
	}
	jar, _ := cookiejar.New(nil)
	client := http.Client{Jar: jar, Timeout: 3 * time.Second}
	resp, err := client.Get(link)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	csp := strings.Join(resp.Header.Values("Content-Security-Policy"), "; ")
	if string(body) != "/dashboard?a=1" || resp.Header.Get("X-Frame-Options") != "" || !strings.Contains(csp, "frame-ancestors http://127.0.0.1:2594") || !strings.Contains(csp, "script-src 'self'") || strings.Contains(csp, "frame-ancestors 'none'") {
		t.Fatalf("framing %q %q", body, csp)
	}
	if previewFrameSource(req) != "http://127.0.0.1:*" {
		t.Fatal("missing UI frame source")
	}
	for _, legacy := range []string{"127.1", "0x7f000001", "2130706433", "0177.0.0.1"} {
		if _, err := previewOrigin("http://" + legacy); err == nil {
			t.Errorf("accepted ambiguous IPv4 host %q", legacy)
		}
	}
	req.Host = "site.invalid;script-src"
	if previewFrameSource(req) != "" {
		t.Fatal("untrusted host entered CSP")
	}
}

func TestPreviewPortDiscovery(t *testing.T) {
	testHome(t)
	excluded, found := map[int]bool{4000: true}, map[int]bool{}
	readListeningPorts(strings.NewReader("0: 0100007F:1435 0 0A\n1: 00000000:0050 0 0A\n2: 00000000:0FA0 0 0A\n3: 0100007F:2328 0 01\n4: 00000000:1435 0 0A\n5: bad 0 0A"), excluded, found)
	if len(found) != 1 || !found[5173] {
		t.Fatal(found)
	}
	m := newDevPreviewManager()
	for _, target := range []string{"local", "remote"} {
		code, out := filesCall(t, m.ports, "GET", "/api/previews/ports?target="+target, "")
		rows, ok := out["ports"].([]any)
		if code != 200 || !ok || (target == "remote" && len(rows) != 0) {
			t.Fatal(out)
		}
		for _, n := range rows {
			if reservedPreviewPorts()[int(n.(float64))] {
				t.Fatal("listed a control port", n)
			}
		}
	}
}
