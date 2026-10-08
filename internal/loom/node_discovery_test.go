package loom

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func discoveryTestSocket(t *testing.T) *net.UDPConn {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Skipf("loopback UDP unavailable in this environment: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func discoveryTestResponder(t *testing.T, host string) (*net.UDPConn, nodeDescriptor) {
	t.Helper()
	conn := discoveryTestSocket(t)
	node, err := describeNode()
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { defer close(done); serveNodeDiscovery(conn, node, host, 2511) }()
	t.Cleanup(func() { conn.Close(); <-done })
	return conn, node
}

func TestNodeDiscoveryLoopbackProbeAndRateLimit(t *testing.T) {
	_, token := pairTestNode(t)
	server, descriptor := discoveryTestResponder(t, "127.0.0.1")
	client := discoveryTestSocket(t)
	var body [4096]byte
	probe := func(message string, expect bool) []byte {
		t.Helper()
		if _, err := client.WriteToUDP([]byte(message), server.LocalAddr().(*net.UDPAddr)); err != nil {
			t.Skipf("UDP send unavailable: %v", err)
		}
		_ = client.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		n, _, err := client.ReadFromUDP(body[:])
		if expect && err != nil {
			t.Fatalf("discovery response: %v", err)
		}
		if !expect && err == nil {
			t.Fatal("unexpected discovery response")
		}
		return append([]byte(nil), body[:n]...)
	}
	probe("not a probe", false)
	raw := probe("LOOM?", true)
	var node discoveredNode
	if json.Unmarshal(raw, &node) != nil || node.ID != descriptor.ID || node.Name != descriptor.Name || node.Version != Version || node.Port != 2511 || node.Paired || node.Address != "http://127.0.0.1:2511" {
		t.Fatalf("wrong advertisement: %s", raw)
	}
	if strings.Contains(string(raw), token) || strings.Contains(string(raw), readAPIKey()) || strings.Contains(string(raw), "key") || strings.Contains(string(raw), "code") {
		t.Fatal("discovery leaked secrets")
	}
	probe("LOOM?", false)
}

func TestNodeDiscoveryLoopbackCollectAndExcludeLinked(t *testing.T) {
	pairTestNode(t)
	_ = setEngineNode(nil)
	t.Cleanup(func() { _ = setEngineNode(nil) })
	if err := updateNodePairState(func(s *nodePairState) error { s.Main = &pairedMain{ID: "other", Name: "Other Loom"}; return nil }); err != nil {
		t.Fatal(err)
	}
	server, descriptor := discoveryTestResponder(t, "0.0.0.0")
	nodes := discoverNodes(t.Context(), []nodeDiscoveryTarget{{Local: net.IPv4(127, 0, 0, 1), Probe: server.LocalAddr().(*net.UDPAddr)}}, 120*time.Millisecond)
	if len(nodes) != 1 || nodes[0].ID != descriptor.ID || !nodes[0].Paired || nodes[0].Address != "http://127.0.0.1:2511" {
		t.Fatalf("collection failed: %+v", nodes)
	}
	if len(unlinkedDiscoveredNodes(nodes)) != 1 {
		t.Fatal("paired elsewhere was excluded")
	}
}

func TestNodeDiscoveryExcludeLinked(t *testing.T) {
	testHome(t)
	_ = setEngineNode(nil)
	t.Cleanup(func() { _ = setEngineNode(nil) })
	descriptor := nodeDescriptor{ID: "fixture-node"}
	nodes := []discoveredNode{{ID: descriptor.ID, Name: "GPU", Address: "http://127.0.0.1:2511", Port: 2511}}
	unlinked := discoveredNode{ID: "unlinked", Name: "Unlinked", Address: "http://127.0.0.1:2513", Port: 2513}
	m := RemoteMachine{ID: "gpu", Host: "127.0.0.1", NodeID: descriptor.ID}
	if err := putStoreJSON(bkState, remoteMachinesState, []RemoteMachine{m}); err != nil {
		t.Fatal(err)
	}
	if len(unlinkedDiscoveredNodes(nodes)) != 1 {
		t.Fatal("SSH-only machine incorrectly excluded")
	}
	if err := putStoreJSON(bkState, machineNodePrefix+m.ID, &engineNode{URL: nodes[0].Address, NodeID: descriptor.ID}); err != nil {
		t.Fatal(err)
	}
	filtered := unlinkedDiscoveredNodes(append(nodes, unlinked))
	if len(filtered) != 1 || filtered[0].ID != "unlinked" {
		t.Fatal("linked node not excluded")
	}
	// Token-based historical links also exclude by endpoint, without an ID.
	if err := putStoreJSON(bkState, machineNodePrefix+m.ID, &engineNode{URL: nodes[0].Address}); err != nil {
		t.Fatal(err)
	}
	if len(unlinkedDiscoveredNodes(nodes)) != 0 {
		t.Fatal("legacy link not excluded")
	}
	if err := setEngineNode(&engineNode{URL: unlinked.Address, Role: "engine-node"}); err != nil {
		t.Fatal(err)
	}
	if len(unlinkedDiscoveredNodes([]discoveredNode{unlinked})) != 0 {
		t.Fatal("active link not excluded")
	}
}

func TestNodeDiscoveryValidationAndLoopbackDisabled(t *testing.T) {
	conn, err := startNodeDiscovery("127.0.0.1:2511")
	if err != nil || conn != nil {
		t.Fatal("loopback node enabled LAN discovery")
	}
	conn, err = startNodeDiscovery("[::1]:2511")
	if err != nil || conn != nil {
		t.Fatal("IPv6 loopback enabled LAN discovery")
	}
	source := net.IPv4(192, 168, 1, 20)
	valid := discoveredNode{ID: "fixture", Name: "GPU", Address: "http://192.168.1.20:2511", Port: 2511}
	if !validDiscoveredNode(valid, source) {
		t.Fatal("valid advertisement refused")
	}
	for _, address := range []string{"http://192.168.1.21:2511", "http://192.168.1.20:22", "ftp://192.168.1.20:2511", "http://secret@192.168.1.20:2511", "http://192.168.1.20:2511/?secret=x", "http://192.168.1.20:2511/api", "http://example.com:2511"} {
		bad := valid
		bad.Address = address
		if validDiscoveredNode(bad, source) {
			t.Fatal("unsafe advertisement accepted", address)
		}
	}
	bad := valid
	bad.ID = ""
	if validDiscoveredNode(bad, source) {
		t.Fatal("empty identity accepted")
	}
	nodes := discoverNodes(t.Context(), nil, nodeDiscoveryWait)
	raw, _ := json.Marshal(map[string]any{"nodes": nodes})
	if string(raw) != `{"nodes":[]}` {
		t.Fatal("empty discovery shape", string(raw))
	}
	w := httptest.NewRecorder()
	handleMachinesDiscover(w, httptest.NewRequest(http.MethodPost, "/api/machines/discover", nil))
	if w.Code != 405 {
		t.Fatal("discovery accepted mutation method")
	}
}
