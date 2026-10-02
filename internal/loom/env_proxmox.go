package loom

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type EnvProxmoxResource struct {
	VMID   *int     `json:"vmid"`
	Name   string   `json:"name"`
	Node   string   `json:"node"`
	Type   string   `json:"type"`
	Status string   `json:"status"`
	CPU    *float64 `json:"cpu"`
	MaxMem *int64   `json:"maxmem"`
	Mem    *int64   `json:"mem"`
	Uptime *int64   `json:"uptime"`
}

func parseEnvProxmox(data []byte) ([]EnvProxmoxResource, error) {
	var body struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(data, &body); err != nil || len(body.Data) == 0 || body.Data[0] != '[' {
		return nil, errors.New("invalid Proxmox resources response")
	}
	var rows []EnvProxmoxResource
	if err := json.Unmarshal(body.Data, &rows); err != nil {
		return nil, errors.New("invalid Proxmox resources response")
	}
	resources := []EnvProxmoxResource{}
	for _, row := range rows {
		switch row.Type {
		case "node", "qemu", "lxc":
		default:
			continue
		}
		if row.Type == "node" && row.Name == "" {
			row.Name = row.Node
		}
		resources = append(resources, row)
	}
	return resources, nil
}

func envFingerprint(s string) (string, error) {
	s = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(s), ":", ""))
	if s == "" {
		return s, nil
	}
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != sha256.Size {
		return "", errors.New("fingerprint must be a SHA-256 certificate fingerprint")
	}
	return s, nil
}

func envProxmoxTLS(pin string) (*tls.Config, error) {
	pin, err := envFingerprint(pin)
	if err != nil {
		return nil, err
	}
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if pin == "" {
		return cfg, nil
	} // normal OS certificate/hostname verification
	want, _ := hex.DecodeString(pin)
	// Custom verification is enabled ONLY with a saved, explicitly confirmed
	// certificate pin. VerifyConnection runs on every handshake, even resumptions.
	cfg.InsecureSkipVerify = true
	cfg.VerifyConnection = func(cs tls.ConnectionState) error {
		if len(cs.PeerCertificates) == 0 {
			return errors.New("Proxmox certificate missing")
		}
		cert := cs.PeerCertificates[0]
		got := sha256.Sum256(cert.Raw)
		if subtle.ConstantTimeCompare(got[:], want) != 1 {
			return errors.New("Proxmox certificate fingerprint mismatch")
		}
		now := time.Now()
		if now.Before(cert.NotBefore) || now.After(cert.NotAfter) {
			return errors.New("Proxmox certificate outside validity period")
		}
		return nil
	}
	return cfg, nil
}

func (e *environment) proxmox(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodGet {
		cfg, err := envProviderConfig()
		if err != nil {
			envFailure(w, 503, errors.New("environment store unavailable"))
			return
		}
		sendJSON(w, 200, map[string]any{"ok": true, "config": cfg.Proxmox})
		return
	}
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		EnvProxmoxConfig
		TokenSecret        string `json:"token_secret"`
		ConfirmFingerprint bool   `json:"confirm_fingerprint"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	c := req.EnvProxmoxConfig
	c.URL, c.TokenID = strings.TrimSpace(c.URL), strings.TrimSpace(c.TokenID)
	if c.URL != "" || c.Enabled {
		u, err := url.Parse(c.URL)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			envFailure(w, 400, errors.New("Proxmox requires an HTTPS origin without credentials, query or path"))
			return
		}
		if _, err := parseEnvTarget(c.URL); err != nil {
			envFailure(w, 400, err)
			return
		}
		c.URL = strings.TrimRight(c.URL, "/")
	}
	if c.TokenID != "" || c.Enabled {
		if !strings.Contains(c.TokenID, "@") || !strings.Contains(c.TokenID, "!") || strings.ContainsAny(c.TokenID, "= \t\r\n\x00") || len(c.TokenID) > 512 {
			envFailure(w, 400, errors.New("invalid Proxmox token_id (user@realm!token)"))
			return
		}
	}
	if strings.ContainsAny(req.TokenSecret, "\r\n\x00") || len(req.TokenSecret) > 8192 {
		envFailure(w, 400, errors.New("invalid token secret"))
		return
	}
	var err error
	c.Fingerprint, err = envFingerprint(c.Fingerprint)
	if err != nil {
		envFailure(w, 400, err)
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	stored, err := envProviderConfig()
	if err != nil {
		envFailure(w, 503, errors.New("environment store unavailable"))
		return
	}
	if c.Fingerprint != "" && c.Fingerprint != stored.Proxmox.Fingerprint && !req.ConfirmFingerprint {
		envFailure(w, 400, errors.New("confirm_fingerprint is required for a new certificate pin"))
		return
	}
	if req.TokenSecret != "" {
		if err := e.setSecret(envProxmoxKey, req.TokenSecret); err != nil {
			envFailure(w, 503, errors.New("OS keychain unavailable; token was not saved"))
			return
		}
	}
	stored.Proxmox = c
	if err := putStoreJSON(bkState, envProvidersState, stored); err != nil {
		envFailure(w, 503, errors.New("environment store unavailable"))
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "config": c})
}

func (e *environment) proxmoxResources(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodGet) {
		return
	}
	cfg, err := envProviderConfig()
	if err != nil {
		envFailure(w, 503, errors.New("environment store unavailable"))
		return
	}
	if !cfg.Proxmox.Enabled {
		sendJSON(w, 200, map[string]any{"ok": true, "enabled": false, "resources": []EnvProxmoxResource{}})
		return
	}
	// Hold the configuration/key read together with token updates, then release
	// before any network I/O.
	e.mu.Lock()
	cfg, err = envProviderConfig()
	secret, keyErr := e.getSecret(envProxmoxKey)
	e.mu.Unlock()
	if err != nil {
		envFailure(w, 503, errors.New("environment store unavailable"))
		return
	}
	if !cfg.Proxmox.Enabled {
		sendJSON(w, 200, map[string]any{"ok": true, "enabled": false, "resources": []EnvProxmoxResource{}})
		return
	}
	if keyErr != nil || secret == "" || strings.ContainsAny(secret, "\r\n\x00") {
		envFailure(w, 503, errors.New("Proxmox token unavailable in OS keychain"))
		return
	}
	resources, err := fetchEnvProxmox(r.Context(), cfg.Proxmox, secret, e.dial)
	if err != nil {
		envFailure(w, 502, err)
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "enabled": true, "resources": resources})
}

func fetchEnvProxmox(ctx context.Context, c EnvProxmoxConfig, secret string, dial func(context.Context, string, string) (net.Conn, error)) ([]EnvProxmoxResource, error) {
	tlsConfig, err := envProxmoxTLS(c.Fingerprint)
	if err != nil {
		return nil, err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = tlsConfig
	transport.DialContext = dial
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL+"/api2/json/cluster/resources", nil)
	if err != nil {
		return nil, errors.New("invalid Proxmox URL")
	}
	req.Header.Set("Authorization", "PVEAPIToken="+c.TokenID+"="+secret)
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("Proxmox connection failed (check reachability and certificate trust/pin)")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("Proxmox resource request failed: " + http.StatusText(resp.StatusCode))
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (4<<20)+1))
	if err != nil || len(data) > 4<<20 {
		return nil, errors.New("Proxmox resource response unreadable or exceeds 4 MiB")
	}
	return parseEnvProxmox(data)
}
