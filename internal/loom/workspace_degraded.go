package loom

import (
	"slices"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/capability"
	"github.com/lucas-lepajollec/loom/internal/loom/events"
)

var degradedCapabilities = &capability.State{}

func currentEngineCapabilityOwner() string {
	return engineCapabilityOwner(currentEngineNode())
}
func engineCapabilityOwner(n *engineNode) string {
	if n != nil {
		return "engine:" + hashWebKey(n.URL + "|" + n.V1)[:16]
	}
	return "engine:local"
}
func observeEngineReachability(owner string, ok bool) {
	for _, feature := range []string{"chat", "stream"} {
		observeCapability(owner, feature, ok, "engine_unreachable")
	}
}

func observeCapability(owner, feature string, ok bool, reason string) {
	if reason == "" {
		reason = "probe_failed"
	}
	if degradedCapabilities.Observe(owner, feature, ok, reason, time.Now()) {
		kind, status := events.CapabilityDegraded, "degraded"
		if ok {
			kind, status = events.CapabilityRestored, "restored"
		}
		workspaceSessions.events.Publish(events.Event{Type: kind, Status: status, Owner: owner, Capability: feature, Reason: reason})
	}
}
func capabilityDisabled(owner, feature string) bool {
	for _, d := range degradedCapabilities.Snapshot(owner) {
		if d.Capability == feature {
			return true
		}
	}
	return false
}

func degradedDescriptor(d RuntimeDescriptor) RuntimeDescriptor {
	owner := "runtime:" + d.ID
	if d.Kind == "local" {
		owner = currentEngineCapabilityOwner()
	}
	d.Degraded = degradedCapabilities.Snapshot(owner)
	d.Capabilities = degradedCapabilities.Filter(owner, d.Capabilities)
	return d
}

func projectMachineCapabilities(m RemoteMachine) RemoteMachine {
	m.Degraded = degradedCapabilities.Snapshot("machine:" + m.ID)
	m.Modules = slices.DeleteFunc(slices.Clone(m.Modules), func(module string) bool {
		return module != "engine" && module != "voice" && module != "terminal" && module != "observe"
	})
	m.Modules = degradedCapabilities.Filter("machine:"+m.ID, m.Modules)
	return m
}
func observeMachineProbe(m RemoteMachine, info *engineNode, err error) {
	for _, feature := range m.Modules {
		ok, reason := err == nil, "machine_unreachable"
		if err == nil && !slices.Contains(info.Modules, feature) {
			ok, reason = false, "module_unavailable"
		}
		if err == nil && info.Handshake != 0 && info.Handshake != nodeHandshake {
			ok, reason = false, "node_incompatible"
		}
		observeCapability("machine:"+m.ID, feature, ok, reason)
	}
}
