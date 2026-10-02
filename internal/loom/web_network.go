package loom

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
)

// Server mode: Loom's interface (and its control API) listening on the local
// network, for example on a server or a VM, used from other machines. It is
// never opened without a control key: enabling it creates one when missing.
// The /v1 model API keeps its own switch (sys_network.go).

const webHostKey = "WEB_HOST"

// webBound is the address the running interface listens on (set by cmdWeb),
// to tell the user when a restart is needed.
var webBound struct {
	host string
	port int
}

func webHost() string {
	h := strings.TrimSpace(ReadConfig()[webHostKey])
	if h == "" {
		return hostLocalOnly
	}
	return h
}

func isLoopbackHost(h string) bool {
	if strings.EqualFold(h, "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(h, "[]"))
	return ip != nil && ip.IsLoopback()
}

// webListenCheck refuses to expose the interface without a control key.
func webListenCheck(host string) error {
	if isLoopbackHost(host) {
		return nil
	}
	hash, err := webKeyHashErr()
	if err != nil {
		return errors.New("control key unreadable: Loom remains closed to the network")
	}
	if hash == "" {
		return fmt.Errorf("the interface cannot open to the network (%s) without a control key: create one with `loom set-web-key`", host)
	}
	return nil
}

type webNetStatus struct {
	Exposed  bool   `json:"exposed"`  // configured to listen on the network
	Running  bool   `json:"running"`  // the running interface already listens on it
	Host     string `json:"host"`     // configured address
	Port     int    `json:"port"`     // interface port
	URL      string `json:"url"`      // address to open from another machine
	KeySet   bool   `json:"key_set"`  // a control key protects the interface
	Restart  bool   `json:"restart"`  // a restart is needed to apply the setting
	Firewall string `json:"firewall"` // "ouvert", "ferme", "inconnu"
}

func webNetworkStatus() webNetStatus {
	host := webHost()
	st := webNetStatus{Exposed: !isLoopbackHost(host), Host: host, Port: webBound.port, KeySet: webKeyConfigured()}
	st.Running = webBound.host != "" && !isLoopbackHost(webBound.host)
	st.Restart = webBound.host != "" && webBound.host != host
	if st.Port > 0 {
		st.URL = fmt.Sprintf("http://%s:%d", localIP(), st.Port)
		st.Firewall = firewallState(st.Port)
	}
	return st
}

// setWebExposure writes WEB_HOST. Opening creates a control key when none
// exists and returns it once, so the browser can keep using the interface.
func setWebExposure(on bool) (webNetStatus, string, error) {
	created := ""
	host := hostLocalOnly
	if on {
		host = hostAllInterfaces
		if !webKeyConfigured() {
			buf := make([]byte, 24)
			if _, err := rand.Read(buf); err != nil {
				return webNetStatus{}, "", err
			}
			created = "loom-web-" + hex.EncodeToString(buf)
			if err := storeWebKey(created); err != nil {
				return webNetStatus{}, "", err
			}
		}
	}
	if err := SetConfigKey(webHostKey, host); err != nil {
		return webNetStatus{}, "", err
	}
	if webBound.port > 0 && !firewallInert {
		if on {
			_ = firewallOpen(webBound.port)
		} else {
			_ = firewallClose(webBound.port)
		}
	}
	return webNetworkStatus(), created, nil
}

// GET: state. POST {exposed}: switch; {restart:true}: restart the interface
// service when Loom runs as one (otherwise the user restarts it).
func handleWebNetwork(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		sendJSON(w, 200, map[string]any{"ok": true, "status": webNetworkStatus()})
		return
	}
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		Exposed *bool `json:"exposed"`
		Restart bool  `json:"restart"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	if req.Restart {
		ok, msg := restartAfterUpdate()
		if !ok {
			msg = "Restart Loom to apply (service or `loom web` command)."
		}
		sendJSON(w, 200, map[string]any{"ok": true, "restarting": ok, "message": msg})
		return
	}
	if req.Exposed == nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "exposed required"})
		return
	}
	st, key, err := setWebExposure(*req.Exposed)
	if err != nil {
		sendJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	out := map[string]any{"ok": true, "status": st}
	if key != "" {
		out["key"] = key // shown once; the browser keeps it to stay signed in
	}
	sendJSON(w, 200, out)
}
