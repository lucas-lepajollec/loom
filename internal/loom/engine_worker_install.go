package loom

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func nodeUnitValue(value string) (string, error) {
	if strings.ContainsAny(value, "\r\n\x00") {
		return "", errors.New("invalid service value")
	}
	value = strings.ReplaceAll(value, "%", "%%")
	value = strings.ReplaceAll(value, "$", "$$")
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "\"", "\\\"")
	return "\"" + value + "\"", nil
}
func engineWorkerUnit(exe, home, listen string) (string, error) {
	values := []string{exe, home, listen}
	for i, v := range values {
		q, err := nodeUnitValue(v)
		if err != nil {
			return "", err
		}
		values[i] = q
	}
	return fmt.Sprintf(`[Unit]
Description=Loom engine node
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s node serve --home %s --listen %s
Restart=on-failure
RestartSec=3
KillMode=control-group
UMask=0077

[Install]
WantedBy=default.target
`, values[0], values[1], values[2]), nil
}
func installEngineWorker(home, listen string) error {
	if runtime.GOOS != "linux" || os.Geteuid() == 0 {
		return errors.New("node install requires a Linux user session, without sudo")
	}
	if _, err := readNodeToken(); err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	body, err := engineWorkerUnit(exe, home, listen)
	if err != nil {
		return err
	}
	config, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(config, "systemd", "user")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	path := filepath.Join(dir, "loom-node.service")
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		return err
	}
	cmd := exec.Command("systemctl", "--user", "daemon-reload")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}
	if err := putStr(bkState, "node_listener", listen); err != nil {
		return err
	}
	fmt.Printf("Installed %s. Start explicitly with: systemctl --user enable --now loom-node\n", path)
	return nil
}

// The listener belongs to the node service, not llama-server's private front.
func handleNodeNetwork(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodGet) {
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "managed": true, "status": map[string]any{"host": webBound.host, "port": webBound.port, "exposed": !localLoopbackHost(webBound.host), "url": "http://" + r.Host + "/v1", "hint": "Change the listener with loom node listen lan|ADDR:PORT|local."}})
}
