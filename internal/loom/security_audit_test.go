package loom

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/policy"
	"github.com/lucas-lepajollec/loom/internal/loom/web"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestSecurityAuditUIHeadersCSPAndHSTS(t *testing.T) {
	page, err := nextFS.ReadFile("ui/next/index.html")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(page, []byte(`<html lang="en" data-theme="dark">`)) {
		t.Fatal("index.html must default to lang=en")
	}
	if bytes.Contains(page, []byte(`<script>try{`)) {
		t.Fatal("inline theme bootstrap script must be externalized")
	}
	if !bytes.Contains(page, []byte(`<script src="/next/js/theme-boot.js"></script>`)) {
		t.Fatal("missing static theme-boot.js script tag")
	}

	mux := http.NewServeMux()
	registerWebAssets(mux)

	for _, p := range []string{"/", "/next/js/theme-boot.js", "/next/js/features/voice/pcm16-worklet.js", "/offline.html", "/marked.min.js", "/sw.js", "/manifest.webmanifest"} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", p, nil))
		if w.Code != 200 {
			t.Fatalf("%s returned %d", p, w.Code)
		}
		if w.Header().Get("X-Content-Type-Options") != "nosniff" ||
			w.Header().Get("Referrer-Policy") != "no-referrer" ||
			w.Header().Get("Permissions-Policy") != "camera=(), geolocation=(), payment=(), usb=(), microphone=(self)" {
			t.Fatalf("%s missing global security headers: %v", p, w.Header())
		}
		if p == "/" {
			csp := w.Header().Get("Content-Security-Policy")
			if !strings.Contains(csp, "script-src 'self' 'sha256-") ||
				strings.Contains(csp, "script-src 'self' 'unsafe-inline'") ||
				!strings.Contains(csp, "frame-ancestors 'none'") ||
				!strings.Contains(csp, "object-src 'none'") ||
				!strings.Contains(csp, "base-uri 'none'") ||
				w.Header().Get("Cross-Origin-Opener-Policy") != "same-origin" {
				t.Fatalf("unexpected UI CSP/COOP: %v", w.Header())
			}
		}
	}

	// HSTS is emitted ONLY on TLS connections when mode == "files".
	l := &httpsListener{server: &http.Server{}, config: httpsConfig{Enabled: true, Mode: "self-signed"}}
	reqTLS := httptest.NewRequest("GET", "https://loom.test/", nil)
	reqTLS.TLS = &tls.ConnectionState{}
	if l.shouldSendHSTS(reqTLS) {
		t.Fatal("self-signed HTTPS must not emit HSTS")
	}
	l.config.Mode = "files"
	if l.shouldSendHSTS(httptest.NewRequest("GET", "http://loom.test/", nil)) {
		t.Fatal("plain HTTP must not emit HSTS")
	}
	if !l.shouldSendHSTS(reqTLS) {
		t.Fatal("CA-signed files HTTPS must emit HSTS")
	}
}

func TestSecurityAuditMCPPolicyMachineScopeDefaultsAndRevocation(t *testing.T) {
	gatewayTestHome(t)
	if err := putJSON(bkState, remoteMachinesState, []RemoteMachine{
		{ID: "nas-1", Name: "NAS", Host: "192.168.1.50"},
	}); err != nil {
		t.Fatal(err)
	}
	// Save a machine-scoped deny rule for the NAS node.
	doc := policy.Document{
		Version:  1,
		Migrated: true,
		Rules: []policy.Rule{
			{ID: "deny-nas-run", Scope: "machine", ScopeID: "nas-1", Subject: "mcp.tool:loom/loom_machine_run", Decision: policy.Deny},
		},
	}
	if err := putStoreJSON(bkState, policyStateKey, doc); err != nil {
		t.Fatal(err)
	}

	called := false
	next := func(context.Context, string, mcp.Request) (mcp.Result, error) {
		called = true
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "ok"}}}, nil
	}
	mw := gatewayPolicyMiddleware(map[string]string{
		"agents__start_session": "mcp.tool:agents/start_session",
	})(next)

	// 1. Machine name "NAS" resolves to "nas-1" BEFORE policy.Evaluate -> denied!
	reqNAS := &mcp.ServerRequest[*mcp.CallToolParamsRaw]{
		Params: &mcp.CallToolParamsRaw{
			Name:      "loom_machine_run",
			Arguments: json.RawMessage(`{"machine":"NAS","command":"rm -rf /"}`),
		},
	}
	res, err := mw(context.Background(), "tools/call", reqNAS)
	if err != nil {
		t.Fatal(err)
	}
	ctr := res.(*mcp.CallToolResult)
	if called || !ctr.IsError || !strings.Contains(ctr.Content[0].(*mcp.TextContent).Text, "policy denied") {
		t.Fatalf("machine-scoped deny rule bypassed: called=%v res=%+v", called, ctr)
	}

	// 2. High-impact tools default to Confirm (fail closed when canceled without approval).
	for _, tc := range []struct {
		name string
		args string
	}{
		{"loom_machine_run", `{"machine":"local","command":"id"}`},
		{"agents__start_session", `{"cli":"claude"}`},
	} {
		called = false
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		req := &mcp.ServerRequest[*mcp.CallToolParamsRaw]{
			Params: &mcp.CallToolParamsRaw{Name: tc.name, Arguments: json.RawMessage(tc.args)},
		}
		res, err := mw(ctx, "tools/call", req)
		if err != nil {
			t.Fatal(err)
		}
		ctr := res.(*mcp.CallToolResult)
		if called || !ctr.IsError {
			t.Fatalf("%s executed without approval under default policy: called=%v res=%+v", tc.name, called, ctr)
		}
	}

	// 3. Read-only discovery tools remain Allow by default.
	for _, name := range []string{"loom_overview", "loom_machines"} {
		called = false
		req := &mcp.ServerRequest[*mcp.CallToolParamsRaw]{
			Params: &mcp.CallToolParamsRaw{Name: name, Arguments: json.RawMessage(`{}`)},
		}
		res, err := mw(context.Background(), "tools/call", req)
		if err != nil {
			t.Fatal(err)
		}
		if !called || res.(*mcp.CallToolResult).IsError {
			t.Fatalf("read-only tool %s blocked: called=%v res=%+v", name, called, res)
		}
	}

	// 4. Token revocation via POST {revoke:true} and DELETE.
	if err := storeWebKey("control-token-fixture-24chars!!"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	registerBrainRoutes(mux, nil)
	webAPI(mux)("/api/mcp/gateway/token", handleMCPGatewayToken)

	token, err := rotateGatewayToken()
	if err != nil {
		t.Fatal(err)
	}
	initBody := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`
	mcpCall := func(bearer string) int {
		r := httptest.NewRequest("POST", "http://localhost/mcp/loom", strings.NewReader(initBody))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/json, text/event-stream")
		r.Header.Set("Authorization", "Bearer "+bearer)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w.Code
	}
	if code := mcpCall(token); code != 200 {
		t.Fatalf("active token rejected: %d", code)
	}
	r := httptest.NewRequest("POST", "http://localhost/api/mcp/gateway/token", strings.NewReader(`{"revoke":true}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer control-token-fixture-24chars!!")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"token_set":false`) {
		t.Fatalf("POST revoke failed: %d %s", w.Code, w.Body.String())
	}
	if code := mcpCall(token); code != 401 {
		t.Fatalf("revoked token still accepted: %d", code)
	}

	// DELETE also revokes a freshly created token.
	token2, err := rotateGatewayToken()
	if err != nil {
		t.Fatal(err)
	}
	r = httptest.NewRequest("DELETE", "http://localhost/api/mcp/gateway/token", nil)
	r.Header.Set("Authorization", "Bearer control-token-fixture-24chars!!")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"token_set":false`) {
		t.Fatalf("DELETE revoke failed: %d %s", w.Code, w.Body.String())
	}
	if code := mcpCall(token2); code != 401 {
		t.Fatalf("DELETE-revoked token still accepted: %d", code)
	}
}

func TestSecurityAuditLocalDiscussionToolPolicyEnforced(t *testing.T) {
	testHome(t)
	doc := policy.Document{
		Version:  1,
		Migrated: true,
		Rules: []policy.Rule{
			{ID: "deny-bash", Scope: "global", Subject: "agent.command", Decision: policy.Deny},
			{ID: "deny-write", Scope: "global", Subject: "agent.file_write", Decision: policy.Deny},
		},
	}
	if err := putStoreJSON(bkState, policyStateKey, doc); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "should-not-exist")
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		switch calls.Add(1) {
		case 1:
			args := fmt.Sprintf(`{\"command\":\"touch %s\"}`, marker)
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"c1\",\"type\":\"function\",\"function\":{\"name\":\"bash\",\"arguments\":\"%s\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n", args)
		case 2:
			args := fmt.Sprintf(`{\"file\":\"%s\",\"content\":\"nope\"}`, marker)
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"c2\",\"type\":\"function\",\"function\":{\"name\":\"write\",\"arguments\":\"%s\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n", args)
		default:
			fmt.Fprint(w, sseChunk("blocked cleanly"))
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	if err := SetConfigKey("PORT", u.Port()); err != nil {
		t.Fatal(err)
	}

	var results []string
	_, err := runChat(context.Background(), []Message{{Role: "user", Content: "run"}}, 0.7, Caps{Agent: true}, func(ev StreamEvent) bool {
		if ev.ToolUsed != nil && ev.ToolUsed.Done {
			results = append(results, ev.ToolUsed.Result)
		}
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Fatal("denied local discussion tool executed")
	}
	if len(results) != 2 || !strings.Contains(results[0], "policy denied capability: agent.command") || !strings.Contains(results[1], "policy denied capability: agent.file_write") {
		t.Fatalf("unexpected tool results: %v", results)
	}
}

func TestSecurityAuditEngineAndNodeSecretsSealedAtRest(t *testing.T) {
	testHome(t)
	t.Cleanup(func() { _ = setEngineNode(nil) })

	// 1. engineNode WebKey and APIKey are sealed in loom.db.
	node := &engineNode{
		URL:      "http://192.168.1.20:2510",
		WebKey:   "remote-node-web-secret-value",
		V1:       "http://192.168.1.20:8081",
		APIKey:   "remote-node-api-secret-value",
		Hostname: "gpu-box",
		Role:     "engine-node",
	}
	if err := setEngineNode(node); err != nil {
		t.Fatal(err)
	}
	rawNode := string(getBytes(bkState, engineNodeState))
	if strings.Contains(rawNode, "remote-node-web-secret-value") || strings.Contains(rawNode, "remote-node-api-secret-value") {
		t.Fatalf("engine_node persisted plaintext credentials: %s", rawNode)
	}
	if !strings.Contains(rawNode, inlineSecretPrefix) {
		t.Fatalf("engine_node missing sealed prefix: %s", rawNode)
	}
	// Reset in-memory cache and re-read from DB.
	engineNodeMu.Lock()
	engineNodeCache, engineNodeRead = nil, false
	engineNodeMu.Unlock()
	loaded := currentEngineNode()
	if loaded == nil || loaded.WebKey != "remote-node-web-secret-value" || loaded.APIKey != "remote-node-api-secret-value" {
		t.Fatalf("unsealed engine_node mismatch: %+v", loaded)
	}

	// 2. Paired machine credentials are sealed even when memory vault is off.
	m, err := savePairedMachine(RemoteMachine{Name: "gpu", Host: "192.168.1.20"}, node)
	if err != nil {
		t.Fatal(err)
	}
	rawPaired := string(getBytes(bkState, machineNodePrefix+m.ID))
	if strings.Contains(rawPaired, "remote-node-web-secret-value") || strings.Contains(rawPaired, "remote-node-api-secret-value") {
		t.Fatalf("paired machine persisted plaintext credentials: %s", rawPaired)
	}
	if got := savedMachineNode(m); got == nil || got.WebKey != "remote-node-web-secret-value" || got.APIKey != "remote-node-api-secret-value" {
		t.Fatalf("savedMachineNode unseal failed: %+v", got)
	}

	// 3. engine_internal_capability is sealed in loom.db.
	capSecret := loomInferenceSecret()
	if capSecret == "" {
		t.Fatal("empty internal capability secret")
	}
	rawCap := getStr(bkState, "engine_internal_capability")
	if !strings.HasPrefix(rawCap, inlineSecretPrefix) || strings.Contains(rawCap, capSecret) {
		t.Fatalf("engine_internal_capability stored in plaintext: %q", rawCap)
	}
	if again := loomInferenceSecret(); again != capSecret {
		t.Fatalf("internal capability changed on reload: %q vs %q", again, capSecret)
	}

	// 4. Legacy /v1 api_key is sealed in loom.db.
	if err := writeAPIKey("sk-loom-v1-secret-value"); err != nil {
		t.Fatal(err)
	}
	rawAPIKey := getStr(bkState, "api_key")
	if !strings.HasPrefix(rawAPIKey, inlineSecretPrefix) || strings.Contains(rawAPIKey, "sk-loom-v1-secret-value") {
		t.Fatalf("api_key stored in plaintext: %q", rawAPIKey)
	}
	if readAPIKey() != "sk-loom-v1-secret-value" {
		t.Fatalf("readAPIKey mismatch: %q", readAPIKey())
	}

	// 5. Transparent migration of legacy plaintext bkState records on load.
	if err := putStr(bkState, "api_key", "legacy-plain-api-key"); err != nil {
		t.Fatal(err)
	}
	if got := readAPIKey(); got != "legacy-plain-api-key" {
		t.Fatalf("legacy api_key read failed: %q", got)
	}
	if raw := getStr(bkState, "api_key"); !strings.HasPrefix(raw, inlineSecretPrefix) || strings.Contains(raw, "legacy-plain-api-key") {
		t.Fatalf("legacy api_key not migrated on load: %q", raw)
	}

	if err := putStr(bkState, "engine_internal_capability", "legacy-plain-cap"); err != nil {
		t.Fatal(err)
	}
	if got := loomInferenceSecret(); got != "legacy-plain-cap" {
		t.Fatalf("legacy internal capability read failed: %q", got)
	}
	if raw := getStr(bkState, "engine_internal_capability"); !strings.HasPrefix(raw, inlineSecretPrefix) || strings.Contains(raw, "legacy-plain-cap") {
		t.Fatalf("legacy internal capability not migrated on load: %q", raw)
	}

	if err := putStoreJSON(bkState, engineNodeState, node); err != nil {
		t.Fatal(err)
	}
	engineNodeMu.Lock()
	engineNodeCache, engineNodeRead = nil, false
	engineNodeMu.Unlock()
	if got := currentEngineNode(); got == nil || got.WebKey != "remote-node-web-secret-value" || got.APIKey != "remote-node-api-secret-value" {
		t.Fatalf("legacy engine_node read failed: %+v", got)
	}
	if raw := string(getBytes(bkState, engineNodeState)); strings.Contains(raw, "remote-node-web-secret-value") || !strings.Contains(raw, inlineSecretPrefix) {
		t.Fatalf("legacy engine_node not migrated on load: %s", raw)
	}
}

func TestSecurityAuditP2Hardening(t *testing.T) {
	testHome(t)

	// 1. cmdSetWebKey enforces minimum 24 characters for manual keys.
	if err := cmdSetWebKey([]string{"too-short-key"}); err == nil {
		t.Fatal("cmdSetWebKey accepted key shorter than 24 characters")
	}
	if err := cmdSetWebKey([]string{"this-control-key-is-long-enough-24"}); err != nil {
		t.Fatalf("cmdSetWebKey rejected valid 24+ char key: %v", err)
	}
	if err := cmdSetWebKey([]string{""}); err != nil {
		t.Fatalf("cmdSetWebKey failed to clear key: %v", err)
	}

	// 2. LOOM_TRUST_PROXY (opt-in, loopback direct peer only).
	t.Setenv("LOOM_TRUST_PROXY", "")
	t.Setenv("LOOM_COOKIE_SECURE", "")
	reqLoopback := httptest.NewRequest("POST", "http://loom.example/api/auth/login", nil)
	reqLoopback.RemoteAddr = "127.0.0.1:12345"
	reqLoopback.Header.Set("X-Forwarded-For", "198.51.100.99, 203.0.113.42")
	reqLoopback.Header.Set("X-Forwarded-Proto", "https")
	if sessionCookie(reqLoopback, "tok", time.Now()).Secure {
		t.Fatal("X-Forwarded-Proto trusted when LOOM_TRUST_PROXY is unset")
	}
	if got := clientRateLimitKey(reqLoopback); got != "127.0.0.1" {
		t.Fatalf("X-Forwarded-For trusted when LOOM_TRUST_PROXY is unset: %q", got)
	}

	t.Setenv("LOOM_TRUST_PROXY", "1")
	if !sessionCookie(reqLoopback, "tok", time.Now()).Secure {
		t.Fatal("LOOM_TRUST_PROXY=1 ignored X-Forwarded-Proto: https from loopback")
	}
	// Rightmost valid IP is chosen so client-supplied left-side spoofed IPs cannot bypass limits.
	if got := clientRateLimitKey(reqLoopback); got != "203.0.113.42" {
		t.Fatalf("expected rightmost XFF IP 203.0.113.42, got %q", got)
	}
	// Non-loopback direct peer must NEVER be trusted even with LOOM_TRUST_PROXY=1.
	reqRemote := httptest.NewRequest("POST", "http://loom.example/api/auth/login", nil)
	reqRemote.RemoteAddr = "198.51.100.5:12345"
	reqRemote.Header.Set("X-Forwarded-For", "10.0.0.9")
	reqRemote.Header.Set("X-Forwarded-Proto", "https")
	if sessionCookie(reqRemote, "tok", time.Now()).Secure {
		t.Fatal("X-Forwarded-Proto trusted from non-loopback peer")
	}
	if got := clientRateLimitKey(reqRemote); got != "198.51.100.5" {
		t.Fatalf("X-Forwarded-For trusted from non-loopback peer: %q", got)
	}

	// 3. handleWebAuthStatus with no password/key only reports authenticated=true on unprotectedLocalRequest.
	mux := http.NewServeMux()
	registerWebLogin(mux)
	checkAuthStatus := func(host, remote string) bool {
		r := httptest.NewRequest("GET", "http://"+host+"/api/auth/status", nil)
		r.RemoteAddr = remote
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		var out struct {
			Authenticated bool `json:"authenticated"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return out.Authenticated
	}
	if !checkAuthStatus("127.0.0.1:2510", "127.0.0.1:43210") {
		t.Fatal("loopback request should report authenticated=true when no auth configured")
	}
	if checkAuthStatus("127.0.0.1:2510", "198.51.100.5:43210") {
		t.Fatal("remote peer reported authenticated=true with no auth configured")
	}
	if checkAuthStatus("rebind.example:2510", "127.0.0.1:43210") {
		t.Fatal("DNS rebinding host reported authenticated=true with no auth configured")
	}

	// 4. Bearer failure rate limiting on requireWebAuth.
	web.ResetBearerLimiter()
	if err := storeWebKey("valid-control-key-for-rate-limit-test"); err != nil {
		t.Fatal(err)
	}
	prot := requireWebAuth(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })
	for i := 0; i < 20; i++ {
		r := httptest.NewRequest("GET", "http://loom.example/api/status", nil)
		r.RemoteAddr = "203.0.113.77:9000"
		r.Header.Set("Authorization", "Bearer invalid-guess")
		w := httptest.NewRecorder()
		prot(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: got %d want 401", i, w.Code)
		}
	}
	r := httptest.NewRequest("GET", "http://loom.example/api/status", nil)
	r.RemoteAddr = "203.0.113.77:9001"
	r.Header.Set("Authorization", "Bearer valid-control-key-for-rate-limit-test")
	w := httptest.NewRecorder()
	prot(w, r)
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") == "" {
		t.Fatalf("expected 429 on 21st Bearer attempt, got %d %v", w.Code, w.Header())
	}

	// 5. publicIP blocks all RFC 6890 non-global prefixes and IPv4-mapped loopback.
	for _, blocked := range []string{
		"0.0.0.1", "10.0.0.1", "100.64.0.1", "127.0.0.1", "169.254.169.254",
		"172.16.0.1", "192.0.0.1", "192.0.2.1", "192.168.1.1", "198.18.0.1",
		"198.51.100.1", "203.0.113.1", "224.0.0.1", "240.0.0.1",
		"::1", "::", "fe80::1", "fc00::1", "64:ff9b::1", "100::1", "2001:db8::1", "::ffff:127.0.0.1",
	} {
		if publicIP(net.ParseIP(blocked)) {
			t.Fatalf("non-global IP %s allowed by publicIP", blocked)
		}
	}
	for _, allowed := range []string{"1.1.1.1", "8.8.8.8", "2606:4700:4700::1111"} {
		if !publicIP(net.ParseIP(allowed)) {
			t.Fatalf("public IP %s rejected by publicIP", allowed)
		}
	}

	// 6. goHTTPClient disables proxy env vars to prevent SSRF bypass.
	tr, ok := goHTTPClient.Transport.(*http.Transport)
	if !ok || tr.Proxy != nil {
		t.Fatal("goHTTPClient.Transport must have Proxy: nil")
	}

	// 7. handleFilesRaw uses RFC-compliant mime.FormatMediaType for Content-Disposition.
	dir := secondBrainFixture(t)
	special := `weird; "name".txt`
	if err := os.WriteFile(filepath.Join(dir, special), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	handleFilesRaw(w, httptest.NewRequest("GET", "/api/files/raw?path="+url.QueryEscape(special), nil))
	cd := w.Header().Get("Content-Disposition")
	if w.Code != 200 || !strings.HasPrefix(cd, "attachment;") || !strings.Contains(cd, `\"name\"`) {
		t.Fatalf("unexpected Content-Disposition: code=%d header=%q", w.Code, cd)
	}
}
