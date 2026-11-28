package loom

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func nodeRediscoveryFixture(t *testing.T, oldURL, advertisedID, verifiedID string, signed bool) (RemoteMachine, *engineNode, *httptest.Server, *atomic.Int32) {
	t.Helper()
	testHome(t)
	const key = "synthetic-node-control-key"
	n := &engineNode{NodeID: "paired-id", URL: oldURL, V1: oldURL, APIKey: "synthetic-inference-key", WebKey: key,
		Role: "engine-node", Hostname: "fixture", Modules: []string{"engine", "terminal"}, Handshake: 1, LinkedAt: 1234}
	u, _ := url.Parse(oldURL)
	m, err := savePairedMachine(RemoteMachine{Name: "fixture", Host: u.Hostname(), Folders: []string{"/work"}}, n)
	if err != nil {
		t.Fatal(err)
	}
	if err := setEngineNode(n); err != nil {
		t.Fatal(err)
	}

	var probes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+key {
			t.Error("node request did not use the saved credential")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/node/info":
			probes.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "id": verifiedID, "role": "engine-node",
				"handshake": 1, "v1_same_origin": true, "hostname": "fixture", "version": "fixture-new"})
		case "/api/node/workspace":
			raw, _ := io.ReadAll(r.Body)
			if r.Method != http.MethodPost || string(raw) != `{"path":"/work"}` {
				t.Errorf("request changed during recovery: %s %s", r.Method, raw)
			}
			_, _ = io.WriteString(w, `{"ok":true}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	conn := discoveryTestSocket(t)
	address, _ := url.Parse(server.URL)
	port, _ := strconv.Atoi(address.Port())
	ad := discoveredNode{ID: advertisedID, Name: "synthetic", Address: server.URL, Port: port}
	done := make(chan struct{})
	go func() {
		defer close(done)
		var buf [1024]byte
		for {
			n, source, err := conn.ReadFromUDP(buf[:])
			if err != nil {
				return
			}
			req := string(buf[:n])
			reply := ad
			if signed {
				parts := strings.Fields(req)
				if len(parts) != 3 {
					continue
				}
				reply.Proof = hex.EncodeToString(nodeDiscoveryProof(key, parts[2], reply))
			} else if req != "LOOM?" {
				continue
			}
			raw, _ := json.Marshal(reply)
			_, _ = conn.WriteToUDP(raw, source)
		}
	}()
	t.Cleanup(func() { conn.Close(); <-done })
	oldTargets := nodeRediscoveryTargets
	nodeRediscoveryTargets = func() []nodeDiscoveryTarget {
		return []nodeDiscoveryTarget{{Local: net.IPv4(127, 0, 0, 1), Probe: conn.LocalAddr().(*net.UDPAddr)}}
	}
	t.Cleanup(func() { nodeRediscoveryTargets = oldTargets })
	return m, n, server, &probes
}

func TestRediscoverMachineNodeUpdatesPairedAndActivePorts(t *testing.T) {
	m, n, server, probes := nodeRediscoveryFixture(t, "http://127.0.0.1:1", "paired-id", "paired-id", false)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var result struct{ OK bool }
	if err := nodeMachineJSON(ctx, m, http.MethodPost, "/api/node/workspace", strings.NewReader(`{"path":"/work"}`), &result); err != nil || !result.OK {
		t.Fatal(result, err)
	}
	if probes.Load() != 1 {
		t.Fatalf("expected one authenticated identity probe, got %d", probes.Load())
	}
	assertLink := func(saved *engineNode) {
		t.Helper()
		if saved == nil || saved.URL != server.URL || saved.V1 != server.URL || saved.NodeID != n.NodeID ||
			saved.WebKey != n.WebKey || saved.APIKey != n.APIKey || saved.LinkedAt != n.LinkedAt {
			t.Fatal("recovery did not preserve identity and credentials")
		}
	}
	assertLink(savedMachineNode(m))
	assertLink(currentEngineNode())
	var persisted engineNode
	if !getStoreJSON(bkState, engineNodeState, &persisted) || persisted.URL != server.URL ||
		persisted.WebKey == n.WebKey || persisted.APIKey == n.APIKey {
		t.Fatal("active engine was not stored with protected credentials")
	}
}

func TestRediscoverMachineNodeRequiresMatchingIdentity(t *testing.T) {
	for _, tt := range []struct {
		name, host, advertised, verified string
		signed, ok                       bool
		probes                           int32
	}{
		{"wrong advertisement", "127.0.0.1", "other-id", "paired-id", true, false, 0},
		{"wrong authenticated ID", "127.0.0.1", "paired-id", "other-id", true, false, 1},
		{"unsigned different host", "127.0.0.2", "paired-id", "paired-id", false, false, 0},
		{"signed different host", "127.0.0.2", "paired-id", "paired-id", true, true, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m, n, server, probes := nodeRediscoveryFixture(t, "http://"+tt.host+":1", tt.advertised, tt.verified, tt.signed)
			got := rediscoverMachineNode(t.Context(), m, n)
			want := n.URL
			if tt.ok {
				want = server.URL
			}
			if (got != nil) != tt.ok || savedMachineNode(m).URL != want || currentEngineNode().URL != want || probes.Load() != tt.probes {
				t.Fatalf("unexpected recovery: saved %s, probes %d", savedMachineNode(m).URL, probes.Load())
			}
		})
	}
}

func TestRediscoverMachineNodeLeavesOtherEngineSelected(t *testing.T) {
	m, n, server, _ := nodeRediscoveryFixture(t, "http://127.0.0.1:1", "paired-id", "paired-id", true)
	other := &engineNode{NodeID: "other-node", URL: n.URL, V1: n.URL, WebKey: n.WebKey, Role: n.Role}
	if err := setEngineNode(other); err != nil {
		t.Fatal(err)
	}
	if next := rediscoverMachineNode(t.Context(), m, n); next == nil || next.URL != server.URL {
		t.Fatal("paired node URL was not repaired")
	}
	if active := currentEngineNode(); active.NodeID != other.NodeID || active.URL != other.URL {
		t.Fatal("unrelated engine selection changed")
	}
}

func TestAgentMachinesReturnsPairedNodeURL(t *testing.T) {
	gatewayTestHome(t)
	n := &engineNode{NodeID: "paired-fixture", URL: "http://192.168.0.200:2550", WebKey: "fixture-secret",
		APIKey: "inference-secret", Role: "engine-node", Handshake: 1}
	m, err := savePairedMachine(RemoteMachine{Name: "GPU", Host: "192.168.0.200"}, n)
	if err != nil {
		t.Fatal(err)
	}
	if err := saveRemoteMachine(RemoteMachine{ID: "ssh", Host: "other", User: "fixture"}); err != nil {
		t.Fatal(err)
	}
	s := connectOnDemandGateway(t)
	for _, name := range []string{"loom_machines", "loom_overview"} {
		result, err := callMCPFixture(t.Context(), s, name, `{}`)
		if err != nil || result.IsError {
			t.Fatal(name, err)
		}
		raw, _ := json.Marshal(result.StructuredContent)
		var response struct{ Machines []agentMachine }
		if err := json.Unmarshal(raw, &response); err != nil || len(response.Machines) != 3 ||
			response.Machines[0].NodeURL != "" || response.Machines[1].ID != m.ID ||
			response.Machines[1].NodeURL != n.URL || response.Machines[2].NodeURL != "" ||
			strings.Contains(string(raw), n.WebKey) || strings.Contains(string(raw), n.APIKey) {
			t.Fatal(name, "unexpected machine response")
		}
	}
}
