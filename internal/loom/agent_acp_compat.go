package loom

import (
	"errors"
	"fmt"
	"strings"

	"github.com/lucas-lepajollec/loom/internal/loom/harness"
	agent "github.com/lucas-lepajollec/loom/internal/loom/runtime"
	"github.com/lucas-lepajollec/loom/internal/loom/runtime/acp"
)

func acpCompatibility(a acpAgent, info map[string]any, caps []string) *agent.CompatibilityRecord {
	packageName, pin := a.Command, ""
	for _, arg := range a.Args {
		for _, word := range strings.Fields(arg) {
			if strings.HasPrefix(word, "@agentclientprotocol/") || strings.HasPrefix(word, "pi-acp@") {
				if i := strings.LastIndex(word, "@"); i > 0 {
					packageName, pin = word[:i], word[i+1:]
				}
			}
		}
	}
	tested := []string{}
	switch usageHarnessID(a) {
	case "claude-code":
		packageName = "@agentclientprotocol/claude-agent-acp"
		tested = harness.TestedVersions("claude-acp")
	case "opencode":
		packageName = "opencode"
		tested = harness.TestedVersions("opencode")
	case "hermes":
		packageName = "hermes-agent"
		tested = harness.TestedVersions("hermes")
	case "deepseek-harness":
		packageName, pin = "@deepseek-ai/dsh", "0.2.0-rc.2"
		tested = harness.TestedVersions("deepseek-harness")
	case "openclaw":
		packageName = "openclaw"
		tested = harness.TestedVersions("openclaw")
	case "antigravity":
		packageName, pin = "loom agy-acp", Version
	}
	source := "loom"
	if a.RegistryID != "" {
		packageName, pin = a.RegistryPackage, a.RegistryVersion
		tested, source = []string{a.RegistryVersion}, "registry"
	}
	version, _ := info["version"].(string)
	path, _ := lifecycleLookPath(a.Command)
	r := agent.NewCompatibilityRecord(agent.CompatibilityRecord{Runtime: a.ID, Executable: path, Version: version, AgentVersion: version, Protocol: "acp", AdapterVersion: pin, AdapterPackage: packageName, TestedVersions: tested, TestedVersionSource: source, Capabilities: caps})
	if version != "" {
		matched := false
		for _, v := range tested {
			if version == v {
				matched = true
			}
		}
		if !matched {
			r.Warning = fmt.Sprintf("%s handshake version %s has not been tested; ACP compatibility is unverified", a.Name, version)
		}
	}
	if deepseekACPAgent(a) {
		r.AdapterPackage = "@deepseek-ai/dsh"
		r.TestedVersions = harness.TestedVersions("deepseek-harness")
		r.TestedVersion, r.TestedVersionSource = harness.LatestTestedVersion("deepseek-harness"), "loom"
		if len(r.TestedVersions) == 0 {
			r.Warning = "not yet verified with Loom"
		}
	}
	if a.ID == "hermes" || a.ID == "openclaw" {
		if len(harness.TestedVersions(a.ID)) == 0 {
			r.Warning = "not yet verified with Loom"
		}
	}
	return r
}
func recordACPCompatibility(a acpAgent, info map[string]any, caps []string) *agent.CompatibilityRecord {
	r := acpCompatibility(a, info, caps)
	_ = putStoreJSON(bkState, "agent_compat_"+a.ID, r)
	return r
}
func acpClientCapabilities(files, interactive bool) map[string]any {
	return acp.ClientCapabilities(files, interactive)
}

func claudeACPAgent(a acpAgent) bool {
	if usageHarnessID(a) == "claude-code" || a.ID == "claude-code" {
		return true
	}
	for _, arg := range a.Args {
		if strings.Contains(arg, "@agentclientprotocol/claude-agent-acp@") {
			return true
		}
	}
	return false
}

// Other ACP agents keep their historical sanitized RPC errors. Claude's
// versioned failure surface explicitly supplies user-facing native messages.
func claudeACPError(a acpAgent, err error, fallback string) error {
	var rpc *acpRPCError
	if claudeACPAgent(a) && errors.As(err, &rpc) {
		return errors.New(rpc.Message)
	}
	return errors.New(fallback)
}
func (p *acpBinding) publishClaudeRPCFailure(a acpAgent, err error) {
	var rpc *acpRPCError
	if claudeACPAgent(a) && errors.As(err, &rpc) {
		p.publish(DiscussionEvent{"type": "error", "error": rpc.Message, "agent_event": AgentEvent{Type: "error", Runtime: a.ID, Error: rpc.Message, Raw: agent.BoundedJSON(rpc.Raw)}})
	}
}

// Native transports share version provenance and drift warnings. ACP retains
// handshake-specific warnings and launcher-pin interpretation above.
func harnessCompatibility(a acpAgent, protocol, executable, version string, caps []string, pkg, source, tested string) *agent.CompatibilityRecord {
	r := agent.NewCompatibilityRecord(agent.CompatibilityRecord{Runtime: a.ID, Executable: executable, Version: version, Protocol: protocol, AdapterVersion: agentAdapterVersion, AdapterPackage: pkg, TestedVersion: tested, TestedVersions: harness.TestedVersions(a.ID), TestedVersionSource: source, Capabilities: caps})
	if version != "" && !harness.VersionTested(a.ID, version) {
		r.Warning = fmt.Sprintf("%s %s differs from tested %s; protocol compatibility is unverified", a.Name, version, r.TestedVersion)
	}
	return r
}
