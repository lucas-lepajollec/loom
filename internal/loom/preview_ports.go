package loom

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
)

func reservedPreviewPorts() map[int]bool {
	ports := map[int]bool{defaultWebPort: true, webBound.port: true, 2511: true}
	if u, err := url.Parse(loadCodemanConfig().URL); err == nil {
		if port, err := strconv.Atoi(u.Port()); err == nil {
			ports[port] = true
		}
	}
	return ports
}

func readListeningPorts(r io.Reader, excluded, found map[int]bool) {
	scan := bufio.NewScanner(r)
	for scan.Scan() {
		fields := strings.Fields(scan.Text())
		if len(fields) < 4 || fields[3] != "0A" {
			continue
		}
		_, hex, ok := strings.Cut(fields[1], ":")
		port, err := strconv.ParseUint(hex, 16, 16)
		if ok && err == nil && port >= 1024 && !excluded[int(port)] {
			found[int(port)] = true
		}
	}
}

func (m *devPreviewManager) ports(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodGet) || !usageVaultAccess(w) {
		return
	}
	ports := []int{}
	if target := r.URL.Query().Get("target"); target == "" || target == "local" {
		excluded, found := reservedPreviewPorts(), map[int]bool{}
		m.Lock()
		for _, p := range m.items {
			excluded[p.listener.Addr().(*net.TCPAddr).Port] = true
		}
		m.Unlock()
		for _, name := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
			if f, err := os.Open(name); err == nil {
				readListeningPorts(f, excluded, found)
				f.Close()
			}
		}
		for port := range found {
			ports = append(ports, port)
		}
		slices.Sort(ports)
	}
	sendJSON(w, 200, map[string]any{"ok": true, "ports": ports})
}
