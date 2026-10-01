package openai

import (
	"errors"
	"net"
	"net/url"
	"strings"
)

func ValidateEndpoint(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return "", errors.New("URL de base HTTPS requise, sans identifiants, paramètres ni fragment")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && LocalNetworkHost(u.Hostname())) {
		return "", errors.New("HTTPS requis (HTTP autorisé seulement sur cette machine ou le réseau local)")
	}
	if len(raw) > 2048 {
		return "", errors.New("URL trop longue")
	}
	return strings.TrimRight(u.String(), "/"), nil
}

// LocalNetworkHost reports whether plain HTTP stays on this machine or the local
// network: loopback, private, link-local or CGNAT (Tailscale) addresses, and
// local host names (localhost, single label, .local, .lan, .home.arpa…).
func LocalNetworkHost(host string) bool {
	h := strings.ToLower(strings.TrimSuffix(host, "."))
	if ip := net.ParseIP(h); ip != nil {
		_, cgnat, _ := net.ParseCIDR("100.64.0.0/10")
		return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || cgnat.Contains(ip)
	}
	if h == "localhost" || !strings.Contains(h, ".") {
		return h != ""
	}
	for _, suffix := range []string{".localhost", ".local", ".lan", ".home", ".internal", ".home.arpa", ".ts.net"} {
		if strings.HasSuffix(h, suffix) {
			return true
		}
	}
	return false
}
