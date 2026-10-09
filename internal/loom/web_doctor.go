package loom

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/capability"
	"github.com/lucas-lepajollec/loom/internal/loom/doctor"
)

var doctorSlots = make(chan struct{}, 2)

func doctorObservationAllowed(ctx context.Context) bool {
	return usageVaultAccessStream() && (ctx.Err() == nil || errors.Is(ctx.Err(), context.DeadlineExceeded))
}

func doctorCheck(status, detail, href string) doctor.Check {
	c := doctor.Check{Status: status, Detail: detail}
	if status != "ok" && href != "" {
		c.Fix = &doctor.Fix{Label: "open_settings", Href: href}
	}
	return c
}
func doctorJobs() []doctor.Job {
	jobs := []doctor.Job{
		{ID: "core.version", Area: "core", Run: func(context.Context) doctor.Check { return doctorCheck("ok", Version+"; "+updateChannel(), "") }},
		{ID: "core.data_writable", Area: "core", Run: func(context.Context) doctor.Check {
			if !doctor.Writable(LoomHome()) {
				return doctorCheck("fail", "data_not_writable", "#/settings")
			}
			return doctorCheck("ok", "data_writable", "")
		}},
		{ID: "core.disk_space", Area: "core", Run: func(context.Context) doctor.Check {
			available := diskFreeAt(LoomHome())
			if available < 0 {
				return doctorCheck("skip", "disk_space_unknown", "")
			}
			if available < 512<<20 {
				return doctorCheck("warn", "disk_space_low", "#/machines")
			}
			return doctorCheck("ok", fmt.Sprintf("available_bytes:%d", available), "")
		}},
		{ID: "security.vault", Area: "security", Run: func(context.Context) doctor.Check {
			if memEncActive() && !memUnlocked() {
				return doctorCheck("fail", "vault_locked", "#/settings")
			}
			if !vaultExists() {
				return doctorCheck("skip", "vault_not_configured", "")
			}
			return doctorCheck("ok", "vault_unlocked", "")
		}},
		{ID: "brain.primary_writable", Area: "brain", Run: func(context.Context) doctor.Check {
			if !usageVaultAccessStream() {
				return doctorCheck("skip", "vault_locked", "")
			}
			sources, err := theBrain().storage.LoadSources()
			if err != nil {
				return doctorCheck("warn", "brain_config_unavailable", "#/brain")
			}
			for _, s := range sources {
				if s.Primary {
					if s.Permission != "write" || !doctor.Writable(s.Path) {
						return doctorCheck("fail", "brain_not_writable", "#/brain")
					}
					return doctorCheck("ok", "brain_writable", "")
				}
			}
			return doctorCheck("skip", "brain_primary_missing", "#/brain")
		}},
		{ID: "engine.reachable", Area: "engine", Run: doctorEngine},
		{ID: "security.password", Area: "security", Run: func(context.Context) doctor.Check {
			p, _, err := readWebPassword()
			if err != nil {
				return doctorCheck("fail", "password_unreadable", "#/settings")
			}
			if p == nil {
				return doctorCheck("warn", "password_missing", "#/settings")
			}
			return doctorCheck("ok", "password_set", "")
		}},
		{ID: "security.lan", Area: "security", Run: func(context.Context) doctor.Check {
			p, _, err := readWebPassword()
			exposed := !isLoopbackHost(webHost()) || webBound.host != "" && !isLoopbackHost(webBound.host)
			if exposed && (err != nil || p == nil) {
				return doctorCheck("fail", "lan_without_password", "#/settings")
			}
			return doctorCheck("ok", "lan_protected_or_loopback", "")
		}},
		{ID: "security.push_https", Area: "security", Run: func(context.Context) doctor.Check {
			cfg, err := notificationConfig()
			if err != nil {
				return doctorCheck("warn", "notification_config_unavailable", "#/settings")
			}
			if !cfg.Push.Enabled {
				return doctorCheck("skip", "push_disabled", "")
			}
			u, err := url.Parse(cfg.PublicBaseURL)
			if err != nil || u.Scheme != "https" {
				return doctorCheck("fail", "push_requires_https", "#/settings")
			}
			return doctorCheck("ok", "push_https", "")
		}},
		{ID: "security.tokens_age", Area: "security", Run: func(context.Context) doctor.Check {
			return doctorTokenAges()
		}},
	}
	if !usageVaultAccessStream() {
		return jobs
	}
	for _, m := range loadRemoteMachines() {
		if savedMachineNode(m) == nil {
			continue
		}
		jobs = append(jobs, doctor.Job{ID: "machine." + m.ID, Area: "machines", Run: func(ctx context.Context) doctor.Check {
			n := savedMachineNode(m)
			if n == nil {
				return doctorCheck("skip", "machine_unlinked", "#/machines")
			}
			info, err := nodeMaintenanceProbe(ctx, n.URL, n.WebKey)
			if doctorObservationAllowed(ctx) {
				observeMachineProbe(m, info, err)
			}
			if err != nil {
				return doctorCheck("fail", "machine_unreachable_or_incompatible", "#/machines/"+url.PathEscape(m.ID))
			}
			if info.Version != Version {
				return doctorCheck("warn", "node_outdated", "#/machines/"+url.PathEscape(m.ID))
			}
			return doctorCheck("ok", "node_reachable_version_match", "")
		}})
	}
	for _, adapter := range registeredRuntimes.Adapters() {
		a, ok := adapter.(*acpAdapter)
		if !ok || !harnessConnected(a.agent) {
			continue
		}
		identity := a.agent
		jobs = append(jobs, doctor.Job{ID: "agent." + identity.ID, Area: "agents", Run: func(ctx context.Context) doctor.Check {
			href := "#/agents/" + url.PathEscape(identity.ID)
			p, exists := cachedAgentProbe(identity.ID)
			compat := p.Compatibility
			if compat == nil {
				var c runtimeCompatibilityRecord
				if getStoreJSON(bkState, "agent_compat_"+identity.ID, &c) {
					compat = &c
				}
			}
			if !identity.Remote {
				binaries := append([]string{}, identity.Detect...)
				binaries = append(binaries, identity.Command)
				// A cached native handshake identifies the actual executable;
				// requiring its unused ACP launcher would report a false failure.
				if compat != nil && compat.Protocol != "acp" && compat.Executable != "" {
					binaries = []string{compat.Executable}
				}
				for _, binary := range binaries {
					if _, err := lifecycleLookPath(binary); err != nil {
						return doctorCheck("fail", "agent_not_installed", href)
					}
				}
			}
			if !exists {
				return doctorCheck("skip", "handshake_not_observed", href)
			}
			if doctorObservationAllowed(ctx) {
				p.Compatibility = compat
				observeAgentProbe(identity.ID, p)
			}
			if p.Error != "" && len(p.CapabilityChecks) == 0 {
				return doctorCheck("fail", "handshake_failed", href)
			}
			checks := append([]capability.Probe{}, p.CapabilityChecks...)
			if compat != nil {
				checks = append(checks, compat.CapabilityChecks...)
			}
			if len(checks) > 0 {
				for _, c := range checks {
					if !c.OK {
						return doctorCheck("warn", "capability_probe_failed", href)
					}
				}
			}
			if compat == nil || compat.Version == "" {
				return doctorCheck("warn", "agent_version_unknown", href)
			}
			if compat.Warning != "" {
				return doctorCheck("warn", "agent_version_untested", href)
			}
			return doctorCheck("ok", "agent_installed_handshake_cached", "")
		}})
	}
	servers, err := LoadMCPConfig()
	if err == nil {
		for name, cfg := range servers {
			jobs = append(jobs, doctor.Job{ID: "mcp." + name, Area: "brain", Run: func(ctx context.Context) doctor.Check {
				if !cfg.Enabled {
					return doctorCheck("skip", "mcp_disabled", "")
				}
				observed, connected, _ := mcpMgr.Probe(ctx, name)
				if !observed {
					return doctorCheck("skip", "mcp_not_observed", "#/brain")
				}
				if !connected {
					return doctorCheck("warn", "mcp_unreachable", "#/brain")
				}
				return doctorCheck("ok", "mcp_reachable", "")
			}})
		}
	}
	for _, channel := range []string{"ntfy", "webhook", "push"} {
		jobs = append(jobs, doctor.Job{ID: "notifications." + channel, Area: "notifications", Run: func(context.Context) doctor.Check {
			cfg, err := notificationConfig()
			if err != nil {
				return doctorCheck("warn", "notification_config_unavailable", "#/settings")
			}
			enabled := map[string]bool{"ntfy": cfg.Ntfy.Enabled, "webhook": cfg.Webhook.Enabled, "push": cfg.Push.Enabled}[channel]
			if !enabled {
				return doctorCheck("skip", "channel_disabled", "")
			}
			svc := workspaceSessions.notifications(nil)
			svc.mu.Lock()
			lastError := svc.lastError[channel]
			svc.mu.Unlock()
			if lastError != "" {
				return doctorCheck("warn", "notification_delivery_failed", "#/settings")
			}
			return doctorCheck("ok", "no_delivery_error_observed", "")
		}})
	}
	return jobs
}
func doctorTokenAges() doctor.Check {
	paths := []string{nodeTokenPath()}
	if usageVaultAccessStream() {
		entries, err := os.ReadDir(providerSecretRoot())
		if err != nil && !os.IsNotExist(err) {
			return doctorCheck("warn", "token_metadata_unavailable", "#/settings")
		}
		for _, entry := range entries {
			if entry.Type().IsRegular() && strings.HasSuffix(entry.Name(), ".sealed") {
				paths = append(paths, filepath.Join(providerSecretRoot(), entry.Name()))
			}
		}
	}
	known := false
	for _, path := range paths {
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil || !info.Mode().IsRegular() {
			return doctorCheck("warn", "token_metadata_unavailable", "#/settings")
		}
		known = true
		if time.Since(info.ModTime()) > 90*24*time.Hour {
			return doctorCheck("warn", "token_rotation_due", "#/settings")
		}
	}
	if usageVaultAccessStream() {
		for _, m := range loadRemoteMachines() {
			if n := savedMachineNode(m); n != nil && n.LinkedAt > 0 {
				known = true
				if time.Since(time.UnixMilli(n.LinkedAt)) > 90*24*time.Hour {
					return doctorCheck("warn", "token_rotation_due", "#/settings")
				}
			}
		}
	}
	if !known {
		return doctorCheck("skip", "token_age_unknown", "")
	}
	return doctorCheck("ok", "known_token_age_recent", "")
}
func doctorEngine(ctx context.Context) doctor.Check {
	if !usageVaultAccessStream() {
		return doctorCheck("skip", "vault_locked", "")
	}
	n := currentEngineNode()
	owner := currentEngineCapabilityOwner()
	outdatedNode := false
	if n != nil && !n.Direct {
		info, err := nodeMaintenanceProbe(ctx, n.URL, n.WebKey)
		if err != nil {
			if doctorObservationAllowed(ctx) {
				observeEngineReachability(owner, false)
			}
			return doctorCheck("warn", "engine_unreachable", "#/models")
		}
		outdatedNode = info.Version != Version
	}
	base, key := llamaBackendURL().String(), backendInferenceKey("")
	if n != nil {
		base, key = strings.TrimSuffix(n.V1, "/v1"), n.APIKey
		if base == "" {
			base = engineBase()
		}
	}
	code, err := directGET(ctx, base, "/health", key, nil)
	if err != nil || code != http.StatusOK {
		// A router without a loaded model is not an outage. GET /models is an
		// observation, never a request to wake the engine or load a model.
		code, err = directGET(ctx, base, "/v1/models", key, nil)
	}
	if err != nil || code != http.StatusOK {
		if doctorObservationAllowed(ctx) {
			observeEngineReachability(owner, false)
		}
		return doctorCheck("warn", "engine_unreachable_or_unloaded", "#/models")
	}
	if doctorObservationAllowed(ctx) {
		observeEngineReachability(owner, true)
	}
	var properties struct {
		BuildInfo string `json:"build_info"`
	}
	code, err = directGET(ctx, base, "/props", key, &properties)
	if err == nil && code == http.StatusOK {
		observeCapability(owner, "properties", true, "probe_succeeded")
		installed, _ := prebuiltVersion()
		if n == nil && resolvedEngineBin() == prebuiltServerBin() && installed != "" && properties.BuildInfo != "" && !strings.Contains(properties.BuildInfo, strings.TrimPrefix(installed, "b")) {
			return doctorCheck("warn", "engine_outdated", "#/models")
		}
	}
	if outdatedNode {
		return doctorCheck("warn", "engine_outdated", "#/models")
	}
	return doctorCheck("ok", "engine_reachable", "")
}
func runDoctor(ctx context.Context, id string) (doctor.Report, error) {
	select {
	case doctorSlots <- struct{}{}:
		defer func() { <-doctorSlots }()
	default:
		return doctor.Report{}, fmt.Errorf("doctor already running")
	}
	jobs := doctorJobs()
	if id != "" {
		found := false
		for _, j := range jobs {
			found = found || j.ID == id
		}
		if !found {
			return doctor.Report{}, fmt.Errorf("unknown doctor check")
		}
	}
	report := doctor.Run(ctx, jobs, id, 5*time.Second)
	if !usageVaultAccessStream() {
		checks := report.Checks[:0]
		for _, check := range report.Checks {
			if check.ID == "brain.primary_writable" || check.ID == "engine.reachable" {
				check.Status, check.Detail, check.Fix = "skip", "vault_locked", nil
			} else if check.Area != "core" && check.Area != "security" {
				continue
			}
			checks = append(checks, check)
		}
		report.Checks = checks
	}
	return report, nil
}
func handleDoctor(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodGet) {
		return
	}
	// Core/security remain inspectable when the vault is locked. Private
	// integration checks are omitted until it is unlocked.
	report, err := runDoctor(r.Context(), "")
	if err != nil {
		sendJSON(w, 429, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, report)
}
func handleDoctorRun(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		ID string `json:"id,omitempty"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	report, err := runDoctor(r.Context(), req.ID)
	if err != nil {
		sendJSON(w, 409, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, report)
}
