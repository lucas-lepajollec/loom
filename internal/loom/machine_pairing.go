package loom

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/store"
	bolt "go.etcd.io/bbolt"
)

// Keep exactly the existing maintenance credential record/encryption, without
// selecting an engine. SSH migrations retain identity and replace launch transport.
func savePairedMachine(m RemoteMachine, n *engineNode) (RemoteMachine, error) {
	return savePairedMachineMode(m, n, false)
}

func savePairedMachineMode(m RemoteMachine, n *engineNode, migrate bool) (RemoteMachine, error) {
	linkJSON, err := json.Marshal(n)
	if err != nil {
		return m, err
	}
	link, err := encodeMemContent(linkJSON)
	if err != nil {
		return m, err
	}
	create := m.ID == ""
	if create {
		id, err := newNodeToken()
		if err != nil {
			return m, err
		}
		m.ID = "node-" + id[:16]
	}
	// Encrypt outside the store lock (vault helpers also read the store). Retry
	// on a concurrent machine edit; write the public machine and secret together.
	for attempt := 0; attempt < 5; attempt++ {
		raw, err := getBytesErr(bkState, remoteMachinesState)
		if err != nil {
			return m, err
		}
		machines := []RemoteMachine{}
		if len(raw) != 0 {
			plain, err := decodeMemContent(raw)
			if err != nil {
				return m, err
			}
			if err := json.Unmarshal(plain, &machines); err != nil {
				return m, err
			}
		}
		result := m
		index := -1
		for i, old := range machines {
			if old.ID == m.ID || (create && n.NodeID != "" && old.NodeID == n.NodeID) {
				index = i
				break
			}
		}
		if index < 0 && create {
			for i, old := range machines {
				if old.Host != m.Host || m.Host == "" {
					continue
				}
				existing := savedMachineNode(old)
				if existing == nil || existing.URL == n.URL {
					index = i
					break
				}
			}
		}
		if index >= 0 {
			result = machines[index]
			if create && result.User != "" {
				migrate = true
			}
			result.NodeID, result.Modules, result.Handshake = n.NodeID, n.Modules, n.Handshake
			machines[index] = result
		} else {
			result.NodeID, result.Modules, result.Handshake = n.NodeID, n.Modules, n.Handshake
			machines = append(machines, result)
		}
		if migrate && index < 0 {
			return m, errors.New("machine removed during migration")
		}
		var agents []acpAgent
		var agentRaw, agentEncoded []byte
		if migrate {
			for i, other := range machines {
				if i != index && other.NodeID == n.NodeID {
					return m, errors.New("node is already linked to a different machine")
				}
			}
			result.User, result.Port = "", 0
			u, _ := url.Parse(n.URL)
			result.Host, result.Hostname = u.Hostname(), n.Hostname
			machines[index] = result
			agentRaw, err = getBytesErr(bkState, acpCustomState)
			if err != nil {
				return m, err
			}
			if len(agentRaw) != 0 {
				plain, err := decodeMemContent(agentRaw)
				if err != nil {
					return m, err
				}
				if err := json.Unmarshal(plain, &agents); err != nil {
					return m, err
				}
			}
			for i, agent := range agents {
				if agent.Machine != result.ID {
					continue
				}
				harness := strings.TrimPrefix(agent.ID, "custom-"+result.ID+"-")
				replacement, err := nodeAgent(result, harness)
				if err != nil {
					return m, err
				}
				agents[i].Command, agents[i].Args, agents[i].Detect = replacement.Command, replacement.Args, nil
				agents[i].RemoteHome = result.Home
			}
			plain, err := json.Marshal(agents)
			if err != nil {
				return m, err
			}
			agentEncoded, err = encodeMemContent(plain)
			if err != nil {
				return m, err
			}
		}
		plain, err := json.Marshal(machines)
		if err != nil {
			return m, err
		}
		encoded, err := encodeMemContent(plain)
		if err != nil {
			return m, err
		}
		changed := false
		err = store.Update(dbPath(), bkState, func(b *bolt.Bucket) error {
			if !bytes.Equal(raw, b.Get([]byte(remoteMachinesState))) || migrate && !bytes.Equal(agentRaw, b.Get([]byte(acpCustomState))) {
				changed = true
				return nil
			}
			if migrate {
				if err := b.Put([]byte(acpCustomState), agentEncoded); err != nil {
					return err
				}
			}
			if err := b.Put([]byte(machineNodePrefix+result.ID), link); err != nil {
				return err
			}
			return b.Put([]byte(remoteMachinesState), encoded)
		})
		if err != nil {
			return m, err
		}
		if !changed {
			if migrate {
				for _, agent := range agents {
					if agent.Machine == result.ID {
						registeredRuntimes.upsert(&acpAdapter{agent: agent})
					}
				}
			}
			return result, nil
		}
	}
	return m, errors.New("machines changed concurrently; retry pairing")
}

func handleMachinesPair(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !workspaceMethod(w, r, http.MethodPost) || !usageVaultAccess(w) {
		return
	}
	var req machinePairInput
	if !workspaceDecode(w, r, &req) {
		return
	}
	handleMachinePairInput(w, r, req)
}

type machinePairInput struct {
	Address   string `json:"address"`
	Code      string `json:"code"`
	Force     bool   `json:"force,omitempty"`
	MachineID string `json:"machine_id,omitempty"`
}

func handleMachinePairInput(w http.ResponseWriter, r *http.Request, req machinePairInput) {
	var target RemoteMachine
	if req.MachineID != "" {
		for _, m := range loadRemoteMachines() {
			if m.ID == req.MachineID {
				target = m
				break
			}
		}
		if target.ID == "" {
			sendJSON(w, 404, map[string]any{"ok": false, "error": "machine not found"})
			return
		}
	}
	base, u, err := cleanNodeURL(req.Address)
	if err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if len(req.Code) > 32 || strings.TrimSpace(req.Code) == "" {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "pairing code required"})
		return
	}
	id, err := loomMachineID()
	if err != nil {
		sendJSON(w, 500, map[string]any{"ok": false, "error": "main identity could not be saved"})
		return
	}
	name, _ := os.Hostname()
	pair := nodePairRequest{Code: req.Code, Force: req.Force}
	pair.Main.ID, pair.Main.Name, pair.Main.Version = id, name, Version
	body, _ := json.Marshal(pair)
	request, _ := http.NewRequestWithContext(r.Context(), http.MethodPost, base+"/api/node/pair", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	resp, err := nodeClient.Do(request)
	if err != nil {
		sendJSON(w, 502, map[string]any{"ok": false, "error_code": "unreachable", "error": "engine node unreachable; check its address and listener"})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// Never echo an arbitrary upstream body: it might contain credentials.
		status, code, message := 502, "node_error", "unexpected response from engine node"
		var owner struct {
			Main pairedMain `json:"main"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 16<<10)).Decode(&owner)
		switch resp.StatusCode {
		case 401, 400:
			status, code, message = 401, "invalid_code", "invalid or expired pairing code"
		case 409:
			status, code, message = 409, "already_paired", "node already paired to another main Loom"
			if validPairLabel(owner.Main.Name) {
				message += ": " + owner.Main.Name
			}
		case 429:
			status, code, message = 429, "rate_limited", "too many pairing attempts; wait 10 minutes"
		case 404:
			status, code, message = 409, "pairing_unsupported", "this node does not support pairing; update it or use its machine token"
		}
		sendJSON(w, status, map[string]any{"ok": false, "error_code": code, "error": message})
		return
	}
	var exchange nodePairExchange
	if json.NewDecoder(io.LimitReader(resp.Body, 16<<10)).Decode(&exchange) != nil {
		sendJSON(w, 502, map[string]any{"ok": false, "error": "invalid node pairing response"})
		return
	}
	if exchange.Node.Handshake != nodeHandshake {
		message := "node handshake mismatch: expected major 1"
		if err := checkNodeHandshake(exchange.Node.Handshake); err != nil {
			message = err.Error()
		}
		sendJSON(w, 409, map[string]any{"ok": false, "error_code": "handshake_mismatch", "error": message})
		return
	}
	if !validPairLabel(exchange.Node.ID) || !validPairLabel(exchange.Node.Name) || exchange.Node.Role != "engine-node" ||
		!validNodeCredential(exchange.MachineToken) || !validNodeCredential(exchange.InferenceKey) || !hasEngineModule(exchange.Node.Modules) {
		sendJSON(w, 502, map[string]any{"ok": false, "error": "invalid node pairing response"})
		return
	}
	if !usageVaultAccess(w) {
		return
	}
	n := &engineNode{URL: base, V1: base, WebKey: exchange.MachineToken, APIKey: exchange.InferenceKey,
		NodeID: exchange.Node.ID, Hostname: exchange.Node.Name, Version: exchange.Node.Version,
		Role: exchange.Node.Role, Modules: exchange.Node.Modules, Handshake: exchange.Node.Handshake, LinkedAt: time.Now().UnixMilli()}
	m := RemoteMachine{Name: n.Hostname, Host: u.Hostname(), Hostname: n.Hostname, NodeID: n.NodeID, Modules: n.Modules, Handshake: n.Handshake}
	if target.ID != "" {
		m = target
	}
	m, err = savePairedMachineMode(m, n, target.ID != "")
	if err != nil {
		sendJSON(w, 500, map[string]any{"ok": false, "error": "machine maintenance link could not be saved: " + err.Error() + "; run loom node pair to retry"})
		return
	}
	if target.ID == "" && hasNodeModule(m.Modules, "harness") {
		if checked, checkErr := refreshNodeMachine(r.Context(), m); checkErr == nil {
			machines := loadRemoteMachines()
			for i := range machines {
				if machines[i].ID == m.ID {
					machines[i] = checked
				}
			}
			if putStoreJSON(bkState, remoteMachinesState, machines) == nil {
				m = checked
			}
		}
	}
	sendJSON(w, 200, map[string]any{"ok": true, "machine": m})
}

func validNodeCredential(key string) bool {
	return key != "" && len(key) <= 4096 && !strings.ContainsAny(key, "\r\n\x00")
}

func hasEngineModule(modules []string) bool {
	for _, module := range modules {
		if module == "engine" {
			return true
		}
	}
	return false
}

func machineNodeMatches(m RemoteMachine, node discoveredNode) bool {
	n := savedMachineNode(m)
	if n == nil {
		return false
	}
	if node.ID == n.NodeID || node.ID == m.NodeID {
		return true
	}
	a, err := url.Parse(n.URL)
	b, otherErr := url.Parse(node.Address)
	return err == nil && otherErr == nil && a.Host == b.Host
}
