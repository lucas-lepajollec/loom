package loom

import "errors"

// Policies refer to upstream protections, never a sandbox implemented by
// Loom's delegated ACP file API. Only known launchers with a tested native mode
// are advertised. Strict read confinement is deliberately absent for Codex:
// its workspace-write mode still allows reads outside the workspace.
func harnessFilesystemPolicies(agent acpAgent) []string {
	policies := []string{"native"}
	if agent.ID == "codex" || agent.Machine != "" && usageHarnessID(agent) == "codex" {
		policies = append(policies, "workspace-write", "full-access")
	}
	return policies
}
func harnessFilesystemMode(agent acpAgent, policy string) (string, error) {
	if policy == "" || policy == "native" {
		return "", nil
	}
	allowed := false
	for _, candidate := range harnessFilesystemPolicies(agent) {
		if candidate == policy {
			allowed = true
		}
	}
	if !allowed {
		return "", errors.New("this filesystem protection is unavailable in the native harness")
	}
	switch policy {
	case "workspace-write":
		return "workspace-write", nil
	case "full-access":
		return "agent-full-access", nil
	}
	return "", errors.New("invalid filesystem policy")
}
