package loom

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
)

const (
	envServicesState  = "env_services"
	envProvidersState = "env_providers"
	envConcurrency    = 8
	envProxmoxKey     = "environment:proxmox"
)

type EnvService struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	URL     string `json:"url"`
	Machine string `json:"machine"`
	Kind    string `json:"kind"`
	Notes   string `json:"notes"`
}

// Secrets are deliberately absent from every persistent/response type.
type EnvProxmoxConfig struct {
	Enabled     bool   `json:"enabled"`
	URL         string `json:"url"`
	TokenID     string `json:"token_id"`
	Fingerprint string `json:"fingerprint"`
}

type envProviders struct {
	Docker  map[string]bool  `json:"docker"`
	Proxmox EnvProxmoxConfig `json:"proxmox"`
}

// Each web router owns its environment service, including injectable I/O and
// a shared check limit (also bounds simultaneous matrix requests).
type environment struct {
	mu        sync.Mutex
	run       func(context.Context, string, []string, string) ([]byte, error)
	sshKey    func() (string, string, error)
	machines  func() []RemoteMachine
	transport http.RoundTripper
	dial      func(context.Context, string, string) (net.Conn, error)
	setSecret func(string, string) error
	getSecret func(string) (string, error)
	checks    chan struct{}
}

func newEnvironment() *environment {
	return &environment{
		run: envExec, sshKey: loomSSHKey, machines: loadRemoteMachines,
		transport: http.DefaultTransport, dial: (&net.Dialer{}).DialContext,
		setSecret: keyringSet, getSecret: keyringGet, checks: make(chan struct{}, envConcurrency),
	}
}

// Unlike getStoreJSON's convenience fallback, a failed/locked read must not
// become an empty list which a subsequent mutation overwrites.
func envRead(key string, dst any) error {
	raw, err := getBytesErr(bkState, key)
	if err != nil || len(raw) == 0 {
		return err
	}
	plain, err := decodeMemContent(raw)
	if err != nil {
		return err
	}
	return json.Unmarshal(plain, dst)
}

func envServices() ([]EnvService, error) {
	list := []EnvService{}
	err := envRead(envServicesState, &list)
	return list, err
}

func envProviderConfig() (envProviders, error) {
	c := envProviders{Docker: map[string]bool{}}
	err := envRead(envProvidersState, &c)
	if c.Docker == nil {
		c.Docker = map[string]bool{}
	}
	return c, err
}

func (e *environment) machine(id string) (*RemoteMachine, error) {
	if id == "" || id == "local" {
		return nil, nil
	}
	for _, m := range e.machines() {
		if m.ID == id {
			return &m, nil
		}
	}
	return nil, errors.New("unknown machine")
}

type envTarget struct{ url, host, port string }

func parseEnvTarget(target string) (envTarget, error) {
	bad := errors.New("target must be an HTTP(S) URL without credentials or a host:port")
	if target == "" || len(target) > 4096 || strings.ContainsAny(target, "\r\n\x00") {
		return envTarget{}, bad
	}
	if strings.Contains(target, "://") {
		u, err := url.Parse(target)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Hostname() == "" || u.Fragment != "" {
			return envTarget{}, bad
		}
		port := u.Port()
		if port == "" {
			port = "80"
			if u.Scheme == "https" {
				port = "443"
			}
		}
		if !envValidPort(port) {
			return envTarget{}, bad
		}
		return envTarget{url: u.String(), host: u.Hostname(), port: port}, nil
	}
	host, port, err := net.SplitHostPort(target)
	if err != nil || strings.TrimSpace(host) != host || host == "" || strings.ContainsAny(host, "/\\ \t'\"") || !envValidPort(port) {
		return envTarget{}, bad
	}
	return envTarget{host: host, port: port}, nil
}

func envValidPort(p string) bool { n, err := strconv.Atoi(p); return err == nil && n > 0 && n <= 65535 }

func (e *environment) validateService(s EnvService) (EnvService, error) {
	s.Name, s.URL, s.Machine = strings.TrimSpace(s.Name), strings.TrimSpace(s.URL), strings.TrimSpace(s.Machine)
	if s.Name == "" || len(s.Name) > 200 || len(s.Notes) > 8192 || len(s.ID) > 128 {
		return s, errors.New("invalid service name, ID or notes")
	}
	if s.Machine == "" {
		s.Machine = "local"
	}
	if _, err := e.machine(s.Machine); err != nil {
		return s, err
	}
	if _, err := parseEnvTarget(s.URL); err != nil {
		return s, err
	}
	switch s.Kind {
	case "web", "api", "db", "other":
	default:
		return s, errors.New("kind must be web, api, db or other")
	}
	if s.ID == "" {
		var id [12]byte
		if _, err := rand.Read(id[:]); err != nil {
			return s, err
		}
		s.ID = hex.EncodeToString(id[:])
	}
	return s, nil
}

func envFailure(w http.ResponseWriter, code int, err error) {
	sendJSON(w, code, map[string]any{"ok": false, "error": err.Error()})
}

func (e *environment) services(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodGet {
		list, err := envServices()
		if err != nil {
			envFailure(w, 503, errors.New("environment store unavailable"))
			return
		}
		sendJSON(w, 200, map[string]any{"ok": true, "services": list})
		return
	}
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var s EnvService
	if !workspaceDecode(w, r, &s) {
		return
	}
	s, err := e.validateService(s)
	if err != nil {
		envFailure(w, 400, err)
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	list, err := envServices()
	if err != nil {
		envFailure(w, 503, errors.New("environment store unavailable"))
		return
	}
	found := false
	for i := range list {
		if list[i].ID == s.ID {
			list[i], found = s, true
		}
	}
	if !found {
		if len(list) >= 256 {
			envFailure(w, 400, errors.New("maximum 256 services"))
			return
		}
		list = append(list, s)
	}
	if err := putStoreJSON(bkState, envServicesState, list); err != nil {
		envFailure(w, 503, errors.New("environment store unavailable"))
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "service": s})
}

func (e *environment) deleteService(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	list, err := envServices()
	if err != nil {
		envFailure(w, 503, errors.New("environment store unavailable"))
		return
	}
	kept := []EnvService{}
	for _, s := range list {
		if s.ID != req.ID {
			kept = append(kept, s)
		}
	}
	if len(kept) == len(list) {
		envFailure(w, 404, errors.New("unknown service"))
		return
	}
	if err := putStoreJSON(bkState, envServicesState, kept); err != nil {
		envFailure(w, 503, errors.New("environment store unavailable"))
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true})
}

func (e *environment) register(api func(string, http.HandlerFunc)) {
	api("/api/env/services", e.services)
	api("/api/env/services/delete", e.deleteService)
	api("/api/env/docker", e.docker)
	api("/api/env/proxmox", e.proxmox)
	api("/api/env/proxmox/resources", e.proxmoxResources)
	api("/api/env/proxmox/fingerprint", handleProxmoxFingerprint)
	api("/api/env/check", e.checkHandler)
	api("/api/env/matrix", e.matrixHandler)
}
