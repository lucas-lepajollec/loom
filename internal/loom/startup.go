package loom

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

type startupService struct {
	ID        string `json:"id"`
	Unit      string `json:"unit"`
	User      bool   `json:"user"`
	Installed bool   `json:"installed"`
	Enabled   bool   `json:"enabled"`
	Active    bool   `json:"active"`
}
type engineStartup struct {
	Engine string `json:"engine"`
	Model  string `json:"model"`
}

func readEngineStartup() engineStartup {
	p := engineStartup{Engine: "off"}
	getStoreJSON(bkState, "engine_startup", &p)
	return p
}

var startupUnitName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.@-]{0,120}$`)
var startupMu sync.Mutex
var startupCommand = func(ctx context.Context, user, write bool, args ...string) ([]byte, error) {
	command := "systemctl"
	if user {
		args = append([]string{"--user"}, args...)
	}
	if write && !user && os.Geteuid() != 0 {
		args = append([]string{"-n", "systemctl"}, args...)
		command = "sudo"
	}
	return exec.CommandContext(ctx, command, args...).Output()
}

func startupServices(ctx context.Context) []startupService {
	specs := []startupService{{ID: "ui", Unit: uiServiceName()}, {ID: "engine", Unit: serviceName()}}
	if isEngineWorker() {
		specs = []startupService{{ID: "node", Unit: "loom-node", User: true}}
	}
	for i := range specs {
		s := &specs[i]
		if !startupUnitName.MatchString(s.Unit) {
			continue
		}
		out, err := startupCommand(ctx, s.User, false, "show", s.Unit, "-p", "LoadState", "--value")
		s.Installed = err == nil && strings.TrimSpace(string(out)) == "loaded"
		if !s.Installed {
			continue
		}
		out, _ = startupCommand(ctx, s.User, false, "is-enabled", s.Unit)
		s.Enabled = strings.TrimSpace(string(out)) == "enabled"
		out, _ = startupCommand(ctx, s.User, false, "is-active", s.Unit)
		s.Active = strings.TrimSpace(string(out)) == "active"
	}
	return specs
}
func validateEngineStartup(p engineStartup, node bool) error {
	switch p.Engine {
	case "off":
		return nil
	case "llama.cpp":
		if !node {
			return errors.New("use the installed engine service to start llama.cpp on this control plane")
		}
		if !isLlamaServerPath(ReadConfig()["BIN"]) {
			return errors.New("configure an installed llama-server first")
		}
	case "vllm":
		if !vllmInstalled() || !validVLLMModel(p.Model) {
			return errors.New("install vLLM and select a valid model first")
		}
		models, err := listVLLMCache(vllmHFCache())
		cached := false
		for _, model := range models {
			cached = cached || model.ID == p.Model && model.Servable
		}
		if err != nil || !cached {
			return errors.New("download this vLLM model before enabling automatic startup")
		}
	default:
		return errors.New("engine must be off, llama.cpp or vllm")
	}
	return nil
}
func handleStartup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	if !isEngineWorker() && !usageVaultAccess(w) {
		return
	}
	if runtime.GOOS != "linux" {
		sendJSON(w, 200, map[string]any{"ok": true, "supported": false, "reason": "startup management currently requires Linux systemd"})
		return
	}
	if r.Method == http.MethodPost {
		startupMu.Lock()
		defer startupMu.Unlock()
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	services := startupServices(ctx)
	if r.Method == http.MethodPost {
		var req struct {
			Service string         `json:"service"`
			Enabled bool           `json:"enabled"`
			Policy  *engineStartup `json:"policy"`
		}
		if !workspaceDecode(w, r, &req) {
			return
		}
		var err error
		if req.Policy != nil && req.Service == "" {
			err = validateEngineStartup(*req.Policy, isEngineWorker())
			if req.Policy.Engine == "vllm" {
				for _, s := range services {
					if s.ID == "engine" && s.Enabled {
						err = errors.New("disable automatic llama.cpp service startup before choosing vLLM")
					}
				}
			}
			if err == nil {
				err = putStoreJSON(bkState, "engine_startup", req.Policy)
			}
		} else if req.Policy == nil {
			found := false
			for _, s := range services {
				if s.ID == req.Service && s.Installed {
					found = true
					if s.ID == "engine" && req.Enabled && readEngineStartup().Engine == "vllm" {
						err = errors.New("disable automatic vLLM startup before enabling the llama.cpp service")
						break
					}
					action := "disable"
					if req.Enabled {
						action = "enable"
					}
					_, err = startupCommand(ctx, s.User, true, action, s.Unit)
				}
			}
			if !found {
				err = errors.New("installed Loom service not found")
			}
		} else {
			err = errors.New("choose a service or engine policy")
		}
		if err != nil {
			message := "startup setting not saved: service unavailable or authorization denied"
			if req.Policy != nil || req.Service == "engine" && req.Enabled && readEngineStartup().Engine == "vllm" {
				message = err.Error()
			}
			sendJSON(w, 409, map[string]any{"ok": false, "error": message})
			return
		}
		services = startupServices(ctx)
	}
	var linger *bool
	if isEngineWorker() {
		out, err := exec.CommandContext(ctx, "loginctl", "show-user", strconv.Itoa(os.Getuid()), "-p", "Linger", "--value").Output()
		if err == nil {
			yes := strings.TrimSpace(string(out)) == "yes"
			linger = &yes
		}
	}
	sendJSON(w, 200, map[string]any{"ok": true, "supported": true, "node": isEngineWorker(), "services": services, "policy": readEngineStartup(), "linger": linger})
}

// Explicit saved policy, no generation/download and no second inference runtime.
// Native start paths retain responsibility for parameters, supervision and ports.
func startConfiguredEngine(ctx context.Context) {
	p := readEngineStartup()
	if p.Engine == "off" || validateEngineStartup(p, isEngineWorker()) != nil {
		return
	}
	if p.Engine == "llama.cpp" {
		_ = serviceAction("start")
		return
	}
	if vllmServingRequirements() != "" {
		return
	}
	if serviceIsActive() {
		vllm.mu.Lock()
		vllm.err = "automatic startup skipped: the llama.cpp engine is already running"
		vllm.mu.Unlock()
		return
	}
	values, err := loadVLLMParams(p.Model)
	if err != nil {
		return
	}
	if err = vllm.reserve("start", p.Model); err != nil {
		return
	}
	vllm.mu.Lock()
	vctx, cancel := vllm.startCtx, vllm.startCancel
	vllm.mu.Unlock()
	go func() {
		select {
		case <-ctx.Done():
			cancel()
		case <-vctx.Done():
		}
	}()
	err = vllm.start(vctx, p.Model, values, len(liveGPUs()), true)
	vllm.mu.Lock()
	vllm.job = ""
	vllm.pendingModel = ""
	if err != nil {
		vllm.err = "automatic startup failed"
	}
	vllm.mu.Unlock()
	if err != nil {
		cancel()
	}
}

// SSH startup management never accepts arbitrary unit names or shell fragments.
// It touches only the three known Loom units and never starts/stops them now.
func remoteStartupScript(service string, enabled bool) (string, error) {
	action := ""
	unit := ""
	if service != "" {
		switch service {
		case "ui":
			unit = "loom-ui"
		case "engine":
			unit = "loom-engine"
		case "node":
			unit = "loom-node"
		default:
			return "", errors.New("unknown Loom service")
		}
		action = "disable"
		if enabled {
			action = "enable"
		}
	}
	script := `[ "$(uname -s)" = Linux ] && command -v systemctl >/dev/null 2>&1 || { printf 'LOOM-STARTUP {"ok":true,"supported":false}\n'; exit 0; }
find_scope() { scope=""; [ "$1" != loom-node ] || scope="--user"; state=$(systemctl $scope show "$1" -p LoadState --value 2>/dev/null); [ "$state" = loaded ]; }
`
	if action != "" {
		script += `find_scope ` + unit + ` || exit 3
if [ "$scope" = --user ] || [ "$(id -u)" = 0 ]; then systemctl $scope ` + action + ` ` + unit + `; else sudo -n systemctl ` + action + ` ` + unit + `; fi || exit 4
`
	}
	script += `rows=""; sep=""
for pair in ui:loom-ui engine:loom-engine node:loom-node; do
 id=${pair%%:*}; unit=${pair#*:}; installed=false; enabled=false; active=false; user=false
 find_scope "$unit" && installed=true
 [ "$scope" != --user ] || user=true
 [ "$(systemctl $scope is-enabled "$unit" 2>/dev/null)" != enabled ] || enabled=true
 [ "$(systemctl $scope is-active "$unit" 2>/dev/null)" != active ] || active=true
 rows="$rows$sep{\"id\":\"$id\",\"unit\":\"$unit\",\"user\":$user,\"installed\":$installed,\"enabled\":$enabled,\"active\":$active}";sep=,
done
linger=null; l=$(loginctl show-user "$(id -u)" -p Linger --value 2>/dev/null);[ "$l" != yes ] || linger=true;[ "$l" != no ] || linger=false
printf 'LOOM-STARTUP {"ok":true,"supported":true,"services":[%s],"linger":%s}\n' "$rows" "$linger"
`
	return script, nil
}
func handleMachineStartup(w http.ResponseWriter, r *http.Request) {
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
	if lifecycleOS(&m) == "windows" {
		sendJSON(w, 200, map[string]any{"ok": true, "supported": false, "reason": "startup management currently requires Linux systemd"})
		return
	}
	var req struct {
		Service string `json:"service"`
		Enabled bool   `json:"enabled"`
	}
	if r.Method == http.MethodPost {
		if !workspaceDecode(w, r, &req) {
			return
		}
		if req.Service == "" {
			sendJSON(w, 400, map[string]any{"ok": false, "error": "service required"})
			return
		}
	}
	script, err := remoteStartupScript(req.Service, req.Enabled)
	if err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	key, _, err := loomSSHKey()
	if err != nil {
		sendJSON(w, 500, map[string]any{"ok": false, "error": "SSH key unavailable"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ssh", sshArgs(m, key, "sh", "-s")...)
	cmd.Stdin = strings.NewReader(script)
	out, err := cmd.Output()
	if err != nil {
		sendJSON(w, 409, map[string]any{"ok": false, "error": "startup could not be read or changed: check SSH, systemd and permission to control Loom services"})
		return
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "LOOM-STARTUP ") {
			var state map[string]any
			if json.Unmarshal([]byte(strings.TrimPrefix(line, "LOOM-STARTUP ")), &state) == nil {
				sendJSON(w, 200, state)
				return
			}
		}
	}
	sendJSON(w, 502, map[string]any{"ok": false, "error": "machine did not report startup state"})
}
func handleMachineNodeStartup(w http.ResponseWriter, r *http.Request) {
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
	n := savedMachineNode(m)
	if n == nil || n.Direct || n.Role != "engine-node" {
		sendJSON(w, 409, map[string]any{"ok": false, "error": "link this machine's engine node to configure its engines"})
		return
	}
	cloned := r.Clone(r.Context())
	u := *r.URL
	cloned.URL = &u
	u.Path = "/api/startup"
	u.RawPath = ""
	u.RawQuery = ""
	proxyEngineNode(w, cloned, n)
}
