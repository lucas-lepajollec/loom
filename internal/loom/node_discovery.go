package loom

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"sync"
	"time"
)

const nodeDiscoveryPort = 2512
const nodeDiscoveryWait = 1200 * time.Millisecond

type discoveredNode struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version string `json:"version"`
	Address string `json:"address"`
	Port    int    `json:"port"`
	Paired  bool   `json:"paired"`
}

func startNodeDiscovery(listener string) (*net.UDPConn, error) {
	host, port, err := net.SplitHostPort(listener)
	if err != nil {
		return nil, err
	}
	if localLoopbackHost(host) {
		return nil, nil
	}
	ip := net.ParseIP(host)
	// Discovery is IPv4 broadcast; a specific IPv6 listener cannot serve it.
	if ip != nil && ip.To4() == nil && !ip.IsUnspecified() {
		return nil, nil
	}
	// A socket bound to the unicast listener IP does not receive directed
	// broadcast packets. Enable this wildcard socket only for a LAN listener.
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: nodeDiscoveryPort})
	if err != nil {
		return nil, err
	}
	node, err := describeNode()
	if err != nil {
		conn.Close()
		return nil, err
	}
	n, _ := strconv.Atoi(port)
	go serveNodeDiscovery(conn, node, host, n)
	return conn, nil
}

func serveNodeDiscovery(conn *net.UDPConn, node nodeDescriptor, host string, port int) {
	var buffer [1024]byte
	remotes := map[string]time.Time{}
	window, count := time.Now(), 0
	for {
		n, remote, err := conn.ReadFromUDP(buffer[:])
		if err != nil {
			return
		}
		if string(buffer[:n]) != "LOOM?" || !localNetworkHost(remote.IP.String()) {
			continue
		}
		now := time.Now()
		if now.Sub(window) >= time.Second {
			window, count = now, 0
			for ip, at := range remotes {
				if now.Sub(at) >= time.Second {
					delete(remotes, ip)
				}
			}
		}
		ip := remote.IP.String()
		if count >= 100 || now.Sub(remotes[ip]) < time.Second {
			continue
		}
		count++
		remotes[ip] = now
		addressHost := host
		if parsed := net.ParseIP(host); host == "" || (parsed != nil && parsed.IsUnspecified()) {
			// Ask the routing table for the address reachable by this requester.
			route, err := net.DialUDP("udp4", nil, remote)
			if err != nil {
				continue
			}
			addressHost = route.LocalAddr().(*net.UDPAddr).IP.String()
			route.Close()
		}
		body, err := json.Marshal(discoveredNode{ID: node.ID, Name: node.Name, Version: node.Version,
			Address: "http://" + net.JoinHostPort(addressHost, strconv.Itoa(port)), Port: port, Paired: nodeIsPaired()})
		if err == nil {
			_, _ = conn.WriteToUDP(body, remote)
		}
	}
}

type nodeDiscoveryTarget struct {
	Local net.IP
	Probe *net.UDPAddr
}

func nodeDiscoveryTargets() []nodeDiscoveryTarget {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var targets []nodeDiscoveryTarget
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagBroadcast == 0 {
			continue
		}
		addresses, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, address := range addresses {
			ip, network, err := net.ParseCIDR(address.String())
			if err != nil || ip.To4() == nil || ip.IsLoopback() {
				continue
			}
			ones, bits := network.Mask.Size()
			if bits != 32 || ones >= 31 {
				continue
			}
			broadcast := append(net.IP(nil), ip.To4()...)
			for i := range broadcast {
				broadcast[i] |= ^network.Mask[i]
			}
			targets = append(targets, nodeDiscoveryTarget{Local: ip.To4(), Probe: &net.UDPAddr{IP: broadcast, Port: nodeDiscoveryPort}})
		}
	}
	return targets
}

func discoverNodes(ctx context.Context, targets []nodeDiscoveryTarget, wait time.Duration) []discoveredNode {
	nodes := map[string]discoveredNode{}
	var mu sync.Mutex
	var workers sync.WaitGroup
	deadline := time.Now().Add(wait)
	if limit, ok := ctx.Deadline(); ok && limit.Before(deadline) {
		deadline = limit
	}
	for _, target := range targets {
		workers.Go(func() {
			conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: target.Local})
			if err != nil {
				return
			}
			defer conn.Close()
			stop := context.AfterFunc(ctx, func() { conn.Close() })
			defer stop()
			_ = conn.SetDeadline(deadline)
			if _, err := conn.WriteToUDP([]byte("LOOM?"), target.Probe); err != nil {
				return
			}
			var buffer [4096]byte
			for {
				n, remote, err := conn.ReadFromUDP(buffer[:])
				if err != nil {
					return
				}
				var node discoveredNode
				if json.Unmarshal(buffer[:n], &node) != nil || !validDiscoveredNode(node, remote.IP) {
					continue
				}
				mu.Lock()
				nodes[node.ID] = node
				mu.Unlock()
			}
		})
	}
	workers.Wait()
	list := make([]discoveredNode, 0, len(nodes))
	for _, node := range nodes {
		list = append(list, node)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	return list
}

func validDiscoveredNode(node discoveredNode, source net.IP) bool {
	if !validPairLabel(node.ID) || !validPairLabel(node.Name) || len(node.Version) > 128 || node.Port < 1 || node.Port > 65535 {
		return false
	}
	u, err := url.Parse(node.Address)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return false
	}
	return localNetworkHost(source.String()) && net.ParseIP(u.Hostname()).Equal(source) && u.Port() == strconv.Itoa(node.Port)
}

func unlinkedDiscoveredNodes(nodes []discoveredNode) []discoveredNode {
	list := make([]discoveredNode, 0, len(nodes))
	machines := loadRemoteMachines()
	active := currentEngineNode()
	for _, node := range nodes {
		linked := active != nil && !active.Direct && (active.NodeID == node.ID || active.URL == node.Address)
		for _, m := range machines {
			if machineNodeMatches(m, node) {
				linked = true
				break
			}
		}
		if !linked {
			list = append(list, node)
		}
	}
	return list
}

func handleMachinesDiscover(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !workspaceMethod(w, r, http.MethodGet) || !usageVaultAccess(w) {
		return
	}
	nodes := discoverNodes(r.Context(), nodeDiscoveryTargets(), nodeDiscoveryWait)
	sendJSON(w, 200, map[string]any{"nodes": unlinkedDiscoveredNodes(nodes)})
}
