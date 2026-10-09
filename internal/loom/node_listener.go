package loom

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Automatic exposure uses only private IPv4 routes, including CGNAT/Tailscale.
func privateNodeIPv4(address string) bool {
	ip := net.ParseIP(address).To4()
	return ip != nil && (ip[0] == 10 || ip[0] == 172 && ip[1] >= 16 && ip[1] <= 31 || ip[0] == 192 && ip[1] == 168 || ip[0] == 100 && ip[1] >= 64 && ip[1] <= 127)
}
func nodeLANAddress(route, addresses string) (string, error) {
	fields := strings.Fields(route)
	for i, field := range fields {
		if field == "src" && i+1 < len(fields) && privateNodeIPv4(fields[i+1]) {
			return fields[i+1], nil
		}
	}
	for _, address := range strings.Fields(addresses) {
		if privateNodeIPv4(address) {
			return address, nil
		}
	}
	return "", errors.New("no private LAN address found; use loom node listen ADDR:PORT or local")
}
func resolveNodeListener(listen string) (string, error) {
	switch listen {
	case "local":
		return defaultNodeListen, nil
	case "lan":
		route, _ := exec.Command("ip", "route", "get", "1.1.1.1").Output()
		addresses, _ := exec.Command("hostname", "-I").Output()
		address, err := nodeLANAddress(string(route), string(addresses))
		if err != nil {
			return "", err
		}
		return net.JoinHostPort(address, "2511"), nil
	}
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return "", fmt.Errorf("invalid node listener: %w", err)
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return "", errors.New("invalid node port")
	}
	if host == "" || net.ParseIP(host) == nil && host != "localhost" {
		return "", errors.New("node listener requires an IP address or localhost")
	}
	return listen, nil
}

func changeNodeListener(home, listen string) error {
	config, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	path := filepath.Join(config, "systemd", "user", "loom-node.service")
	previous, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	old := getStr(bkState, "node_listener")
	if old == "" {
		old = defaultNodeListen
	}
	oldValue, err := nodeUnitValue(old)
	if err != nil {
		return err
	}
	newValue, err := nodeUnitValue(listen)
	if err != nil {
		return err
	}
	homeValue, err := nodeUnitValue(home)
	if err != nil {
		return err
	}
	if !strings.Contains(string(previous), "--home "+homeValue) || !strings.Contains(string(previous), "--listen "+oldValue) {
		return errors.New("existing node unit does not match this home/listener; use its configured --home or reinstall the node")
	}
	body := strings.Replace(string(previous), "--listen "+oldValue, "--listen "+newValue, 1)
	restore := func() {
		_ = os.WriteFile(path, previous, 0644)
		_ = putStr(bkState, "node_listener", old)
		_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
		_ = exec.Command("systemctl", "--user", "restart", "loom-node").Run()
	}
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		restore()
		return err
	}
	if out, err := exec.Command("systemctl", "--user", "daemon-reload").CombinedOutput(); err != nil {
		restore()
		return fmt.Errorf("reload loom-node: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if err := putStr(bkState, "node_listener", listen); err != nil {
		restore()
		return err
	}
	if out, err := exec.Command("systemctl", "--user", "restart", "loom-node").CombinedOutput(); err != nil {
		restore()
		return fmt.Errorf("restart loom-node: %w: %s", err, strings.TrimSpace(string(out)))
	}
	fmt.Println("Node address: http://" + listen)
	return nil
}

type nodePairDetails struct {
	Address string      `json:"address"`
	Code    string      `json:"code,omitempty"`
	Expires string      `json:"expires,omitempty"`
	Main    *pairedMain `json:"main,omitempty"`
}

func printNodePairDetails(listen string, ifUnpaired, asJSON bool) error {
	details := nodePairDetails{Address: "http://" + listen}
	now := time.Now()
	// Check and issue in one transaction; never replace a paired code implicitly.
	code, err := generatePairCode()
	if err != nil {
		return err
	}
	if _, err := readNodeToken(); err != nil {
		return err
	}
	err = updateNodePairState(func(s *nodePairState) error {
		details.Main = s.Main
		if ifUnpaired && s.Main != nil {
			return nil
		}
		setNodePairCode(s, code, now)
		details.Code, details.Expires = code, now.Add(pairCodeLifetime).UTC().Format(time.RFC3339)
		return nil
	})
	if err != nil {
		return err
	}
	if asJSON {
		return json.NewEncoder(os.Stdout).Encode(details)
	}
	fmt.Println("Node address: " + details.Address)
	if details.Code != "" {
		printNodePairCode(details.Code)
		fmt.Println("Expires: " + details.Expires)
	} else if details.Main != nil {
		fmt.Println("already paired with " + details.Main.Name)
	}
	fmt.Println("In Loom: Machines › Add a machine, or let Loom find it on the network")
	return nil
}
