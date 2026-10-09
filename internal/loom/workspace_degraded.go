package loom

import (
	"slices"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/capability"
	"github.com/lucas-lepajollec/loom/internal/loom/events"
	"github.com/lucas-lepajollec/loom/internal/loom/policy"
	agent "github.com/lucas-lepajollec/loom/internal/loom/runtime"
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
func observeAgentProbe(id string, p acpProbe) {
	owner := "agent:" + id
	if p.Error != "" && len(p.CapabilityChecks) == 0 {
		// A catalog handshake failure invalidates discovered catalog/history
		// operations, not the installed agent's chat/tool protocol.
		for _, c := range []string{"models", "session-list", "history-import", "resume"} {
			observeCapability(owner, c, false, "handshake_failed")
		}
		return
	}
	checks := slices.Clone(p.CapabilityChecks)
	if p.Compatibility != nil {
		checks = append(checks, p.Compatibility.CapabilityChecks...)
	}
	failed := map[string]bool{}
	for _, c := range checks {
		if !c.OK {
			failed[c.Capability] = true
		}
	}
	if p.Error == "" {
		for _, d := range degradedCapabilities.Snapshot(owner) {
			if !failed[d.Capability] {
				observeCapability(owner, d.Capability, true, "probe_succeeded")
			}
		}
	}
	for _, c := range checks {
		observeCapability(owner, c.Capability, c.OK, c.Reason)
	}
}
func degradedDescriptor(d RuntimeDescriptor) RuntimeDescriptor {
	owner := "agent:" + d.ID
	if d.Kind == "local" {
		owner = currentEngineCapabilityOwner()
	}
	d.Degraded = degradedCapabilities.Snapshot(owner)
	d.Capabilities = degradedCapabilities.Filter(owner, d.Capabilities)
	if d.Features != nil {
		f := *d.Features
		applyDegradedFeatures(&f, d.Degraded)
		d.Features = &f
	}
	if d.Compatibility != nil {
		copy := *d.Compatibility
		copy.Capabilities = slices.Clone(d.Capabilities)
		d.Compatibility = &copy
	}
	return d
}
func applyDegradedFeatures(f *agent.HarnessFeatures, failed []capability.Degraded) {
	fields := map[string]*bool{"models": &f.Models, "resume": &f.Resume, "session-list": &f.SessionList, "history-import": &f.HistoryImport, "approvals": &f.Approvals, "user-input": &f.Questions, "elicitation": &f.Forms, "mcp": &f.MCPSelection, "mcp-gateway": &f.MCPGateway, "terminal": &f.Terminal, "reasoning-effort": &f.Effort, "plan": &f.Plan, "quota": &f.Quota, "usage": &f.Usage, "memory": &f.Memory, "skills": &f.Skills}
	fields["workdir"], fields["remote"], fields["sandbox"], fields["additional-dirs"] = &f.Workdir, &f.Remote, &f.Sandbox, &f.AdditionalDirs
	for _, d := range failed {
		if field := fields[d.Capability]; field != nil {
			*field = false
		}
	}
	options := make([]string, 0, len(f.ConfigOptions))
	for _, id := range f.ConfigOptions {
		if id == "reasoning_effort" && !f.Effort || id == "model" && !f.Models || (id == "sandbox" || id == "sandbox_mode") && !f.Sandbox {
			continue
		}
		options = append(options, id)
	}
	f.ConfigOptions = options
}

// cachedAgentProbe avoids transport detection or native CLI execution in Doctor.
func cachedAgentProbe(id string) (acpProbe, bool) {
	var p acpProbe
	ok := getStoreJSON(bkState, acpProbeKey+id, &p) && p.At > 0
	return p, ok
}
func projectMachineCapabilities(m RemoteMachine) RemoteMachine {
	m.Degraded = degradedCapabilities.Snapshot("machine:" + m.ID)
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
func (p *acpBinding) authorizeFile(path, subject string) error {
	in := p.policyInput(subject, policy.Allow)
	in.Path = path
	p.mu.Lock()
	ctx := p.ctx
	p.mu.Unlock()
	if ctx == nil {
		return errACPClosed
	}
	m := p.manager
	if m == nil {
		m = workspaceSessions
	}
	return m.authorizePolicy(ctx, in, false)
}
