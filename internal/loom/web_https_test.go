package loom

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestHTTPSCertificateSANsPersistenceAndRegeneration(t *testing.T) {
	home := testHome(t)
	c := httpsConfig{Mode: "self-signed", Port: 2543}
	cert, err := httpsCertificate(c, true, false)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	dns, ips := httpsNames()
	for _, name := range dns {
		if err := parsed.VerifyHostname(name); err != nil {
			t.Fatal(name, err)
		}
	}
	for _, ip := range ips {
		if err := parsed.VerifyHostname(ip.String()); err != nil {
			t.Fatal(ip, err)
		}
	}
	if len(parsed.DNSNames) != len(dns) || len(parsed.IPAddresses) != len(ips) {
		t.Fatal("SAN mismatch")
	}
	if key, ok := cert.PrivateKey.(*ecdsa.PrivateKey); !ok || key.Curve.Params().Name != "P-256" {
		t.Fatal("not ECDSA P-256")
	}
	if days := time.Until(parsed.NotAfter).Hours() / 24; days < 824 || days > 825 {
		t.Fatal("certificate lifetime", days)
	}
	raw, err := os.ReadFile(filepath.Join(home, "secrets", "providers", httpsSecretID+".sealed"))
	if err != nil || strings.Contains(string(raw), "PRIVATE KEY") {
		t.Fatal("unsealed key", err)
	}
	again, err := httpsCertificate(c, true, false)
	if err != nil || httpsFingerprint(again) != httpsFingerprint(cert) {
		t.Fatal("certificate not persistent", err)
	}
	regenerated, err := httpsCertificate(c, true, true)
	if err != nil || httpsFingerprint(regenerated) == httpsFingerprint(cert) {
		t.Fatal("regeneration failed", err)
	}
	persisted, err := httpsCertificate(c, false, false)
	if err != nil || httpsFingerprint(persisted) != httpsFingerprint(regenerated) {
		t.Fatal("regenerated cert not persisted", err)
	}
	// Corruption must refuse, never silently replace the generated identity.
	path := filepath.Join(providerSecretRoot(), httpsSecretID+".sealed")
	if err := os.WriteFile(path, []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := httpsCertificate(c, true, false); err == nil {
		t.Fatal("corruption silently regenerated")
	}
}
func TestHTTPSCertificateFilesAndURLs(t *testing.T) {
	testHome(t)
	pair, err := generateHTTPSCertificate([]string{"loom.test", "localhost"}, []net.IP{net.ParseIP("192.168.2.3")}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	certBlock, rest := pem.Decode([]byte(pair))
	if certBlock == nil {
		t.Fatal("missing cert")
	}
	certPath, keyPath := filepath.Join(t.TempDir(), "cert.pem"), filepath.Join(t.TempDir(), "key.pem")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(certBlock), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, rest, 0600); err != nil {
		t.Fatal(err)
	}
	c := httpsConfig{Mode: "files", CertPaths: &httpsPaths{Cert: certPath, Key: keyPath}}
	if _, err := httpsCertificate(c, false, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(providerSecretRoot(), httpsSecretID+".sealed")); !os.IsNotExist(err) {
		t.Fatal("files mode generated a key")
	}
	urls := httpsURLs("192.168.2.3", 2543)
	if len(urls) != 1 || urls[0] != "https://192.168.2.3:2543" {
		t.Fatal(urls)
	}
	urls = httpsURLs("::1", 2543)
	if urls[0] != "https://[::1]:2543" {
		t.Fatal(urls)
	}
}
func TestHTTPSListenerStartStopAndLiveRegenerate(t *testing.T) {
	testHome(t)
	manager := &httpsListener{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := httpsConfig{Enabled: true, Mode: "self-signed", Port: 0}
	cert, err := httpsCertificate(c, true, false)
	if err != nil {
		t.Fatal(err)
	}
	manager.mu.Lock()
	manager.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("same handlers")) })
	manager.host = "127.0.0.1"
	err = manager.applyLocked(c, cert, nil)
	if err != nil {
		manager.mu.Unlock()
		t.Fatal("sandbox socket failure: HTTPS listener:", err)
	}
	addr := manager.listener.Addr().String()
	manager.mu.Unlock()
	defer func() {
		manager.mu.Lock()
		if manager.server != nil {
			manager.server.Close()
		}
		manager.mu.Unlock()
	}()
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}, Timeout: time.Second}
	defer client.CloseIdleConnections()
	check := func(want string) {
		t.Helper()
		resp, err := client.Get("https://" + addr)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if string(raw) != "same handlers" || httpsFingerprint(tls.Certificate{Certificate: [][]byte{resp.TLS.PeerCertificates[0].Raw}}) != want {
			t.Fatal("wrong handler or identity")
		}
	}
	check(httpsFingerprint(cert))
	regenerated, err := httpsCertificate(c, true, true)
	if err != nil {
		t.Fatal(err)
	}
	manager.mu.Lock()
	err = manager.applyLocked(c, regenerated, nil)
	manager.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	client.CloseIdleConnections()
	check(httpsFingerprint(regenerated))
	manager.mu.Lock()
	err = manager.applyLocked(httpsConfig{Mode: "self-signed", Port: 2543}, tls.Certificate{}, nil)
	manager.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	// Graceful stop closes the listening socket; wait only for the owned drain.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 20*time.Millisecond)
		if err != nil {
			return
		}
		conn.Close()
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Millisecond):
		}
	}
	t.Fatal("listener still accepting after stop")
}
func TestHTTPSAPIValidationAndInertGET(t *testing.T) {
	testHome(t)
	old := webHTTPS
	webHTTPS = &httpsListener{}
	t.Cleanup(func() { webHTTPS = old })
	w := httptest.NewRecorder()
	handleHTTPS(w, httptest.NewRequest("GET", "/api/https", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"port":2543`) || !strings.Contains(w.Body.String(), `"running":false`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(providerSecretRoot(), httpsSecretID+".sealed")); !os.IsNotExist(err) {
		t.Fatal("GET generated certificate")
	}
	for _, body := range []string{`{"enabled":true,"port":-1}`, `{"enabled":true,"mode":"invalid"}`, `{"enabled":true,"mode":"files","cert_paths":{"cert":"relative","key":"relative"}}`, `{"enabled":true,"mode":"files","regenerate":true}`} {
		r := httptest.NewRequest("POST", "/api/https", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w = httptest.NewRecorder()
		handleHTTPS(w, r)
		if w.Code != 400 {
			t.Fatal(body, w.Code, w.Body.String())
		}
	}
}

// Exercise the real TLS/http server without requiring sandbox socket privileges.
type httpsLoopbackPipe struct{ net.Conn }

func (httpsLoopbackPipe) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345}
}

type httpsPipeListener struct {
	accepted chan net.Conn
	closed   chan struct{}
	once     sync.Once
}

func (l *httpsPipeListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.accepted:
		return conn, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}
func (l *httpsPipeListener) Close() error { l.once.Do(func() { close(l.closed) }); return nil }
func (*httpsPipeListener) Addr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 2543}
}
func TestHTTPSInMemoryTLSLifecycleAndFailedPortChange(t *testing.T) {
	testHome(t)
	c := httpsConfig{Enabled: true, Mode: "self-signed", Port: 2543}
	if err := saveHTTPSConfig(c); err != nil {
		t.Fatal(err)
	}
	manager := &httpsListener{listen: func(_, addr string) (net.Listener, error) {
		if strings.HasSuffix(addr, ":2544") {
			return nil, errors.New("occupied fixture port")
		}
		return &httpsPipeListener{accepted: make(chan net.Conn), closed: make(chan struct{})}, nil
	}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	old := webHTTPS
	webHTTPS = manager
	t.Cleanup(func() { webHTTPS = old })
	mux := http.NewServeMux()
	mux.HandleFunc("/api/https", requireWebAuth(handleHTTPS))
	mux.HandleFunc("/api/ping", requireWebAuth(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("same authenticated handlers")) }))
	if err := manager.start(ctx, mux, "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		manager.mu.Lock()
		listener, ok := manager.listener.(*httpsPipeListener)
		manager.mu.Unlock()
		if !ok {
			return nil, net.ErrClosed
		}
		server, client := net.Pipe()
		select {
		case listener.accepted <- httpsLoopbackPipe{server}:
			return client, nil
		case <-ctx.Done():
			server.Close()
			client.Close()
			return nil, ctx.Err()
		case <-listener.closed:
			server.Close()
			client.Close()
			return nil, net.ErrClosed
		}
	}}
	client := &http.Client{Transport: transport, Timeout: time.Second}
	defer client.CloseIdleConnections()
	response, err := client.Get("https://127.0.0.1:2543/api/ping")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if string(raw) != "same authenticated handlers" {
		t.Fatal(string(raw))
	}
	first := response.TLS.PeerCertificates[0].Raw
	manager.mu.Lock()
	persistErr := errors.New("fixture storage unavailable")
	err = manager.applyLocked(httpsConfig{Mode: "self-signed", Port: 2543}, tls.Certificate{}, func() error { return persistErr })
	retained := manager.server != nil && manager.config.Enabled
	manager.mu.Unlock()
	if !errors.Is(err, persistErr) || !retained {
		t.Fatal("failed persistence changed listener", err)
	}
	post := func(body string) *http.Response {
		t.Helper()
		response, err := client.Post("https://127.0.0.1:2543/api/https", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, response.Body)
		response.Body.Close()
		return response
	}
	response = post(`{"enabled":true,"port":2544,"mode":"self-signed"}`)
	if response.StatusCode != 409 || readHTTPSConfig().Port != 2543 {
		t.Fatal("failed bind replaced config")
	}
	response = post(`{"enabled":true,"port":2543,"mode":"self-signed","regenerate":true}`)
	if response.StatusCode != 200 {
		t.Fatal("regeneration dropped active request", response.StatusCode)
	}
	client.CloseIdleConnections()
	response, err = client.Get("https://127.0.0.1:2543/api/ping")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if bytes.Equal(first, response.TLS.PeerCertificates[0].Raw) {
		t.Fatal("TLS certificate did not change")
	}
	response = post(`{"enabled":false,"port":2543,"mode":"self-signed"}`)
	if response.StatusCode != 200 || readHTTPSConfig().Enabled {
		t.Fatal("disabling dropped active request")
	}
	manager.mu.Lock()
	running := manager.server != nil
	manager.mu.Unlock()
	if running {
		t.Fatal("listener not stopped")
	}
}
func TestHTTPSDoctorWarnsForVoiceWithoutSecureOrigin(t *testing.T) {
	testHome(t)
	oldHTTPS, oldBound := webHTTPS, webBound
	webHTTPS = &httpsListener{}
	webBound.host = "0.0.0.0"
	webBound.port = 2510
	t.Cleanup(func() { webHTTPS = oldHTTPS; webBound = oldBound })
	if err := putStr(bkState, "voice_machine", "fixture-node"); err != nil {
		t.Fatal(err)
	}
	if c := doctorHTTPS(context.Background()); c.Status != "warn" {
		t.Fatal(c)
	}
	webBound.host = "127.0.0.1"
	if c := doctorHTTPS(context.Background()); c.Status != "ok" {
		t.Fatal(c)
	}
	webBound.host = "0.0.0.0"
	if err := putStr(bkState, "voice_machine", "local"); err != nil {
		t.Fatal(err)
	}
	if c := doctorHTTPS(context.Background()); c.Status != "skip" {
		t.Fatal(c)
	}
}
