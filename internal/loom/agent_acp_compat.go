package loom

import (
	"errors"
	"fmt"
	"strings"

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
		tested = []string{"0.88.0"}
	case "opencode":
		packageName = "opencode"
		tested = []string{"1.18.33"}
	case "hermes":
		packageName = "hermes-agent"
	case "openclaw":
		packageName = "openclaw"
	case "antigravity":
		packageName, pin = "loom agy-acp", Version
	}
	version, _ := info["version"].(string)
	path, _ := lifecycleLookPath(a.Command)
	r := &agent.CompatibilityRecord{Runtime: a.ID, Executable: path, Version: version, AgentVersion: version, Protocol: "acp", AdapterVersion: pin, AdapterPackage: packageName, TestedVersions: tested, Capabilities: caps}
	if len(tested) > 0 {
		r.TestedVersion = tested[0]
	}
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
