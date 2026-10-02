package loom

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// POST {url}: the certificate a Proxmox server presents, so the user can
// compare it with the one shown in Proxmox and confirm the pin. Nothing is
// sent to the server besides the TLS handshake, and nothing is trusted here.
func handleProxmoxFingerprint(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		URL string `json:"url"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	u, err := url.Parse(strings.TrimSpace(req.URL))
	if err != nil || u.Scheme != "https" || u.Host == "" {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "https address expected (e.g. https://192.168.1.10:8006)"})
		return
	}
	host := u.Host
	if u.Port() == "" {
		host = net.JoinHostPort(u.Hostname(), "443")
	}
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 5 * time.Second}, "tcp", host, &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12}) // inspection only, never used for requests
	if err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "Proxmox unreachable: " + err.Error()})
		return
	}
	defer conn.Close()
	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		sendJSON(w, 400, map[string]any{"ok": false, "error": errors.New("no certificate presented").Error()})
		return
	}
	c := certs[0]
	sum := sha256.Sum256(c.Raw)
	pairs := []string{}
	for _, b := range sum {
		pairs = append(pairs, strings.ToUpper(hex.EncodeToString([]byte{b})))
	}
	_, verr := c.Verify(x509Opts(u.Hostname()))
	sendJSON(w, 200, map[string]any{"ok": true, "fingerprint": strings.Join(pairs, ":"), "subject": c.Subject.CommonName, "issuer": c.Issuer.CommonName,
		"not_after": c.NotAfter, "trusted": verr == nil})
}

func x509Opts(host string) x509.VerifyOptions { return x509.VerifyOptions{DNSName: host} }
