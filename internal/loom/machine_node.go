package loom

import (
	"bytes"
	"context"
	"crypto/hmac"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/store"
	bolt "go.etcd.io/bbolt"
)

// Machine maintenance links are independent of the active inference engine.
// Credentials use the same protected store as engine_node, never machine JSON.
const machineNodePrefix = "machine_engine_node:"

func savedMachineNode(m RemoteMachine) *engineNode {
	var n engineNode
	if getStoreJSON(bkState, machineNodePrefix+m.ID, &n) && n.URL != "" {
		if migrated, err := unsealEngineNode(&n); err == nil {
			if migrated {
				if sealed, serr := sealEngineNode(&n); serr == nil {
					_ = putStoreJSON(bkState, machineNodePrefix+m.ID, sealed)
				}
			}
			return &n
		}
		return nil
	}
	// Existing active links need no second key entry. Persisted maintenance links
	// remain usable after another engine is selected.
	if active := currentEngineNode(); active != nil && !active.Direct && active.Role == "engine-node" {
		if m.NodeID != "" && active.NodeID != "" {
			if m.NodeID == active.NodeID {
				return active
			}
			return nil
		}
		u, err := url.Parse(active.URL)
		if err == nil && (u.Hostname() == m.Host || active.Hostname == m.Hostname && m.Hostname != "") {
			return active
		}
	}
	return nil
}

func machineNodeURL(m RemoteMachine) string {
	if m.NodeID == "" {
		return ""
	}
	if n := savedMachineNode(m); n != nil && !n.Direct && (n.NodeID == "" || n.NodeID == m.NodeID) {
		base, _, _ := cleanNodeURL(n.URL)
		return base
	}
	return ""
}

func nodeMaintenanceProbe(ctx context.Context, rawURL, key string) (*engineNode, error) {
	base, _, err := cleanNodeURL(rawURL)
	if err != nil {
		return nil, err
	}
	key = strings.TrimSpace(key)
	if key == "" || len(key) > 4096 || strings.ContainsAny(key, "\r\n\x00") {
		return nil, errors.New("valid engine node control key required")
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/node/info", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := nodeClient.Do(req)
	if err != nil {
		return nil, errors.New("engine node unreachable")
	}
	defer resp.Body.Close()
	var info struct {
		ID         string   `json:"id"`
		Modules    []string `json:"modules"`
		Handshake  int      `json:"handshake"`
		OK         bool     `json:"ok"`
		Hostname   string   `json:"hostname"`
		Version    string   `json:"version"`
		Role       string   `json:"role"`
		SameOrigin bool     `json:"v1_same_origin"`
	}
	if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&info) != nil || !info.OK || info.Role != "engine-node" {
		return nil, errors.New("engine node rejected the key or does not support maintenance")
	}
	if err := checkNodeHandshake(info.Handshake); err != nil {
		return nil, err
	}
	n := &engineNode{NodeID: info.ID, Modules: info.Modules, Handshake: info.Handshake, URL: base, WebKey: key, Hostname: info.Hostname, Version: info.Version, Role: info.Role, LinkedAt: time.Now().UnixMilli()}
	if info.SameOrigin {
		n.V1 = base
	}
	return n, nil
}

var nodeRediscoveryTargets = nodeDiscoveryTargets

// rediscoverMachineNode is called after a failed dial. A signed response can
// move the link to a new LAN IP. Older nodes can suggest a port on the already
// trusted IP. Both paths confirm the ID through the authenticated control API.
func rediscoverMachineNode(ctx context.Context, m RemoteMachine, n *engineNode) *engineNode {
	if ctx.Err() != nil || n == nil || n.Direct || m.NodeID == "" || m.NodeID != n.NodeID || !validNodeCredential(n.WebKey) {
		return nil
	}
	current := savedMachineNode(m)
	if current == nil || current.NodeID != n.NodeID || current.WebKey != n.WebKey {
		return nil
	}
	if current.URL != n.URL {
		return current
	}
	_, old, err := cleanNodeURL(current.URL)
	if err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	challenge, err := newNodeToken()
	if err != nil {
		return nil
	}
	probes := []string{"LOOM? " + m.NodeID + " " + challenge, "LOOM?"}
	nodes := collectDiscoveredNodes(ctx, nodeRediscoveryTargets(), nodeDiscoveryWait, probes, func(reply discoveredNode) bool {
		if reply.ID != m.NodeID {
			return false
		}
		if reply.Proof != "" {
			proof, err := hex.DecodeString(reply.Proof)
			return err == nil && hmac.Equal(proof, nodeDiscoveryProof(n.WebKey, challenge, reply))
		}
		u, _ := url.Parse(reply.Address) // Already checked against the UDP source.
		return net.ParseIP(old.Hostname()).Equal(net.ParseIP(u.Hostname())) ||
			old.Hostname() == "localhost" && net.ParseIP(u.Hostname()).IsLoopback()
	})
	if len(nodes) == 0 {
		return nil
	}
	base, candidate, err := cleanNodeURL(nodes[0].Address)
	if err != nil {
		return nil
	}
	if old.Scheme == "https" || old.Hostname() == "localhost" {
		// Keep the configured TLS name and scheme, or the loopback alias.
		candidate.Scheme = old.Scheme
		candidate.Host = net.JoinHostPort(old.Hostname(), candidate.Port())
		base = candidate.String()
	}
	if base == old.String() {
		return nil
	}
	verified, err := nodeMaintenanceProbe(ctx, base, current.WebKey)
	if err != nil || verified.NodeID != m.NodeID || ctx.Err() != nil {
		return nil
	}
	return saveRediscoveredNode(m, current, verified)
}

func relocatedNode(n, verified *engineNode) *engineNode {
	next := *n
	if n.V1 != "" && (n.V1 == n.URL || verified.V1 != "") {
		next.V1 = verified.URL
	}
	next.URL, next.Hostname, next.Version = verified.URL, verified.Hostname, verified.Version
	next.Modules, next.Handshake = verified.Modules, verified.Handshake
	return &next
}

func encodeNodeRecord(n *engineNode) ([]byte, error) {
	sealed, err := sealEngineNode(n)
	if err != nil {
		return nil, err
	}
	plain, err := json.Marshal(sealed)
	if err != nil {
		return nil, err
	}
	return encodeMemContent(plain)
}

func saveRediscoveredNode(m RemoteMachine, old, verified *engineNode) *engineNode {
	path, key := dbPath(), machineNodePrefix+m.ID
	raw, err := getBytesErr(bkState, key)
	if err != nil {
		return nil
	}
	machines, err := getBytesErr(bkState, remoteMachinesState)
	if err != nil {
		return nil
	}
	if len(raw) == 0 {
		// Older active links can supply the credential without a separate
		// maintenance record. Ensure this machine is still paired.
		found := false
		for _, saved := range loadRemoteMachines() {
			found = found || saved.ID == m.ID && saved.NodeID == m.NodeID
		}
		if !found {
			return nil
		}
	}
	current := savedMachineNode(m)
	if current == nil || current.NodeID != old.NodeID || current.WebKey != old.WebKey {
		return nil
	}
	if current.URL != old.URL {
		return current
	}
	next := relocatedNode(current, verified)
	link, err := encodeNodeRecord(next)
	if err != nil {
		return nil
	}
	engineNodeMu.Lock()
	defer engineNodeMu.Unlock()
	activeRaw, err := getBytesErr(bkState, engineNodeState)
	if err != nil {
		return nil
	}
	var activeNext *engineNode
	var engineLink []byte
	var active engineNode
	if getStoreJSON(bkState, engineNodeState, &active) && !active.Direct && active.NodeID == m.NodeID {
		if _, err := unsealEngineNode(&active); err != nil {
			return nil
		}
		// The probe verified the saved credential at the new endpoint.
		// Preserve an explicitly configured TLS origin when the probe used HTTP.
		if active.WebKey == old.WebKey && (!strings.HasPrefix(active.URL, "https://") || strings.HasPrefix(verified.URL, "https://")) {
			activeNext = relocatedNode(&active, verified)
			engineLink, err = encodeNodeRecord(activeNext)
			if err != nil {
				return nil
			}
		}
	}
	if len(raw) == 0 && activeNext == nil {
		return nil
	}
	saved := false
	err = store.Update(path, bkState, func(b *bolt.Bucket) error {
		if !bytes.Equal(raw, b.Get([]byte(key))) ||
			!bytes.Equal(machines, b.Get([]byte(remoteMachinesState))) ||
			!bytes.Equal(activeRaw, b.Get([]byte(engineNodeState))) {
			return nil
		}
		if err := b.Put([]byte(key), link); err != nil {
			return err
		}
		if activeNext != nil {
			if err := b.Put([]byte(engineNodeState), engineLink); err != nil {
				return err
			}
		}
		saved = true
		return nil
	})
	if err != nil || !saved {
		return nil
	}
	if activeNext != nil {
		cacheEngineNodeLocked(activeNext)
	}
	return next
}

func rediscoverLinkedNode(ctx context.Context, n *engineNode) *engineNode {
	if ctx.Err() != nil || n == nil || n.Direct || n.NodeID == "" {
		return nil
	}
	for _, m := range loadRemoteMachines() {
		if m.NodeID == n.NodeID {
			return rediscoverMachineNode(ctx, m, n)
		}
	}
	return nil
}

// nodeRecoveryTransport recovers only on dial failure, before a request was
// delivered. A streaming body cannot be replayed, but later requests still
// benefit from the repaired link.
type nodeRecoveryTransport struct {
	base http.RoundTripper
	node *engineNode
}

func (t nodeRecoveryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	resp, err := base.RoundTrip(req)
	var op *net.OpError
	if !errors.As(err, &op) || op.Op != "dial" {
		return resp, err
	}
	next := rediscoverLinkedNode(req.Context(), t.node)
	if next == nil {
		return resp, err
	}
	retry := req.Clone(req.Context())
	if req.Body != nil && req.Body != http.NoBody {
		if req.GetBody == nil {
			return resp, err
		}
		var bodyErr error
		retry.Body, bodyErr = req.GetBody()
		if bodyErr != nil {
			return resp, err
		}
	}
	_, u, _ := cleanNodeURL(next.URL)
	retry.URL.Scheme, retry.URL.Host, retry.Host = u.Scheme, u.Host, u.Host
	return base.RoundTrip(retry)
}

func nodeControlClient(n *engineNode) *http.Client {
	client := *nodeClient
	client.Transport = nodeRecoveryTransport{client.Transport, n}
	return &client
}

func machineForNode(w http.ResponseWriter, r *http.Request) (RemoteMachine, bool) {
	for _, m := range loadRemoteMachines() {
		if m.ID == r.PathValue("id") {
			return m, true
		}
	}
	sendJSON(w, 404, map[string]any{"ok": false, "error": "machine not found"})
	return RemoteMachine{}, false
}

func handleMachineNode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	if !usageVaultAccess(w) {
		return
	}
	m, ok := machineForNode(w, r)
	if !ok {
		return
	}
	if r.Method == http.MethodPost {
		var req struct {
			URL    string `json:"url"`
			Key    string `json:"key"`
			Unlink bool   `json:"unlink"`
		}
		if !workspaceDecode(w, r, &req) {
			return
		}
		var err error
		if req.Unlink {
			err = putBytes(bkState, machineNodePrefix+m.ID, nil)
		} else {
			var n *engineNode
			n, err = nodeMaintenanceProbe(r.Context(), req.URL, req.Key)
			if err != nil {
				sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
				return
			}
			if !usageVaultAccess(w) {
				return
			}
			m.NodeID, m.Modules, m.Handshake = n.NodeID, n.Modules, n.Handshake
			m, err = savePairedMachine(m, n)
		}
		if err != nil {
			sendJSON(w, 500, map[string]any{"ok": false, "error": "maintenance link could not be saved"})
			return
		}
	}
	n := savedMachineNode(m)
	if n == nil {
		sendJSON(w, 200, map[string]any{"ok": true, "linked": false})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "linked": true, "url": n.URL, "hostname": n.Hostname, "version": n.Version, "role": n.Role, "modules": degradedCapabilities.Filter("machine:"+m.ID, n.Modules), "handshake": n.Handshake, "degraded": degradedCapabilities.Snapshot("machine:" + m.ID)})
}

func handleMachineNodeUpdate(w http.ResponseWriter, r *http.Request) {
	if !usageVaultAccess(w) {
		return
	}
	m, ok := machineForNode(w, r)
	if !ok {
		return
	}
	n := savedMachineNode(m)
	if n == nil || n.Direct || n.Role != "engine-node" {
		sendJSON(w, 409, map[string]any{"ok": false, "error": "link this machine's engine node first"})
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/machines/"+m.ID+"/node/update")
	if path != "" {
		forgetMachineRefresh(m.ID)
	}
	switch path {
	case "":
		if !workspaceMethod(w, r, http.MethodGet) {
			return
		}
		path = "/api/update"
	case "/ping":
		if !workspaceMethod(w, r, http.MethodGet) {
			return
		}
		path = "/api/ping"
	case "/apply":
		if !workspaceMethod(w, r, http.MethodPost) {
			return
		}
		path = "/api/update/apply"
	default:
		http.NotFound(w, r)
		return
	}
	cloned := r.Clone(r.Context())
	copied := *r.URL
	cloned.URL = &copied
	cloned.URL.Path, cloned.URL.RawPath, cloned.URL.RawQuery = path, "", "channel="+updateChannel()
	proxyEngineNode(w, cloned, n)
}
