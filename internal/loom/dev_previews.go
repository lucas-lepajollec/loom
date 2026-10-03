package loom

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// An application preview must have its own browser origin. Proxying arbitrary
// developer JavaScript under Loom's origin would give it control-plane access.
// Each preview owns one listener, transport, expiring grants and SSH processes.
type devPreviewInfo struct {
	ID      string `json:"id"`
	Target  string `json:"target"`
	Port    int    `json:"port"`
	Origin  string `json:"origin"`
	Expires int64  `json:"expires"`
}
type previewGrant struct {
	Expires  time.Time
	Owner    string
	Password string
	Key      string
}
type previewTicket struct {
	grant   previewGrant
	expires time.Time
}
type devPreview struct {
	devPreviewInfo
	mu        sync.Mutex
	tickets   map[string]previewTicket
	grants    map[string]previewGrant
	proxy     *httputil.ReverseProxy
	transport *http.Transport
	server    *http.Server
	listener  net.Listener
	ctx       context.Context
	cancel    context.CancelFunc
	closeOnce sync.Once
}
type devPreviewManager struct {
	sync.Mutex
	items map[string]*devPreview
}

func newDevPreviewManager() *devPreviewManager {
	return &devPreviewManager{items: map[string]*devPreview{}}
}
func (m *devPreviewManager) shutdown() {
	m.Lock()
	items := m.items
	m.items = map[string]*devPreview{}
	m.Unlock()
	for _, p := range items {
		p.close()
	}
}
func (p *devPreview) close() {
	p.closeOnce.Do(func() { p.cancel(); _ = p.server.Close(); p.transport.CloseIdleConnections() })
}
func previewOwner(r *http.Request) (previewGrant, error) {
	_, credential, err := readWebPassword()
	if err != nil {
		return previewGrant{}, errors.New("authentication unavailable")
	}
	key, err := webKeyHashErr()
	if err != nil {
		return previewGrant{}, errors.New("authentication unavailable")
	}
	owner := ""
	if valid, _ := webSessionValid(r, credential); valid {
		owner = sessionRecordKey(r)
	}
	return previewGrant{Expires: time.Now().Add(8 * time.Hour), Owner: owner, Password: hashWebKey(string(credential)), Key: key}, nil
}
func (g previewGrant) valid() bool {
	if time.Now().After(g.Expires) || (memEncActive() && !memUnlocked()) {
		return false
	}
	_, credential, err := readWebPassword()
	if err != nil || hashWebKey(string(credential)) != g.Password {
		return false
	}
	key, err := webKeyHashErr()
	if err != nil || key != g.Key {
		return false
	}
	if g.Owner != "" {
		var s webSession
		if !getStoreJSON(bkState, g.Owner, &s) || s.Expires <= time.Now().Unix() || s.Credential != hashWebKey(string(credential)) {
			return false
		}
	}
	return true
}
func (p *devPreview) ticket(r *http.Request) (string, error) {
	if p.ctx.Err() != nil || time.Now().Unix() >= p.Expires {
		return "", errors.New("preview expired")
	}
	grant, err := previewOwner(r)
	if err != nil {
		return "", err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for k, v := range p.tickets {
		if time.Now().After(v.expires) {
			delete(p.tickets, k)
		}
	}
	for k, v := range p.grants {
		if time.Now().After(v.Expires) {
			delete(p.grants, k)
		}
	}
	if len(p.tickets)+len(p.grants) >= 64 {
		return "", errors.New("too many preview sessions")
	}
	token := randomID(32)
	p.tickets[hashWebKey(token)] = previewTicket{grant, time.Now().Add(time.Minute)}
	return p.Origin + "/__loom_preview/open?ticket=" + url.QueryEscape(token), nil
}
func (p *devPreview) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	origin, _ := url.Parse(p.Origin)
	if !strings.EqualFold(r.Host, origin.Host) || time.Now().Unix() >= p.Expires {
		http.Error(w, "preview unavailable", http.StatusForbidden)
		return
	}
	if r.URL.Path == "/__loom_preview/open" {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", 405)
			return
		}
		token := r.URL.Query().Get("ticket")
		p.mu.Lock()
		ticket, ok := p.tickets[hashWebKey(token)]
		delete(p.tickets, hashWebKey(token))
		p.mu.Unlock()
		if !ok || time.Now().After(ticket.expires) || !ticket.grant.valid() {
			http.Error(w, "open this preview again from Loom", http.StatusUnauthorized)
			return
		}
		session := randomID(32)
		p.mu.Lock()
		p.grants[hashWebKey(session)] = ticket.grant
		p.mu.Unlock()
		http.SetCookie(w, &http.Cookie{Name: "loom_preview_" + p.ID, Value: session, Path: "/", HttpOnly: true, Secure: origin.Scheme == "https", SameSite: http.SameSiteStrictMode, MaxAge: 8 * 60 * 60})
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	cookie, err := r.Cookie("loom_preview_" + p.ID)
	if err != nil {
		http.Error(w, "open this preview from Loom", http.StatusUnauthorized)
		return
	}
	p.mu.Lock()
	grant, ok := p.grants[hashWebKey(cookie.Value)]
	p.mu.Unlock()
	if !ok || !grant.valid() {
		http.Error(w, "preview session expired", http.StatusUnauthorized)
		return
	}
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" || (r.Header.Get("Origin") != "" && r.Header.Get("Origin") != p.Origin) {
		http.Error(w, "preview origin refused", 403)
		return
	}
	p.proxy.ServeHTTP(w, r)
}
func previewProxy(target *url.URL, origin string, id string, transport *http.Transport) *httputil.ReverseProxy {
	cookiePrefix := "loom_app_" + id + "_"
	return &httputil.ReverseProxy{Transport: transport, FlushInterval: -1,
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			// Neither API keys nor any Loom/other-preview cookies reach the application.
			pr.Out.Header.Del("Authorization")
			pr.Out.Header.Del("Proxy-Authorization")
			pr.Out.Header.Del("Cookie")
			for _, c := range pr.In.Cookies() {
				if strings.HasPrefix(c.Name, cookiePrefix) {
					copy := *c
					copy.Name = strings.TrimPrefix(c.Name, cookiePrefix)
					pr.Out.AddCookie(&copy)
				}
			}
			pr.Out.Header.Del("Forwarded")
			pr.Out.Header.Del("X-Forwarded-Host")
			pr.Out.Header.Del("X-Forwarded-Proto")
			pr.Out.Header.Del("X-Forwarded-For")
			pr.Out.Host = target.Host
			if pr.In.Header.Get("Origin") != "" {
				pr.Out.Header.Set("Origin", target.Scheme+"://"+target.Host)
			}
		}, ModifyResponse: func(resp *http.Response) error {
			// Namespace application cookies: a dev server cannot overwrite Loom's login.
			cookies := resp.Cookies()
			resp.Header.Del("Set-Cookie")
			for _, c := range cookies {
				c.Name = cookiePrefix + c.Name
				c.Domain = ""
				c.Path = "/"
				resp.Header.Add("Set-Cookie", c.String())
			}
			if location := resp.Header.Get("Location"); location != "" {
				if u, err := url.Parse(location); err == nil && u.Host == target.Host {
					public, _ := url.Parse(origin)
					u.Scheme, u.Host = public.Scheme, public.Host
					resp.Header.Set("Location", u.String())
				}
			}
			resp.Header.Set("Cache-Control", "no-store")
			resp.Header.Set("Referrer-Policy", "no-referrer")
			return nil
		}, ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			http.Error(w, "development server unavailable; check its port and SSH forwarding", http.StatusBadGateway)
		}}
}
func (m *devPreviewManager) start(ctx context.Context, target string, port int, r *http.Request) (*devPreview, error) {
	if target == "" {
		target = "local"
	}
	if port < 1024 || port > 65535 {
		return nil, errors.New("use a development port between 1024 and 65535")
	}
	if target == "local" && (port == defaultWebPort || port == webBound.port || port == 2511) {
		return nil, errors.New("a Loom control-plane port cannot be previewed")
	}
	var machine RemoteMachine
	if target != "local" {
		var err error
		machine, err = workspaceMachine(target)
		if err != nil {
			return nil, err
		}
	}
	m.Lock()
	defer m.Unlock()
	for id, p := range m.items {
		if time.Now().Unix() >= p.Expires {
			p.close()
			delete(m.items, id)
		}
	}
	for _, p := range m.items {
		if p.Target == target && p.Port == port {
			return p, nil
		}
		if target == "local" && port == p.listener.Addr().(*net.TCPAddr).Port {
			return nil, errors.New("another preview cannot be proxied")
		}
	}
	if len(m.items) >= 8 {
		return nil, errors.New("maximum 8 application previews")
	}
	bind := os.Getenv("LOOM_PREVIEW_BIND")
	if bind == "" {
		host := webBound.host
		if host == "" {
			host = webHost()
		}
		bind = net.JoinHostPort(host, "0")
	}
	listener, err := net.Listen("tcp", bind)
	if err != nil {
		return nil, errors.New("preview listener unavailable")
	}
	closeOnError := true
	defer func() {
		if closeOnError {
			_ = listener.Close()
		}
	}()
	previewPort := listener.Addr().(*net.TCPAddr).Port
	if previewPort == port && target == "local" {
		return nil, errors.New("preview listener conflicts with application port")
	}
	hostname := r.URL.Hostname()
	if hostname == "" {
		u, err := url.Parse("http://" + r.Host)
		if err != nil || u.Hostname() == "" {
			return nil, errors.New("invalid interface host")
		}
		hostname = u.Hostname()
	}
	publicOrigin := strings.ReplaceAll(os.Getenv("LOOM_PREVIEW_ORIGIN"), "{port}", strconv.Itoa(previewPort))
	if publicOrigin == "" {
		if r.TLS != nil || os.Getenv("LOOM_COOKIE_SECURE") == "1" {
			return nil, errors.New("HTTPS needs LOOM_PREVIEW_ORIGIN on a separate preview origin; see the preview setup documentation")
		}
		publicOrigin = "http://" + net.JoinHostPort(hostname, strconv.Itoa(previewPort))
	}
	public, err := url.Parse(publicOrigin)
	if err != nil || (public.Scheme != "http" && public.Scheme != "https") || public.Host == "" || public.User != nil || public.RawQuery != "" || public.Fragment != "" || (public.Path != "" && public.Path != "/") {
		return nil, errors.New("invalid preview origin")
	}
	public.Path = ""
	publicOrigin = public.String()
	if strings.EqualFold(public.Host, r.Host) {
		return nil, errors.New("the preview must use a different origin from Loom")
	}
	for _, p := range m.items {
		if p.Origin == publicOrigin {
			return nil, errors.New("this preview origin is already in use; close its preview first")
		}
	}
	previewCtx, cancel := context.WithCancel(ctx)
	p := &devPreview{devPreviewInfo: devPreviewInfo{randomID(8), target, port, publicOrigin, time.Now().Add(8 * time.Hour).Unix()}, ctx: previewCtx, cancel: cancel, listener: listener, tickets: map[string]previewTicket{}, grants: map[string]previewGrant{}}
	transport := &http.Transport{Proxy: nil, ResponseHeaderTimeout: 15 * time.Second, MaxIdleConnsPerHost: 4, IdleConnTimeout: time.Minute}
	upstream, _ := url.Parse("http://127.0.0.1:" + strconv.Itoa(port))
	if target == "local" {
		transport.DialContext = func(dialCtx context.Context, network, address string) (net.Conn, error) {
			conn, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(dialCtx, network, address)
			if err != nil {
				return nil, err
			}
			return ownPreviewConnection(previewCtx, conn), nil
		}
	} else {
		key, _, err := loomSSHKey()
		if err != nil {
			cancel()
			return nil, errors.New("SSH identity unavailable")
		}
		transport.DialContext = func(dialCtx context.Context, _, _ string) (net.Conn, error) {
			conn, err := previewSSHDial(dialCtx, previewCtx, machine, key, port)
			if err != nil {
				return nil, err
			}
			return ownPreviewConnection(previewCtx, conn), nil
		}
	}
	p.transport = transport
	p.proxy = previewProxy(upstream, publicOrigin, p.ID, transport)
	p.server = &http.Server{Handler: http.HandlerFunc(p.serve), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: time.Minute}
	m.items[p.ID] = p
	closeOnError = false
	go func() { _ = p.server.Serve(listener); p.close() }()
	go func() {
		timer := time.NewTimer(8 * time.Hour)
		defer timer.Stop()
		select {
		case <-previewCtx.Done():
		case <-timer.C:
		}
		p.close()
	}()
	return p, nil
}
func (m *devPreviewManager) handle(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	if !usageVaultAccess(w) {
		return
	}
	if r.Method == http.MethodGet {
		m.Lock()
		list := []devPreviewInfo{}
		for _, p := range m.items {
			if time.Now().Unix() < p.Expires {
				list = append(list, p.devPreviewInfo)
			}
		}
		m.Unlock()
		sendJSON(w, 200, map[string]any{"ok": true, "previews": list})
		return
	}
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		Action  string `json:"action"`
		Target  string `json:"target"`
		Port    int    `json:"port"`
		ID      string `json:"id"`
		Consent bool   `json:"consent"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	if req.Action == "close" {
		m.Lock()
		p := m.items[req.ID]
		delete(m.items, req.ID)
		m.Unlock()
		if p != nil {
			p.close()
		}
		sendJSON(w, 200, map[string]any{"ok": true})
		return
	}
	if !req.Consent {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "confirm opening this development server to an authenticated preview"})
		return
	}
	var p *devPreview
	var err error
	if req.Action == "open" || req.Action == "" {
		p, err = m.start(ctx, req.Target, req.Port, r)
	} else if req.Action == "ticket" {
		m.Lock()
		p = m.items[req.ID]
		m.Unlock()
		if p == nil {
			err = errors.New("preview not found")
		}
	} else {
		err = errors.New("invalid preview action")
	}
	if err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	link, err := p.ticket(r)
	if err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "preview": p.devPreviewInfo, "url": link})
}

// SSH -W opens a TCP stream on the target without starting a remote shell or
// opening an unprotected forwarding port. Only this process belongs to Loom.
type previewSSHConn struct {
	in                    io.WriteCloser
	out                   io.ReadCloser
	cmd                   *exec.Cmd
	once                  sync.Once
	mu                    sync.Mutex
	readTimer, writeTimer *time.Timer
}

func previewSSHDial(dialCtx, ownerCtx context.Context, m RemoteMachine, key string, port int) (net.Conn, error) {
	if err := dialCtx.Err(); err != nil {
		return nil, err
	}
	args := sshArgs(m, key)
	last := args[len(args)-1]
	args = append(args[:len(args)-1], "-W", fmt.Sprintf("127.0.0.1:%d", port), last)
	cmd := exec.CommandContext(ownerCtx, "ssh", args...)
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	// Own the parent pipe ends directly. Cmd.Wait closes StdoutPipe and could
	// truncate buffered HTTP data when a short-lived SSH stream exits.
	childIn, in, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	out, childOut, err := os.Pipe()
	if err != nil {
		_ = childIn.Close()
		_ = in.Close()
		return nil, err
	}
	cmd.Stdin, cmd.Stdout = childIn, childOut
	if err := cmd.Start(); err != nil {
		_ = childIn.Close()
		_ = in.Close()
		_ = out.Close()
		_ = childOut.Close()
		return nil, errors.New("SSH forwarding unavailable")
	}
	_ = childIn.Close()
	_ = childOut.Close()
	conn := &previewSSHConn{in: in, out: out, cmd: cmd}
	go func() { _ = cmd.Wait() }()
	return conn, nil
}
func (c *previewSSHConn) Read(b []byte) (int, error)  { return c.out.Read(b) }
func (c *previewSSHConn) Write(b []byte) (int, error) { return c.in.Write(b) }
func (c *previewSSHConn) Close() error {
	c.once.Do(func() {
		_ = c.in.Close()
		_ = c.out.Close()
		_ = c.cmd.Process.Kill()
		c.mu.Lock()
		if c.readTimer != nil {
			c.readTimer.Stop()
		}
		if c.writeTimer != nil {
			c.writeTimer.Stop()
		}
		c.mu.Unlock()
	})
	return nil
}

type previewAddr string

func (a previewAddr) Network() string          { return "ssh" }
func (a previewAddr) String() string           { return string(a) }
func (c *previewSSHConn) LocalAddr() net.Addr  { return previewAddr("loom-preview") }
func (c *previewSSHConn) RemoteAddr() net.Addr { return previewAddr("remote-loopback") }
func (c *previewSSHConn) deadline(t time.Time, write bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	timer := &c.readTimer
	if write {
		timer = &c.writeTimer
	}
	if *timer != nil {
		(*timer).Stop()
		*timer = nil
	}
	if !t.IsZero() {
		*timer = time.AfterFunc(max(0, time.Until(t)), func() { _ = c.Close() })
	}
	return nil
}
func (c *previewSSHConn) SetDeadline(t time.Time) error {
	_ = c.SetReadDeadline(t)
	return c.SetWriteDeadline(t)
}
func (c *previewSSHConn) SetReadDeadline(t time.Time) error  { return c.deadline(t, false) }
func (c *previewSSHConn) SetWriteDeadline(t time.Time) error { return c.deadline(t, true) }

// Server.Close does not close hijacked WebSockets. The preview lifecycle also
// owns every transport connection so stopping a preview closes HTTP and HMR.
type previewOwnedConn struct {
	net.Conn
	once sync.Once
	done chan struct{}
}

func (c *previewOwnedConn) Close() error {
	var err error
	c.once.Do(func() { err = c.Conn.Close(); close(c.done) })
	return err
}
func ownPreviewConnection(ctx context.Context, conn net.Conn) net.Conn {
	c := &previewOwnedConn{Conn: conn, done: make(chan struct{})}
	go func() {
		select {
		case <-ctx.Done():
			_ = c.Close()
		case <-c.done:
		}
	}()
	return c
}
