package loom

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/policy"
)

var ollamaModelName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*(/[A-Za-z0-9][A-Za-z0-9._-]*){0,2}(:[A-Za-z0-9][A-Za-z0-9._-]*)?$`)

func handleOllamaPull(w http.ResponseWriter, r *http.Request)   { handleOllamaAction(w, r, true) }
func handleOllamaUnload(w http.ResponseWriter, r *http.Request) { handleOllamaAction(w, r, false) }

func handleOllamaAction(w http.ResponseWriter, r *http.Request, pull bool) {
	w.Header().Set("Cache-Control", "no-store")
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	fail := func(code int, message string) { sendJSON(w, code, map[string]any{"ok": false, "error": message}) }
	var input struct {
		Model string          `json:"model"`
		URL   json.RawMessage `json:"url"`
	}
	if !workspaceDecode(w, r, &input) {
		return
	}
	if len(input.URL) != 0 || len(input.Model) > 200 || !ollamaModelName.MatchString(input.Model) || pull && !isPublicOllamaModel(input.Model) {
		fail(400, "invalid Ollama model or target")
		return
	}
	n := currentEngineNode()
	if n == nil || !n.Direct || n.Kind != "ollama" {
		fail(409, "activate a saved Ollama connection first")
		return
	}
	timeout := 15 * time.Second
	if pull {
		timeout = 60 * time.Minute
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	if pull {
		if err := workspaceSessions.authorizePolicy(ctx, policy.Input{Subject: "node.install", MachineID: "local", Fallback: policy.Allow}, false); err != nil {
			fail(403, err.Error())
			return
		}
	}
	if currentEngineNode() != n {
		fail(409, "active engine changed")
		return
	}
	endpoint := "/api/generate"
	payload := map[string]any{"model": input.Model, "keep_alive": 0, "stream": false}
	if pull {
		endpoint = "/api/pull"
		payload = map[string]any{"model": input.Model, "stream": true}
	}
	// Old direct links are also persisted in engine_node. Revalidate the saved
	// origin, resolve its IP and integer port, and use fixed native paths.
	// nodeClient follows no redirects.
	base, _, err := cleanDirectURL(n.V1)
	if err != nil {
		fail(409, "invalid saved Ollama connection")
		return
	}
	parsed, err := url.Parse(base)
	if err != nil {
		fail(409, "invalid saved Ollama connection")
		return
	}
	targetURL, sniHost, err := resolvedOllamaEndpoint(ctx, parsed, endpoint)
	if err != nil {
		fail(409, "invalid saved Ollama connection")
		return
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		fail(409, "invalid saved Ollama connection")
		return
	}
	if sniHost != "" {
		req.Host = sniHost
	}
	req.Header.Set("Content-Type", "application/json")
	if n.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+n.APIKey)
	}
	// Pull streams progress to avoid a response-header timeout during a long
	// download. The browser awaits completion and receives only the result.
	client := *nodeClient
	if sniHost != "" && parsed.Scheme == "https" {
		if baseTr, ok := nodeClient.Transport.(*http.Transport); ok && baseTr != nil {
			tr := baseTr.Clone()
			if tr.TLSClientConfig == nil {
				tr.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
			} else {
				tr.TLSClientConfig = tr.TLSClientConfig.Clone()
			}
			tr.TLSClientConfig.ServerName = sniHost
			client.Transport = tr
		}
	}
	if pull {
		client.Timeout = timeout
	}
	resp, err := client.Do(req)
	if err != nil {
		fail(502, "Ollama request failed")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fail(502, fmt.Sprintf("Ollama returned HTTP %d", resp.StatusCode))
		return
	}
	var result struct{ Status, Error string }
	dec := json.NewDecoder(io.LimitReader(resp.Body, 32<<20))
	for {
		result = struct{ Status, Error string }{}
		err := dec.Decode(&result)
		if err != nil || result.Error != "" {
			fail(502, "Ollama did not complete the request")
			return
		}
		if !pull || result.Status == "success" {
			break
		}
	}
	sendJSON(w, 200, map[string]any{"ok": true})
}

// An Ollama name may include a registry. Native pulls without an explicit URL
// still must not be allowed to redirect Ollama to an arbitrary host.
func isPublicOllamaModel(model string) bool {
	first, _, hasNamespace := strings.Cut(model, "/")
	if !hasNamespace || strings.Count(model, "/") == 1 && !strings.Contains(first, ".") {
		return true
	}
	return first == "registry.ollama.ai"
}

// resolvedOllamaEndpoint reconstructs a fixed-path request URL from the saved
// Ollama origin's validated scheme, resolved IP, and numeric port.
func resolvedOllamaEndpoint(ctx context.Context, parsed *url.URL, endpoint string) (string, string, error) {
	if parsed == nil {
		return "", "", fmt.Errorf("missing Ollama URL")
	}
	var scheme string
	var defaultPort int
	switch strings.ToLower(parsed.Scheme) {
	case "http":
		scheme, defaultPort = "http", 80
	case "https":
		scheme, defaultPort = "https", 443
	default:
		return "", "", fmt.Errorf("unsupported scheme")
	}
	portNum := defaultPort
	if p := parsed.Port(); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return "", "", fmt.Errorf("invalid port")
		}
		portNum = n
	}
	host := parsed.Hostname()
	if host == "" {
		return "", "", fmt.Errorf("missing host")
	}
	var resolvedIP net.IP
	sniHost := ""
	if ip := net.ParseIP(host); ip != nil {
		resolvedIP = ip
	} else {
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil || len(ips) == 0 {
			return "", "", fmt.Errorf("cannot resolve Ollama host")
		}
		resolvedIP = ips[0].IP
		sniHost = host
	}
	if !localNetworkHost(resolvedIP.String()) {
		return "", "", fmt.Errorf("Ollama host must be on the local network")
	}
	var fixedPath string
	switch endpoint {
	case "/api/pull":
		fixedPath = "/api/pull"
	case "/api/generate":
		fixedPath = "/api/generate"
	default:
		return "", "", fmt.Errorf("unsupported Ollama endpoint")
	}
	u := url.URL{
		Scheme: scheme,
		Host:   net.JoinHostPort(resolvedIP.String(), strconv.Itoa(portNum)),
		Path:   fixedPath,
	}
	return u.String(), sniHost, nil
}
