package loom

import (
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// Only canonical HTTP authorities may enter a CSP source expression. In
// particular, never interpolate the raw Host header or a forwarded host.
var previewDNSName = regexp.MustCompile(`^(?:[a-z0-9-]+\.)*[a-z0-9-]+\.?$`)
var numericHostLabel = regexp.MustCompile(`^(?:[0-9]+|0x[0-9a-f]*)$`)

func previewOrigin(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil ||
		(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return nil, errors.New("invalid preview origin")
	}
	host := strings.ToLower(u.Hostname())
	ip, ipErr := netip.ParseAddr(host)
	labels := strings.Split(strings.TrimSuffix(host, "."), ".")
	if ipErr == nil && ip.Zone() == "" {
		host = ip.String()
	} else if !previewDNSName.MatchString(host) || numericHostLabel.MatchString(labels[len(labels)-1]) {
		return nil, errors.New("invalid preview host")
	}
	port := u.Port()
	if port != "" {
		n, err := strconv.ParseUint(port, 10, 16)
		if err != nil || n == 0 {
			return nil, errors.New("invalid preview port")
		}
		port = strconv.Itoa(int(n))
	}
	u.Host, u.Path = host, ""
	if port != "" && !(u.Scheme == "http" && port == "80") && !(u.Scheme == "https" && port == "443") {
		u.Host = net.JoinHostPort(host, port)
	} else if ip.Is6() {
		u.Host = "[" + host + "]"
	}
	return u, nil
}

func previewParent(r *http.Request) (*url.URL, error) {
	scheme := "http"
	if requestIsHTTPS(r) {
		scheme = "https"
	}
	return previewOrigin(scheme + "://" + r.Host)
}

func previewFrameSource(r *http.Request) string {
	parent, err := previewParent(r)
	if err != nil {
		return ""
	}
	config := os.Getenv("LOOM_PREVIEW_ORIGIN")
	if config == "" {
		if parent.Scheme == "https" {
			return ""
		}
		return "http://" + net.JoinHostPort(parent.Hostname(), "*")
	}
	const marker = "54321"
	u, err := previewOrigin(strings.ReplaceAll(config, "{port}", marker))
	if err != nil {
		return ""
	}
	if strings.HasSuffix(strings.TrimSuffix(config, "/"), ":{port}") {
		return u.Scheme + "://" + net.JoinHostPort(u.Hostname(), "*")
	}
	return u.String()
}

// Keep navigation on the authenticated application, including after browser
// URL normalization. The redirect is constructed with the trusted origin.
func previewPath(raw string) (string, error) {
	if raw == "" {
		return "/", nil
	}
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 2048 || !strings.HasPrefix(raw, "/") ||
		u.IsAbs() || u.Host != "" || strings.HasPrefix(u.Path, "//") ||
		strings.ContainsAny(u.Path, "\\\r\n\t") || strings.HasPrefix(u.Path, "/__loom_preview/") {
		return "", errors.New("use a path within the preview application")
	}
	return u.String(), nil
}
