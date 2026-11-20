package loom

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Each brick of Loom runs on one machine: local AI (the engine), voice,
// Codeman (agents) and files (the second brain). The engine and voice keep
// their own settings (/api/engine/node, /api/voice/node) and Codeman its
// connection; this file adds the files brick and one read-only view of
// which machine provides what. "local" is the machine serving this Loom.

const filesMachineKey = "files_machine"

func filesMachine() string {
	if m := strings.TrimSpace(getStr(bkState, filesMachineKey)); m != "" {
		return m
	}
	return "local"
}

var filesProxyClient = &http.Client{Timeout: 60 * time.Second}

// proxyFiles forwards a files request to the machine that holds the files
// brick. It returns false when the files are local.
func proxyFiles(w http.ResponseWriter, r *http.Request) bool {
	machine := filesMachine()
	if machine == "local" {
		return false
	}
	access, err := nodeMachineAccessForModule(machine, "files")
	if err != nil {
		sendJSON(w, 502, map[string]any{"ok": false, "error": "files machine unavailable: " + err.Error()})
		return true
	}
	target := strings.TrimRight(access.URL, "/") + r.URL.Path
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, r.Method, target, http.MaxBytesReader(w, r.Body, filesMaxText+8192))
	if err != nil {
		sendJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
		return true
	}
	req.Header.Set("Authorization", "Bearer "+access.Token)
	if ct := r.Header.Get("Content-Type"); ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	resp, err := filesProxyClient.Do(req)
	if err != nil {
		sendJSON(w, 502, map[string]any{"ok": false, "error": "files machine unreachable"})
		return true
	}
	defer resp.Body.Close()
	for _, h := range []string{"Content-Type", "Content-Disposition", "Content-Security-Policy", "X-Content-Type-Options", "Last-Modified"} {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, io.LimitReader(resp.Body, filesMaxRaw+1))
	return true
}

// machineForURL finds the paired machine whose host serves url ("local" for
// this machine or loopback, "" when no paired machine matches).
func machineForURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return ""
	}
	host := u.Hostname()
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return "local"
	}
	for _, m := range loadRemoteMachines() {
		if strings.EqualFold(m.Host, host) || strings.EqualFold(m.Hostname, host) {
			return m.ID
		}
		if n := savedMachineNode(m); n != nil {
			if nu, err := url.Parse(n.URL); err == nil && strings.EqualFold(nu.Hostname(), host) {
				return m.ID
			}
		}
	}
	return ""
}

// handleMachineRoles:
//
//	GET  → {roles: {engine, voice, codeman, files}} (machine ids, "local" or "")
//	POST {role: "files", machine} → moves the files brick ("local" for this machine)
func handleMachineRoles(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		var body struct {
			Role    string `json:"role"`
			Machine string `json:"machine"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
			sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		if body.Role != "files" {
			sendJSON(w, 400, map[string]any{"ok": false, "error": "only the files role is set here; the engine, voice and Codeman keep their own settings"})
			return
		}
		machine := strings.TrimSpace(body.Machine)
		if machine == "" {
			machine = "local"
		}
		if machine != "local" {
			if _, err := nodeMachineAccessForModule(machine, "files"); err != nil {
				sendJSON(w, 409, map[string]any{"ok": false, "error": err.Error()})
				return
			}
		}
		if err := putStr(bkState, filesMachineKey, machine); err != nil {
			sendJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
			return
		}
	}
	roles := map[string]string{"engine": "local", "voice": "local", "codeman": "", "files": filesMachine()}
	if n := currentEngineNode(); n != nil {
		roles["engine"] = machineForURL(n.URL)
		if n.Direct {
			roles["engine"] = machineForURL(n.V1)
		}
	}
	if v := strings.TrimSpace(getStr(bkState, "voice_machine")); v != "" {
		roles["voice"] = v
	}
	if c := loadCodemanConfig().URL; c != "" {
		roles["codeman"] = machineForURL(c)
	}
	sendJSON(w, 200, map[string]any{"ok": true, "roles": roles})
}
