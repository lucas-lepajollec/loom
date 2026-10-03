package loom

import (
	"net"
	"net/http"
	"strings"
)

// A loopback listener alone does not prevent DNS rebinding: an attacker's host
// name can resolve to it and satisfy browser same-origin checks. Anonymous
// control access requires both a literal loopback Host and a loopback peer.
// Forwarded headers are deliberately ignored. Authenticated deployments and
// the verified E2E transport retain their separate access contracts.
func unprotectedLocalRequest(r *http.Request) bool {
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	peer, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		peer = r.RemoteAddr
	}
	ip := net.ParseIP(peer)
	return isLoopbackHost(strings.TrimSuffix(host, ".")) && ip != nil && ip.IsLoopback()
}
