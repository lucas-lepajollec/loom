package loom

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

type EnvContainer struct {
	Name   string `json:"name"`
	Image  string `json:"image"`
	State  string `json:"state"`
	Status string `json:"status"`
	Ports  string `json:"ports"`
}

func parseEnvDocker(out []byte) ([]EnvContainer, error) {
	containers := []EnvContainer{}
	for _, line := range bytes.Split(out, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var row struct{ Names, Image, State, Status, Ports string }
		if err := json.Unmarshal(line, &row); err != nil {
			return nil, errors.New("invalid Docker JSON line")
		}
		containers = append(containers, EnvContainer{row.Names, row.Image, row.State, row.Status, row.Ports})
	}
	return containers, nil
}

// Bound command output without losing cancellation or waiting on pipe readers.
type envOutput struct {
	bytes.Buffer
	overflow bool
}

func (b *envOutput) Write(p []byte) (int, error) {
	n := len(p)
	left := (1 << 20) - b.Len()
	if n > left {
		b.overflow = true
		p = p[:left]
	}
	_, _ = b.Buffer.Write(p)
	return n, nil
}

func envExec(ctx context.Context, name string, args []string, stdin string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	// A terminated command must not keep a check alive through inherited pipes.
	cmd.WaitDelay = 250 * time.Millisecond
	cmd.Stdin = strings.NewReader(stdin)
	var out, stderr envOutput
	cmd.Stdout, cmd.Stderr = &out, &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if len(msg) > 2048 {
			msg = msg[:2048]
		}
		return nil, fmt.Errorf("%s: %w %s", name, err, msg)
	}
	if out.overflow {
		return nil, errors.New("command output exceeds 1 MiB")
	}
	return out.Bytes(), nil
}

func (e *environment) remoteCommand(ctx context.Context, m RemoteMachine, script string) ([]byte, error) {
	key, _, err := e.sshKey()
	if err != nil {
		return nil, errors.New("Loom SSH key unavailable")
	}
	return e.run(ctx, "ssh", sshArgs(m, key, "sh", "-s"), remotePathPreamble+script)
}

func (e *environment) docker(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	machine := r.URL.Query().Get("machine")
	if machine == "" {
		machine = "local"
	}
	if r.Method == http.MethodPost {
		var req struct {
			Machine string `json:"machine"`
			Enabled bool   `json:"enabled"`
		}
		if !workspaceDecode(w, r, &req) {
			return
		}
		if req.Machine == "" {
			req.Machine = "local"
		}
		m, err := e.machine(req.Machine)
		if err != nil {
			envFailure(w, 400, err)
			return
		}
		if m != nil && req.Enabled {
			if _, err := machineOperationTransport(*m, "observe", lifecycleOS(m) != "windows"); err != nil {
				sendJSON(w, 409, map[string]any{"ok": false, "unsupported": true, "error": err.Error()})
				return
			}
		}
		e.mu.Lock()
		defer e.mu.Unlock()
		cfg, err := envProviderConfig()
		if err != nil {
			envFailure(w, 503, errors.New("environment store unavailable"))
			return
		}
		cfg.Docker[req.Machine] = req.Enabled
		if err := putStoreJSON(bkState, envProvidersState, cfg); err != nil {
			envFailure(w, 503, errors.New("environment store unavailable"))
			return
		}
		sendJSON(w, 200, map[string]any{"ok": true, "machine": req.Machine, "enabled": req.Enabled})
		return
	}
	if !workspaceMethod(w, r, http.MethodGet) {
		return
	}
	m, err := e.machine(machine)
	if err != nil {
		envFailure(w, 400, err)
		return
	}
	cfg, err := envProviderConfig()
	if err != nil {
		envFailure(w, 503, errors.New("environment store unavailable"))
		return
	}
	result := map[string]any{"ok": true, "machine": machine, "enabled": cfg.Docker[machine], "containers": []EnvContainer{}, "error": "", "unsupported": false, "transport": "local"}
	if m != nil {
		transport, supportErr := machineOperationTransport(*m, "observe", lifecycleOS(m) != "windows")
		result["transport"] = transport
		if supportErr != nil {
			result["ok"], result["unsupported"], result["error"] = false, true, supportErr.Error()
			sendJSON(w, 200, result)
			return
		}
	}
	if cfg.Docker[machine] {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		result["containers"], err = e.readDocker(ctx, m)
		if err != nil {
			result["ok"], result["error"], result["containers"] = false, err.Error(), []EnvContainer{}
			result["unsupported"] = unsupportedMachineOperation(err)
		}
	}
	// Missing Docker, denied access and offline machines are observations, not
	// failures of the Environment API or the other machines.
	sendJSON(w, 200, result)
}

// Shared Docker recipe and JSON projection; transports only supply command IO.
func (e *environment) readDocker(ctx context.Context, m *RemoteMachine) ([]EnvContainer, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	argv := []string{"ps", "-a", "--format", "{{json .}}"}
	var out []byte
	var err error
	if m == nil {
		out, err = e.run(ctx, "docker", argv, "")
	} else {
		transport, supportErr := machineOperationTransport(*m, "observe", lifecycleOS(m) != "windows")
		if supportErr != nil {
			return nil, supportErr
		}
		if transport == "node" {
			var result struct {
				OK         bool           `json:"ok"`
				Containers []EnvContainer `json:"containers"`
				Error      string         `json:"error"`
			}
			err = nodeMachineJSON(ctx, *m, http.MethodGet, "/api/node/env/docker", nil, &result)
			if err != nil {
				return nil, err
			}
			if !result.OK {
				if result.Error == "" {
					return nil, errors.New("node Docker observation unavailable")
				}
				return nil, errors.New(result.Error)
			}
			if result.Containers == nil || result.Error != "" {
				return nil, errors.New("invalid node Docker response")
			}
			return result.Containers, nil
		}
		out, err = e.remoteCommand(ctx, *m, "exec docker "+strings.Join([]string{argv[0], argv[1], argv[2], shellQuote(argv[3])}, " ")+"\n")
	}
	if err != nil {
		return nil, err
	}
	return parseEnvDocker(out)
}
