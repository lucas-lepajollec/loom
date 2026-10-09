package loom

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/doctor"
)

const httpsSecretID = "web-https"

type httpsPaths struct {
	Cert string `json:"cert"`
	Key  string `json:"key"`
}
type httpsConfig struct {
	Enabled   bool        `json:"enabled"`
	Port      int         `json:"port"`
	Mode      string      `json:"mode"`
	CertPaths *httpsPaths `json:"cert_paths,omitempty"`
}

func readHTTPSConfig() httpsConfig {
	cfg := ReadConfig()
	c := httpsConfig{Enabled: cfg["https.enabled"] == "true", Port: 2543, Mode: "self-signed"}
	if port, err := strconv.Atoi(cfg["https.port"]); err == nil {
		c.Port = port
	}
	if mode := cfg["https.mode"]; mode != "" {
		c.Mode = mode
	}
	if c.Mode == "files" {
		c.CertPaths = &httpsPaths{Cert: cfg["https.cert_path"], Key: cfg["https.key_path"]}
	}
	return c
}
func saveHTTPSConfig(c httpsConfig) error {
	values := map[string]string{"https.enabled": strconv.FormatBool(c.Enabled), "https.port": strconv.Itoa(c.Port), "https.mode": c.Mode, "https.cert_path": "", "https.key_path": ""}
	if c.CertPaths != nil {
		values["https.cert_path"], values["https.key_path"] = c.CertPaths.Cert, c.CertPaths.Key
	}
	return setConfigKeys(values)
}

func httpsNames() ([]string, []net.IP) {
	names := map[string]bool{"localhost": true}
	if hostname, err := os.Hostname(); err == nil && hostname != "" {
		names[hostname] = true
	}
	host := webHost()
	if host != "" && net.ParseIP(host) == nil && host != "0.0.0.0" {
		names[host] = true
	}
	ips := map[string]net.IP{"127.0.0.1": net.ParseIP("127.0.0.1"), "::1": net.ParseIP("::1")}
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, addr := range addrs {
			ip, _, err := net.ParseCIDR(addr.String())
			if err == nil && ip.IsPrivate() {
				ips[ip.String()] = ip
			}
		}
	}
	dns := []string{}
	for name := range names {
		dns = append(dns, name)
	}
	sort.Strings(dns)
	keys := []string{}
	for key := range ips {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	addresses := []net.IP{}
	for _, key := range keys {
		addresses = append(addresses, ips[key])
	}
	return dns, addresses
}
func generateHTTPSCertificate(dns []string, ips []net.IP, now time.Time) (string, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return "", err
	}
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "Loom"}, NotBefore: now.Add(-5 * time.Minute), NotAfter: now.Add(825 * 24 * time.Hour), DNSNames: dns, IPAddresses: ips, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return "", err
	}
	private, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})) + string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private})), nil
}
func httpsCertificate(c httpsConfig, create, regenerate bool) (tls.Certificate, error) {
	if c.Mode == "files" {
		if c.CertPaths == nil {
			return tls.Certificate{}, errors.New("certificate and key paths required")
		}
		read := func(path string) ([]byte, error) {
			info, err := os.Stat(path)
			if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
				return nil, errors.New("certificate file unavailable or too large")
			}
			return os.ReadFile(path)
		}
		cert, err := read(c.CertPaths.Cert)
		if err != nil {
			return tls.Certificate{}, err
		}
		key, err := read(c.CertPaths.Key)
		if err != nil {
			return tls.Certificate{}, err
		}
		return tls.X509KeyPair(cert, key)
	}
	if c.Mode != "self-signed" {
		return tls.Certificate{}, errors.New("mode must be self-signed or files")
	}
	path := filepath.Join(providerSecretRoot(), httpsSecretID+".sealed")
	_, statErr := os.Lstat(path)
	if (create && os.IsNotExist(statErr)) || regenerate {
		dns, ips := httpsNames()
		pair, err := generateHTTPSCertificate(dns, ips, time.Now())
		if err != nil {
			return tls.Certificate{}, err
		}
		if err := sealProviderSecret(httpsSecretID, pair); err != nil {
			return tls.Certificate{}, err
		}
	}
	pair, err := readProviderSecret(httpsSecretID)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.X509KeyPair([]byte(pair), []byte(pair))
}
func httpsFingerprint(cert tls.Certificate) string {
	if len(cert.Certificate) == 0 {
		return ""
	}
	sum := sha256.Sum256(cert.Certificate[0])
	return hex.EncodeToString(sum[:])
}
func httpsURLs(host string, port int) []string {
	hosts := []string{host}
	if host == "" || host == "0.0.0.0" || host == "::" {
		dns, ips := httpsNames()
		hosts = dns
		for _, ip := range ips {
			if host != "0.0.0.0" || ip.To4() != nil {
				hosts = append(hosts, ip.String())
			}
		}
	}
	urls := []string{}
	for _, h := range hosts {
		urls = append(urls, "https://"+net.JoinHostPort(h, strconv.Itoa(port)))
	}
	sort.Strings(urls)
	return urls
}

type httpsListener struct {
	mu          sync.Mutex
	handler     http.Handler
	host        string
	server      *http.Server
	listener    net.Listener
	listen      func(string, string) (net.Listener, error)
	certificate tls.Certificate
	config      httpsConfig
	fingerprint string
	lastError   string
}

var webHTTPS = &httpsListener{}

// The active certificate is swapped without dropping this request's TLS
// connection. A new port is bound before replacing the old listener.
func (m *httpsListener) applyLocked(c httpsConfig, cert tls.Certificate, persist func() error) error {
	commit := func() error {
		if persist != nil {
			return persist()
		}
		return nil
	}
	if !c.Enabled {
		if err := commit(); err != nil {
			return err
		}
		if m.server != nil {
			drainHTTPSServer(m.server)
		}
		m.server = nil
		m.listener = nil
		m.config = c
		m.certificate = tls.Certificate{}
		m.fingerprint = ""
		m.lastError = ""
		return nil
	}
	if err := webListenCheck(m.host); err != nil {
		return err
	}
	if m.handler == nil {
		return errors.New("HTTPS listener is unavailable in this serving mode")
	}
	if m.server != nil && m.config.Port == c.Port {
		if err := commit(); err != nil {
			return err
		}
		m.config = c
		m.fingerprint = httpsFingerprint(cert)
		m.lastError = ""
		m.certificate = cert
		// GetCertificate below reads the current certificate under the same mutex.
		return nil
	}
	listen := m.listen
	if listen == nil {
		listen = net.Listen
	}
	ln, err := listen("tcp", net.JoinHostPort(m.host, strconv.Itoa(c.Port)))
	if err != nil {
		return err
	}
	if err := commit(); err != nil {
		_ = ln.Close()
		return err
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.server == nil {
			return nil, errors.New("HTTPS stopped")
		}
		cert := m.certificate
		return &cert, nil
	}}
	srv := &http.Server{Handler: m.handler, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 90 * time.Second, TLSConfig: tlsConfig}
	old := m.server
	m.server = srv
	m.listener = ln
	m.config = c
	m.certificate = cert
	m.fingerprint = httpsFingerprint(cert)
	m.lastError = ""
	go func() {
		err := srv.Serve(tls.NewListener(ln, tlsConfig))
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.server == srv && err != nil && !errors.Is(err, http.ErrServerClosed) {
			m.lastError = "HTTPS listener stopped"
			m.server = nil
			m.listener = nil
		}
	}()
	// Close asynchronously: a POST arriving through the old listener can return
	// its result before its connection is drained.
	if old != nil {
		drainHTTPSServer(old)
	}
	return nil
}
func (m *httpsListener) start(ctx context.Context, handler http.Handler, host string) error {
	m.mu.Lock()
	m.handler = handler
	m.host = host
	c := readHTTPSConfig()
	var cert tls.Certificate
	var err error
	if c.Enabled {
		cert, err = httpsCertificate(c, true, false)
	}
	if err == nil {
		err = m.applyLocked(c, cert, nil)
	}
	if err != nil {
		m.lastError = err.Error()
	}
	m.mu.Unlock()
	go func() {
		<-ctx.Done()
		m.mu.Lock()
		if m.server != nil {
			_ = m.server.Close()
		}
		m.server = nil
		m.listener = nil
		m.handler = nil
		m.mu.Unlock()
	}()
	return err
}
func (m *httpsListener) status(c httpsConfig) map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	fingerprint := m.fingerprint
	if fingerprint == "" {
		if cert, err := httpsCertificate(c, false, false); err == nil {
			fingerprint = httpsFingerprint(cert)
		}
	}
	host := m.host
	if host == "" {
		host = webHost()
	}
	out := map[string]any{"ok": true, "enabled": c.Enabled, "port": c.Port, "mode": c.Mode, "urls": httpsURLs(host, c.Port), "fingerprint_sha256": fingerprint, "running": m.server != nil}
	if c.CertPaths != nil {
		out["cert_paths"] = c.CertPaths
	}
	if m.lastError != "" {
		out["error"] = m.lastError
	}
	return out
}
func handleHTTPS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !usageVaultAccess(w) {
		return
	}
	if r.Method == http.MethodGet {
		sendJSON(w, 200, webHTTPS.status(readHTTPSConfig()))
		return
	}
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		httpsConfig
		Regenerate bool `json:"regenerate"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	c := req.httpsConfig
	if c.Port == 0 {
		c.Port = 2543
	}
	if c.Mode == "" {
		c.Mode = "self-signed"
	}
	if c.Mode == "self-signed" {
		c.CertPaths = nil
	}
	if c.Port < 1 || c.Port > 65535 || (c.Mode != "files" && c.Mode != "self-signed") || (req.Regenerate && c.Mode != "self-signed") {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "valid port and certificate mode required"})
		return
	}
	if c.Mode == "files" && (c.CertPaths == nil || !filepath.IsAbs(c.CertPaths.Cert) || !filepath.IsAbs(c.CertPaths.Key)) {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "absolute certificate and key paths required"})
		return
	}
	webHTTPS.mu.Lock()
	var cert tls.Certificate
	var err error
	if c.Enabled || req.Regenerate {
		cert, err = httpsCertificate(c, true, req.Regenerate)
	}
	if err == nil {
		err = webHTTPS.applyLocked(c, cert, func() error { return saveHTTPSConfig(c) })
	}
	webHTTPS.mu.Unlock()
	if err != nil {
		sendJSON(w, 409, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, webHTTPS.status(c))
}
func doctorHTTPS(context.Context) doctor.Check {
	webHTTPS.mu.Lock()
	running := webHTTPS.server != nil
	webHTTPS.mu.Unlock()
	if running || (webBound.host != "" && isLoopbackHost(webBound.host)) {
		return doctorCheck("ok", "secure_origin_available", "")
	}
	installed := voiceEngineInstalled() || voiceSelectedMachine() != "local"
	if !installed {
		return doctorCheck("skip", "voice_not_installed", "")
	}
	return doctorCheck("warn", "voice_requires_secure_origin", "#/settings/security")
}

func drainHTTPSServer(server *http.Server) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
		_ = server.Close()
	}()
}
