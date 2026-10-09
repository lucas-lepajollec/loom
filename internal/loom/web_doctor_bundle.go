package loom

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/capability"
	"github.com/lucas-lepajollec/loom/internal/loom/diagnostics"
)

func diagnosticSources(ctx context.Context) ([]diagnostics.Source, diagnostics.Redactor, error) {
	report, err := runDoctor(ctx, "")
	if err != nil {
		return nil, diagnostics.Redactor{}, err
	}
	home, _ := os.UserHomeDir()
	r := diagnostics.Redactor{Homes: []string{home, LoomHome()}, Secrets: []string{sessionGatewayToken, readAPIKey()}}
	if token, err := readNodeToken(); err == nil {
		r.Secrets = append(r.Secrets, token)
	}
	gatewayMu.Lock()
	if gatewayToken.home == LoomHome() {
		r.Secrets = append(r.Secrets, gatewayToken.token)
	}
	gatewayMu.Unlock()
	workspaceSessions.mu.Lock()
	for _, key := range workspaceSessions.keys {
		r.Secrets = append(r.Secrets, key)
	}
	workspaceSessions.mu.Unlock()
	if n := currentEngineNode(); n != nil {
		r.Secrets = append(r.Secrets, n.WebKey, n.APIKey)
	}
	compatibility := []runtimeCompatibilityRecord{}
	failed := []map[string]any{}
	for _, adapter := range registeredRuntimes.Adapters() {
		a, ok := adapter.(*acpAdapter)
		if !ok {
			continue
		}
		var c runtimeCompatibilityRecord
		if getStoreJSON(bkState, "agent_compat_"+a.agent.ID, &c) {
			compatibility = append(compatibility, c)
		}
		if p, ok := cachedAgentProbe(a.agent.ID); ok {
			if p.Error != "" && len(p.CapabilityChecks) == 0 {
				failed = append(failed, map[string]any{"runtime": a.agent.ID, "at": p.At, "duration_seconds": p.Duration, "reason": "handshake_failed"})
			}
			checks := append([]capability.Probe{}, p.CapabilityChecks...)
			if p.Compatibility != nil {
				checks = append(checks, p.Compatibility.CapabilityChecks...)
			}
			for _, check := range checks {
				if !check.OK {
					failed = append(failed, map[string]any{"runtime": a.agent.ID, "at": p.At, "capability": check.Capability, "reason": check.Reason})
				}
			}
		}
	}
	for _, m := range loadRemoteMachines() {
		r.Homes = append(r.Homes, m.Home)
		if n := savedMachineNode(m); n != nil {
			r.Secrets = append(r.Secrets, n.WebKey, n.APIKey)
		}
	}
	// Never include the database, discussion/session records, Brain folders,
	// raw protocol frames, process environment or arbitrary files from LOOM_HOME.
	config := ReadConfig()
	for key, value := range config {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "key") || strings.Contains(lower, "token") || strings.Contains(lower, "password") || strings.Contains(lower, "secret") {
			r.Secrets = append(r.Secrets, value)
		}
	}
	providers := workspaceSessions.providers()
	servers, _ := LoadMCPConfig()
	for _, server := range servers {
		for _, value := range server.Env {
			r.Secrets = append(r.Secrets, value)
		}
		for _, value := range server.Headers {
			r.Secrets = append(r.Secrets, value)
		}
	}
	for _, name := range []string{"notify-ntfy", "notify-webhook", "notify-vapid", "notify-actions"} {
		if secret, err := readProviderSecret(name); err == nil {
			r.Secrets = append(r.Secrets, secret)
			if name == "notify-vapid" {
				r.Secrets = append(r.Secrets, strings.Split(secret, ".")...)
			}
		}
	}
	r.Secrets = append(r.Secrets, getStr(bkState, stPushVAPIDPriv))
	events, _, _ := workspaceSessions.events.Snapshot(0)
	types := []map[string]any{}
	for _, e := range events {
		types = append(types, map[string]any{"type": e.Type, "at": e.At})
	}
	sources := []diagnostics.Source{
		{Name: "versions.json", Value: map[string]any{"loom": Version, "channel": updateChannel(), "os": runtime.GOOS, "arch": runtime.GOARCH, "go": runtime.Version()}},
		{Name: "doctor.json", Value: report},
		{Name: "compatibility.json", Value: compatibility},
		{Name: "failed-probes.json", Value: failed},
		{Name: "config.json", Value: map[string]any{"config": config, "providers": providers, "mcp": servers}},
		{Name: "events.json", Value: types},
	}
	// Known, local service log files only. No subprocess/journalctl, SSH,
	// arbitrary configured log path, native transcript or agent session log.
	for _, log := range []struct{ file, archive string }{{serviceName() + ".log", "logs/engine.log"}, {"loom-node.log", "logs/node.log"}, {"loom-build.log", "logs/build.log"}} {
		name := log.file
		if strings.ContainsAny(name, "/\\") {
			continue
		}
		data := diagnosticLogTail(filepath.Join(LoomHome(), name))
		sources = append(sources, diagnostics.Source{Name: log.archive, Log: data, IsLog: true})
	}
	// Lifecycle logs describe installs/updates only, but still receive the
	// same content omission as engine/node logs.
	var logs strings.Builder
	for key := range allKV(bkState) {
		if !strings.HasPrefix(key, harnessLifecyclePrefix) || logs.Len() >= diagnostics.MaxSourceBytes {
			continue
		}
		var setting harnessLifecycleSetting
		if getStoreJSON(bkState, key, &setting) && setting.LastAuto != nil {
			logs.WriteString(setting.LastAuto.Log)
			logs.WriteByte('\n')
		}
	}
	sources = append(sources, diagnostics.Source{Name: "logs/agents.log", Log: logs.String(), IsLog: true})
	return sources, r, nil
}
func diagnosticLogTail(path string) string {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	if info.Size() > diagnostics.MaxSourceBytes {
		if _, err = f.Seek(-diagnostics.MaxSourceBytes, io.SeekEnd); err != nil {
			return ""
		}
	}
	data, _ := io.ReadAll(io.LimitReader(f, diagnostics.MaxSourceBytes))
	return string(data)
}
func handleDoctorBundle(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) || !usageVaultAccess(w) {
		return
	}
	var req struct{}
	if !workspaceDecode(w, r, &req) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	sources, redactor, err := diagnosticSources(ctx)
	if err == nil {
		var data []byte
		data, _, err = diagnostics.Build(sources, redactor)
		if err == nil && usageVaultAccess(w) {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Content-Type", "application/zip")
			w.Header().Set("Content-Disposition", `attachment; filename="loom-diagnostics.zip"`)
			w.WriteHeader(200)
			_, _ = w.Write(data)
			return
		}
	}
	if err != nil {
		sendJSON(w, 503, map[string]any{"ok": false, "error": "diagnostic bundle unavailable"})
	}
}
