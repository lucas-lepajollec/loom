package loom

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"slices"

	agent "github.com/lucas-lepajollec/loom/internal/loom/runtime"
	"github.com/lucas-lepajollec/loom/internal/loom/store"
	bolt "go.etcd.io/bbolt"
)

// Records contain only opaque identity and fixed operation enums, never session
// IDs, model IDs, paths, account identities, environment, or event payloads.
const agentEvidencePrefix = "agent_evidence_"
const agentEvidenceRevision = "1"

type agentEvidencePartition struct {
	Fingerprint  string                              `json:"fingerprint"`
	At           int64                               `json:"at"`
	Capabilities map[string]agent.CapabilityEvidence `json:"capabilities"`
}

func agentEvidenceFingerprint(a acpAgent, r *agent.CompatibilityRecord) string {
	transport := r.Protocol
	if a.Remote {
		transport = "acp-ssh"
		if len(a.Args) > 0 && a.Args[0] == "node-bridge" {
			transport = "acp-node"
		}
	}
	// Paths/argv participate only in an opaque digest, never the evidence record.
	// Launch environment and projected provider keys are excluded. A changed
	// version, adapter, launcher,
	// native/Loom source selection or local executable metadata starts unknown.
	stamp := ""
	if !a.Remote {
		if info, err := os.Stat(r.Executable); err == nil {
			stamp = fmt.Sprintf("%d:%d", info.Size(), info.ModTime().UnixNano())
		}
	}
	pkg := r.AdapterPackage
	if r.Protocol != "acp" {
		pkg = ""
	}
	installation := ""
	if a.Remote {
		for _, machine := range loadRemoteMachines() {
			if machine.ID != a.Machine {
				continue
			}
			binary := usageHarnessID(a)
			if spec, ok := harnessInspectSpec(binary); ok {
				binary = spec.Binary
			}
			var tool RemoteTool
			for _, candidate := range machine.Tools {
				if candidate.ID == binary {
					tool = candidate
					break
				}
			}
			metadata, _ := json.Marshal([]any{machine.NodeID, machine.OS, tool.Path, tool.Version})
			installation = string(metadata)
			break
		}
	}
	raw, _ := json.Marshal([]any{agentEvidenceRevision, runtime.GOOS, a.ID, a.Machine, transport, a.Command, a.Args, r.Version, r.AdapterVersion, pkg, r.Executable, stamp, installation, modelSinkEnabled(a.ID)})
	return hashWebKey(string(raw))
}

func evidenceKey(id string) string { return agentEvidencePrefix + hashWebKey(id) }

// readAgentEvidence is read-only and never upgrades implementation declarations
// or accepted-version metadata to installation observations.
func readAgentEvidence(a acpAgent, r *agent.CompatibilityRecord) {
	r.Evidence = nil
	r.Fingerprint = agentEvidenceFingerprint(a, r)
	var partitions []agentEvidencePartition
	if !getStoreJSON(bkCapabilities, evidenceKey(a.ID), &partitions) {
		return
	}
	for _, partition := range partitions {
		if partition.Fingerprint != r.Fingerprint {
			continue
		}
		r.Evidence = partition.Capabilities
		for name, evidence := range r.Evidence {
			if capabilityDisabled("agent:"+a.ID, name) {
				evidence.Health = "degraded"
			}
			r.Evidence[name] = evidence
		}
		return
	}
}

// updateAgentEvidence serializes concurrent sessions/probes with the existing
// bbolt transaction and vault encoding, rather than a new in-memory ledger.
func updateAgentEvidence(a acpAgent, r *agent.CompatibilityRecord, at int64, level, check string, names []string, ok bool) {
	if r == nil || at == 0 || len(names) == 0 || !(check == "acp.announcement" || check == "session.operation" || len(check) > 6 && check[:6] == "probe." && knownEvidenceCapability(check[6:])) {
		return
	}
	fingerprint := agentEvidenceFingerprint(a, r)
	key := evidenceKey(a.ID)
	// Resolve vault state before opening the transaction: ReadConfig itself uses
	// this store, whose short-lived handle is deliberately serialized.
	encrypted := memEncActive()
	var dek []byte
	if encrypted {
		var err error
		dek, err = currentDEK()
		if err != nil {
			return
		}
	}
	_ = store.Update(dbPath(), bkCapabilities, func(b *bolt.Bucket) error {
		var partitions []agentEvidencePartition
		if raw := b.Get([]byte(key)); len(raw) > 0 {
			plain, err := decodeMemContent(raw)
			if err != nil {
				return err
			}
			if err := json.Unmarshal(plain, &partitions); err != nil {
				return err
			}
		} else {
			count := 0
			c := b.Cursor()
			for k, _ := c.First(); k != nil; k, _ = c.Next() {
				if len(k) >= len(agentEvidencePrefix) && string(k[:len(agentEvidencePrefix)]) == agentEvidencePrefix {
					count++
				}
			}
			if count >= 512 {
				return nil
			}
		}
		index := slices.IndexFunc(partitions, func(p agentEvidencePartition) bool { return p.Fingerprint == fingerprint })
		if index < 0 {
			if len(partitions) >= 4 {
				partitions = partitions[1:]
			}
			partitions = append(partitions, agentEvidencePartition{Fingerprint: fingerprint, Capabilities: map[string]agent.CapabilityEvidence{}})
			index = len(partitions) - 1
		}
		partition := &partitions[index]
		partition.At = at
		for _, name := range names {
			if !knownEvidenceCapability(name) {
				continue
			}
			evidence, exists := partition.Capabilities[name]
			if !exists && len(partition.Capabilities) >= 64 {
				continue
			}
			if evidence.Health == "" {
				evidence.Health = "unknown"
			}
			if !ok {
				evidence.Health = "degraded"
			} else {
				var observation **agent.CapabilityObservation
				switch level {
				case "discovered":
					observation = &evidence.Discovered
				case "protocol_verified":
					observation = &evidence.ProtocolVerified
				case "observed_working":
					observation = &evidence.ObservedWorking
				default:
					continue
				}
				count := uint64(1)
				if *observation != nil {
					count += (*observation).Count
				}
				*observation = &agent.CapabilityObservation{Check: check, At: at, Count: count}
				if level != "discovered" {
					evidence.Health = "ok"
				}
			}
			partition.Capabilities[name] = evidence
		}
		plain, err := json.Marshal(partitions)
		if err != nil {
			return err
		}
		encoded := plain
		if encrypted {
			encoded, err = encPage(dek, plain)
			if err != nil {
				return err
			}
		}
		return b.Put([]byte(key), encoded)
	})
}

func recordProbeEvidence(a acpAgent, p *acpProbe) {
	r := p.Compatibility
	if r == nil {
		return
	}
	// Only real announcements are discovery. Native feature tables and the
	// synthetic loadSession projection in native probes are not announcements.
	if r.Protocol == "acp" {
		discovered := []string{}
		if resume, _ := p.Caps["loadSession"].(bool); resume {
			discovered = append(discovered, "resume")
		}
		sc, _ := p.Caps["sessionCapabilities"].(map[string]any)
		if acpCapabilityAvailable(sc["list"]) {
			discovered = append(discovered, "session-list")
		}

		updateAgentEvidence(a, r, p.At, "discovered", "acp.announcement", discovered, true)
	}
	checks := append(slices.Clone(p.CapabilityChecks), r.CapabilityChecks...)
	// A failure wins over a conflicting success from another projection.
	results := map[string]bool{}
	for _, c := range checks {
		old, exists := results[c.Capability]
		results[c.Capability] = c.OK && (!exists || old)
	}
	for name, ok := range results {
		if ok {
			updateAgentEvidence(a, r, p.At, "discovered", "probe."+name, []string{name}, true)
		}
		updateAgentEvidence(a, r, p.At, "protocol_verified", "probe."+name, []string{name}, ok)
	}
	readAgentEvidence(a, r)
}

func knownEvidenceCapability(name string) bool {
	return slices.Contains([]string{"chat", "stream", "cancel", "tools", "connect", "models", "reasoning-effort", "approvals", "user-input", "elicitation", "plan", "usage", "quota", "workdir", "resume", "session-list", "history-import", "mcp", "mcp-gateway", "skills", "memory", "terminal", "remote", "native-events", "raw-events", "reasoning-summary", "sandbox", "additional-dirs"}, name)
}

func recordSessionEvidence(a acpAgent, r *agent.CompatibilityRecord, names []string, at int64) {
	updateAgentEvidence(a, r, at, "discovered", "session.operation", names, true)
	updateAgentEvidence(a, r, at, "observed_working", "session.operation", names, true)
	for _, name := range names {
		observeCapability("agent:"+a.ID, name, true, "session_succeeded")
	}
}
