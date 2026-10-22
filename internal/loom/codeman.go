package loom

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/lucas-lepajollec/loom/internal/loom/policy"
	"github.com/lucas-lepajollec/loom/internal/loom/web"
)

// Passerelle Codeman : Loom offre une interface à un serveur Codeman
// indépendant (sessions, tmux, CLI natifs et leurs comptes), via son API
// publique versionnée /api/v1, son flux SSE et le WebSocket du terminal.
// Loom n'exécute aucun agent et ne réimplémente rien de Codeman : il relaie
// les requêtes du navigateur authentifié en ajoutant, côté serveur, les
// identifiants Codeman. Le mot de passe ne quitte jamais ce processus.

const (
	codemanSecretID      = "codeman-gateway"
	codemanDefaultURL    = "http://127.0.0.1:3000"
	codemanDefaultUser   = "admin"
	codemanBodyLimit     = 64 << 20
	codemanWSReadLimit   = 8 << 20
	codemanProbeDeadline = 6 * time.Second
)

type codemanConfig struct {
	URL      string `json:"url"`
	Username string `json:"username"`
	// UIURL est l'adresse de l'interface officielle telle que le navigateur
	// l'atteint (le serveur peut joindre Codeman en loopback, pas le téléphone).
	UIURL string `json:"ui_url,omitempty"`
}

var codemanMu sync.Mutex

func codemanConfigPath() string { return filepath.Join(LoomHome(), "codeman.json") }

func loadCodemanConfig() codemanConfig {
	c := codemanConfig{URL: codemanDefaultURL, Username: codemanDefaultUser}
	b, err := os.ReadFile(codemanConfigPath())
	if err == nil {
		var saved codemanConfig
		if json.Unmarshal(b, &saved) == nil {
			if saved.URL != "" {
				c.URL = saved.URL
			}
			if saved.Username != "" {
				c.Username = saved.Username
			}
			c.UIURL = saved.UIURL
		}
	}
	return c
}

func saveCodemanConfig(c codemanConfig) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(LoomHome(), 0700); err != nil {
		return err
	}
	return memWriteFileAtomic(codemanConfigPath(), b, 0600)
}

func codemanPassword() string {
	p, err := readProviderSecret(codemanSecretID)
	if err != nil {
		return ""
	}
	return p
}

var codemanUserRe = regexp.MustCompile(`^[A-Za-z0-9._@-]{1,64}$`)

// Une adresse de base : schéma http(s), hôte, port facultatif et, si
// Codeman est servi sous un sous-chemin (CODEMAN_BASE_URL), ce chemin. Sans
// requête ni identifiants (ceux-ci sont stockés à part, scellés).
func normalizeCodemanURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw != "" && !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", errors.New("invalid Codeman address: use http(s)://host:port")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" || strings.Contains(u.Path, "..") || strings.Contains(u.Path, "/api/") {
		return "", errors.New("invalid Codeman address: no credentials, API path or query")
	}
	return u.Scheme + "://" + u.Host + strings.TrimRight(u.Path, "/"), nil
}

func codemanLoopback(base string) bool {
	u, err := url.Parse(base)
	if err != nil {
		return false
	}
	h := u.Hostname()
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// Un Codeman lancé avec --https sur cette machine présente un certificat
// auto-signé : accepté seulement en loopback, vérifié partout ailleurs.
var codemanLoopbackTLS = &http.Client{
	Transport: &http.Transport{
		Proxy:                 nil,
		DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: true},
		TLSHandshakeTimeout:   5 * time.Second,
		MaxIdleConnsPerHost:   16,
		IdleConnTimeout:       90 * time.Second,
		ExpectContinueTimeout: time.Second,
	},
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

func codemanHTTP(base string) *http.Client {
	if strings.HasPrefix(base, "https://") && codemanLoopback(base) {
		return codemanLoopbackTLS
	}
	return codemanClient
}

func codemanAuthHeader(c codemanConfig) string {
	p := codemanPassword()
	if p == "" {
		return ""
	}
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(c.Username+":"+p))
}

// Pas de délai global : les attentes longues de Codeman tiennent jusqu'à
// 600 s et le flux SSE reste ouvert. Le contexte du navigateur borne tout.
var codemanClient = &http.Client{
	Transport: &http.Transport{
		Proxy:                 nil,
		DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   5 * time.Second,
		MaxIdleConnsPerHost:   16,
		IdleConnTimeout:       90 * time.Second,
		ExpectContinueTimeout: time.Second,
	},
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

func registerCodemanRoutes(mux *http.ServeMux, api func(string, http.HandlerFunc)) {
	api("/api/codeman/config", handleCodemanConfig)
	api("/api/codeman/status", handleCodemanStatus)
	api("/api/codeman/discover", handleCodemanDiscover)
	api("/api/codeman/connect", handleCodemanConnect)
	api("/api/codeman/disconnect", handleCodemanDisconnect)
	api("/api/codeman/quotas", handleCodemanQuotas)
	// Codeman emploie PUT/PATCH/DELETE : la passerelle est enregistrée hors du
	// filtre GET/POST des anciennes routes, avec la même authentification et la
	// même protection d'origine (requireWebAuth), et reste révocable.
	mux.HandleFunc("/api/codeman/v1/", requireWebAuth(revocableControlStream(handleCodemanProxy)))
	mux.HandleFunc("/api/codeman/ws/{id}", requireWebAuth(handleCodemanWS))
}

func handleCodemanConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		c := loadCodemanConfig()
		_, err := os.Stat(codemanConfigPath())
		sendJSON(w, 200, map[string]any{"ok": true, "configured": err == nil, "url": c.URL, "username": c.Username, "ui_url": c.UIURL, "password_set": codemanPassword() != ""})
	case http.MethodPost:
		var req struct {
			URL           string `json:"url"`
			Username      string `json:"username"`
			UIURL         string `json:"ui_url"`
			Password      string `json:"password"`
			ClearPassword bool   `json:"clear_password"`
		}
		if !workspaceDecode(w, r, &req) {
			return
		}
		base, err := normalizeCodemanURL(req.URL)
		if err != nil {
			sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		ui := ""
		if strings.TrimSpace(req.UIURL) != "" {
			if ui, err = normalizeCodemanURL(req.UIURL); err != nil {
				sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
				return
			}
		}
		user := strings.TrimSpace(req.Username)
		if user == "" {
			user = codemanDefaultUser
		}
		if !codemanUserRe.MatchString(user) {
			sendJSON(w, 400, map[string]any{"ok": false, "error": "invalid Codeman username"})
			return
		}
		if len(req.Password) > 1024 || strings.ContainsAny(req.Password, "\r\n") {
			sendJSON(w, 400, map[string]any{"ok": false, "error": "invalid Codeman password"})
			return
		}
		codemanMu.Lock()
		defer codemanMu.Unlock()
		if req.ClearPassword {
			if err := deleteProviderSecret(codemanSecretID); err != nil {
				sendJSON(w, 500, map[string]any{"ok": false, "error": "could not remove the Codeman password"})
				return
			}
		} else if req.Password != "" {
			if err := sealProviderSecret(codemanSecretID, req.Password); err != nil {
				sendJSON(w, 500, map[string]any{"ok": false, "error": "could not store the Codeman password"})
				return
			}
		}
		if err := saveCodemanConfig(codemanConfig{URL: base, Username: user, UIURL: ui}); err != nil {
			sendJSON(w, 500, map[string]any{"ok": false, "error": "could not save the Codeman configuration"})
			return
		}
		sendJSON(w, 200, map[string]any{"ok": true, "url": base, "username": user, "ui_url": ui, "password_set": codemanPassword() != ""})
	default:
		w.Header().Set("Allow", "GET, POST")
		sendJSON(w, 405, map[string]any{"ok": false, "error": "method not allowed"})
	}
}

// État de la connexion : joignable, authentification acceptée, version.
func handleCodemanStatus(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodGet) {
		return
	}
	c := loadCodemanConfig()
	out := map[string]any{"ok": true, "url": c.URL, "ui_url": codemanUIURL(c, r), "password_set": codemanPassword() != "", "reachable": false, "auth": "unknown"}
	ctx, cancel := context.WithTimeout(r.Context(), codemanProbeDeadline)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.URL+"/api/v1/me", nil)
	if h := codemanAuthHeader(c); h != "" {
		req.Header.Set("Authorization", h)
	}
	resp, err := codemanHTTP(c.URL).Do(req)
	if err != nil {
		out["error"] = "unreachable"
		sendJSON(w, 200, out)
		return
	}
	defer resp.Body.Close()
	out["reachable"] = true
	if resp.StatusCode == http.StatusUnauthorized {
		out["auth"] = "rejected"
		sendJSON(w, 200, out)
		return
	}
	var me struct {
		Success bool `json:"success"`
		Data    struct {
			Username  string `json:"username"`
			Role      string `json:"role"`
			MultiUser bool   `json:"multiUser"`
		} `json:"data"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&me) != nil || !me.Success {
		out["auth"] = "unknown"
		out["error"] = fmt.Sprintf("unexpected response %d", resp.StatusCode)
		sendJSON(w, 200, out)
		return
	}
	out["auth"] = "ok"
	out["user"] = me.Data.Username
	out["role"] = me.Data.Role
	out["multi_user"] = me.Data.MultiUser
	// La version vient de /status (contrat stable) ; un échec ne masque pas l'accès.
	vreq, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.URL+"/api/v1/status", nil)
	vreq.Header = req.Header.Clone()
	if vresp, err := codemanHTTP(c.URL).Do(vreq); err == nil {
		var st struct {
			Data struct {
				Version string `json:"version"`
			} `json:"data"`
		}
		if json.NewDecoder(io.LimitReader(vresp.Body, 4<<20)).Decode(&st) == nil {
			out["version"] = st.Data.Version
		}
		vresp.Body.Close()
	}
	sendJSON(w, 200, out)
}

// Adresse de l'interface officielle pour ce navigateur. Un Codeman joint en
// loopback par le serveur est proposé sur l'hôte que le navigateur utilise.
func codemanUIURL(c codemanConfig, r *http.Request) string {
	if c.UIURL != "" {
		return c.UIURL
	}
	u, err := url.Parse(c.URL)
	if err != nil {
		return c.URL
	}
	host := u.Hostname()
	if ip := net.ParseIP(host); (ip != nil && ip.IsLoopback()) || host == "localhost" {
		if browser, _, err := net.SplitHostPort(r.Host); err == nil && browser != "" {
			u.Host = net.JoinHostPort(browser, u.Port())
		} else if r.Host != "" && !strings.Contains(r.Host, ":") {
			u.Host = net.JoinHostPort(r.Host, u.Port())
		}
	}
	return u.Scheme + "://" + u.Host
}

// Chemins jamais relayés : la session Loom n'ouvre ni ne ferme de session de
// connexion Codeman, et les hooks restent l'affaire des CLI eux-mêmes.
func codemanPathBlocked(p string) bool {
	for _, prefix := range []string{"auth/", "logout", "hook-event"} {
		if p == strings.TrimSuffix(prefix, "/") || strings.HasPrefix(p, prefix) {
			return true
		}
	}
	return false
}

// Routes qui lancent un CLI : elles passent par la politique de Loom, comme
// l'ouverture d'un terminal (sujet codeman.session, autorisé par défaut).
var codemanLaunchRe = regexp.MustCompile(`^(sessions|quick-start|ralph-loop/start|cron/jobs|cron/jobs/[^/]+/run|reboot-restore/restore|sessions/[^/]+/(interactive|shell|interactive-respawn))$`)

func codemanLaunchPolicy(w http.ResponseWriter, r *http.Request, clean string) bool {
	if r.Method != http.MethodPost || !codemanLaunchRe.MatchString(clean) {
		return true
	}
	in := policy.Input{Subject: "codeman.session", MachineID: "codeman", Fallback: policy.Allow}
	if r.Body != nil && r.ContentLength != 0 {
		raw, err := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
		_ = r.Body.Close()
		if err != nil || len(raw) > 1<<20 {
			codemanError(w, 413, "INVALID_INPUT", "request too large")
			return false
		}
		r.Body = io.NopCloser(strings.NewReader(string(raw)))
		r.ContentLength = int64(len(raw))
		var spec struct {
			WorkingDir string `json:"workingDir"`
			Mode       string `json:"mode"`
			AgentType  string `json:"agentType"`
			CaseName   string `json:"caseName"`
		}
		if json.Unmarshal(raw, &spec) == nil {
			in.Workdir = spec.WorkingDir
			in.Command = spec.Mode
			if in.Command == "" {
				in.Command = spec.AgentType
			}
		}
	}
	if err := workspaceSessions.authorizePolicy(r.Context(), in, false); err != nil {
		codemanError(w, 403, "FORBIDDEN", err.Error())
		return false
	}
	return true
}

func codemanError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Cache-Control", "no-store")
	web.SendJSON(w, status, map[string]any{"success": false, "error": msg, "errorCode": code})
}

var codemanForwardRequestHeaders = []string{"Content-Type", "Accept", "Range", "If-None-Match", "If-Modified-Since"}
var codemanForwardResponseHeaders = []string{"Content-Type", "Content-Length", "Content-Disposition", "Content-Range", "Accept-Ranges", "Cache-Control", "Last-Modified", "ETag", "Content-Security-Policy", "X-Content-Type-Options"}

func handleCodemanProxy(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.EscapedPath(), "/api/codeman/v1/")
	clean := strings.TrimPrefix(path.Clean("/"+rest), "/")
	if rest == "" || clean != strings.TrimSuffix(rest, "/") || strings.Contains(rest, "%2e") || strings.Contains(rest, "%2E") {
		codemanError(w, 400, "INVALID_INPUT", "invalid Codeman path")
		return
	}
	if codemanPathBlocked(clean) {
		codemanError(w, 403, "FORBIDDEN", "this Codeman route is not available through Loom")
		return
	}
	if !codemanLaunchPolicy(w, r, clean) {
		return
	}
	c := loadCodemanConfig()
	target := c.URL + "/api/v1/" + rest
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	// Un corps vide part sans corps : sinon le client Go l'enverrait en
	// « chunked » et Codeman (Fastify) le refuse faute de Content-Type.
	var body io.Reader = http.NoBody
	if r.Body != nil && r.ContentLength != 0 && r.Method != http.MethodGet && r.Method != http.MethodHead {
		body = http.MaxBytesReader(w, r.Body, codemanBodyLimit)
	}
	req, err := http.NewRequestWithContext(r.Context(), r.Method, target, body)
	if err != nil {
		codemanError(w, 400, "INVALID_INPUT", "invalid Codeman request")
		return
	}
	if r.ContentLength > 0 {
		req.ContentLength = r.ContentLength
	} else if body == http.NoBody {
		req.ContentLength = 0
	}
	for _, h := range codemanForwardRequestHeaders {
		if v := r.Header.Get(h); v != "" {
			req.Header.Set(h, v)
		}
	}
	if auth := codemanAuthHeader(c); auth != "" {
		req.Header.Set("Authorization", auth)
	}
	// Les envois de fichiers de Codeman exigent une origine identique ou cet
	// en-tête (un client sans navigateur). La requête du navigateur a déjà
	// passé la protection d'origine de Loom ; Loom parle ensuite en son nom.
	req.Header.Set("X-Codeman-CSRF", "1")
	resp, err := codemanHTTP(c.URL).Do(req)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		codemanError(w, 502, "CODEMAN_UNREACHABLE", "Codeman is not reachable at "+c.URL)
		return
	}
	defer resp.Body.Close()
	// Un 401 de Codeman ne doit pas ressembler à une session Loom expirée : le
	// navigateur redemanderait le mot de passe de Loom.
	if resp.StatusCode == http.StatusUnauthorized {
		codemanError(w, 502, "CODEMAN_AUTH", "Codeman rejected the configured credentials")
		return
	}
	for _, h := range codemanForwardResponseHeaders {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	if w.Header().Get("Cache-Control") == "" {
		w.Header().Set("Cache-Control", "no-store")
	}
	streaming := strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream")
	if streaming {
		w.Header().Del("Content-Length")
		w.Header().Set("X-Accel-Buffering", "no")
	}
	w.WriteHeader(resp.StatusCode)
	if !streaming {
		_, _ = io.Copy(w, resp.Body)
		return
	}
	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 32<<10)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err != nil {
			return
		}
	}
}

var codemanSessionIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`)
var codemanClientIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// Pont WebSocket du terminal : navigateur ⇄ Loom ⇄ Codeman. Les trames JSON
// du protocole Codeman passent telles quelles ; Loom n'interprète rien.
func handleCodemanWS(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !codemanSessionIDRe.MatchString(id) {
		http.Error(w, "invalid session", http.StatusBadRequest)
		return
	}
	grant, err := controlOwner(r)
	if err != nil || !grant.valid() {
		webAuthUnavailable(w)
		return
	}
	c := loadCodemanConfig()
	u, err := url.Parse(c.URL)
	if err != nil {
		http.Error(w, "invalid Codeman address", http.StatusBadGateway)
		return
	}
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/ws/sessions/" + id + "/terminal"
	if cid := r.URL.Query().Get("cid"); codemanClientIDRe.MatchString(cid) {
		u.RawQuery = url.Values{"cid": {cid}}.Encode()
	}
	header := http.Header{}
	if auth := codemanAuthHeader(c); auth != "" {
		header.Set("Authorization", auth)
	}
	dialCtx, cancelDial := context.WithTimeout(r.Context(), 8*time.Second)
	upstream, resp, err := websocket.Dial(dialCtx, u.String(), &websocket.DialOptions{HTTPHeader: header, HTTPClient: codemanHTTP(c.URL)})
	cancelDial()
	if err != nil {
		status, msg := http.StatusBadGateway, "Codeman terminal unavailable"
		if resp != nil && resp.StatusCode == http.StatusUnauthorized {
			msg = "Codeman rejected the configured credentials"
		}
		http.Error(w, msg, status)
		return
	}
	upstream.SetReadLimit(codemanWSReadLimit)
	defer upstream.CloseNow()
	conn, err := web.AcceptWebSocket(w, r, codemanWSReadLimit)
	if err != nil {
		return
	}
	defer conn.CloseNow()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type closing struct {
		code   websocket.StatusCode
		reason string
	}
	done := make(chan closing, 2)
	pump := func(from, to *websocket.Conn) {
		for {
			typ, data, err := from.Read(ctx)
			if err != nil {
				code := websocket.CloseStatus(err)
				var ce websocket.CloseError
				reason := ""
				if errors.As(err, &ce) {
					reason = ce.Reason
				}
				done <- closing{code, reason}
				return
			}
			if err := to.Write(ctx, typ, data); err != nil {
				done <- closing{websocket.StatusGoingAway, ""}
				return
			}
		}
	}
	go pump(upstream, conn)
	go pump(conn, upstream)
	recheck := time.NewTicker(time.Second)
	defer recheck.Stop()
	for {
		select {
		case <-recheck.C:
			if !grant.valid() {
				conn.Close(websocket.StatusPolicyViolation, "access revoked")
				return
			}
		case end := <-done:
			code := end.code
			if code < 1000 || code == websocket.StatusNoStatusRcvd || code == websocket.StatusAbnormalClosure {
				code = websocket.StatusGoingAway
			}
			conn.Close(code, end.reason)
			upstream.Close(websocket.StatusNormalClosure, "")
			return
		}
	}
}
