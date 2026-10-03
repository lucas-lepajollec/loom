package loom

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/resources"
)

var workspaceFoldersMu sync.Mutex

func workspaceMachine(target string) (RemoteMachine, error) {
	for _, m := range loadRemoteMachines() {
		if m.ID == target {
			return m, nil
		}
	}
	return RemoteMachine{}, errors.New("workspace machine not found")
}

func managedWorkspace(target string) resources.Workspace {
	w := resources.Workspace{ID: "loom-" + target, Name: "Loom", Target: target, Managed: true, Default: true}
	if target == "local" {
		w.Path = filepath.Join(LoomHome(), "workspaces", "loom")
	} else if m, err := workspaceMachine(target); err == nil && m.Home != "" {
		w.Path = path.Join(strings.ReplaceAll(m.Home, "\\", "/"), "loom")
	}
	return w
}

func savedWorkspaces() resources.Workspaces {
	var c resources.Workspaces
	_ = getStoreJSON(bkState, "saved_workspaces", &c)
	if c.Defaults == nil {
		c.Defaults = map[string]string{}
	}
	if c.Items == nil {
		c.Items = []resources.Workspace{}
	}
	return c
}

func workspaceList(target string) []resources.Workspace {
	c := savedWorkspaces()
	out := []resources.Workspace{}
	for _, w := range c.Items {
		if w.Target == target {
			w.Default = c.Defaults[target] == w.ID
			out = append(out, w)
		}
	}
	if len(out) == 0 {
		if w := managedWorkspace(target); w.Path != "" {
			out = append(out, w)
		}
	}
	return out
}

func workspaceTarget(agent acpAgent) string {
	if !agent.Remote {
		return "local"
	}
	return agent.Machine
}

func defaultWorkspace(agent acpAgent) (resources.Workspace, error) {
	target := workspaceTarget(agent)
	if target == "" {
		return resources.Workspace{}, errors.New("choose a working folder for this custom remote harness")
	}
	for _, w := range workspaceList(target) {
		if w.Default {
			return w, nil
		}
	}
	return resources.Workspace{}, errors.New("choose a default workspace for this machine")
}

func prepareWorkspace(ctx context.Context, w resources.Workspace, create bool) (string, error) {
	if w.Target == "local" {
		if create {
			if !filepath.IsAbs(w.Path) {
				return "", errors.New("absolute workspace path required")
			}
			if err := os.MkdirAll(w.Path, 0755); err != nil {
				return "", errors.New("workspace directory could not be created")
			}
		}
		return acpDirectory(w.Path)
	}
	m, err := workspaceMachine(w.Target)
	if err != nil {
		return "", err
	}
	dir, err := remoteWorkdir(w.Path)
	if err != nil {
		return "", err
	}
	key, _, err := loomSSHKey()
	if err != nil {
		return "", err
	}
	script := "test -d " + shellQuote(dir)
	if create {
		script = "mkdir -p -- " + shellQuote(dir) + " && " + script
	}
	args := sshArgs(m, key, script)
	if lifecycleOS(&m) == "windows" {
		// Native Windows OpenSSH uses PowerShell, not Unix mkdir/test.
		script = "if (-not (Test-Path -LiteralPath " + powershellLiteral(dir) + " -PathType Container)) { throw 'workspace directory not found' }"
		if create {
			script = "New-Item -ItemType Directory -Force -Path " + powershellLiteral(dir) + " | Out-Null; " + script
		}
		args = windowsRemoteSSHArgs(m, key, script)
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, "ssh", args...).Run(); err != nil {
		return "", errors.New("workspace directory unavailable on the selected machine")
	}
	return dir, nil
}

func saveWorkspace(ctx context.Context, w resources.Workspace, create bool) (resources.Workspace, error) {
	w.Name, w.Path = strings.TrimSpace(w.Name), strings.TrimSpace(w.Path)
	if w.Target == "" {
		w.Target = "local"
	}
	if w.Name == "" || len([]rune(w.Name)) > 100 {
		return w, errors.New("workspace name required (maximum 100 characters)")
	}
	if w.Target != "local" {
		if _, err := workspaceMachine(w.Target); err != nil {
			return w, err
		}
	}
	if w.Managed && w.Path != managedWorkspace(w.Target).Path {
		return w, errors.New("invalid managed workspace")
	}
	canonical, err := prepareWorkspace(ctx, w, create)
	if err != nil {
		return w, err
	}
	w.Path = canonical
	workspaceFoldersMu.Lock()
	defer workspaceFoldersMu.Unlock()
	c := savedWorkspaces()
	implicit := managedWorkspace(w.Target)
	if c.Defaults[w.Target] == "" && !w.Default && w.ID != implicit.ID && implicit.Path != "" {
		implicit.Default = true
		c.Items = append(c.Items, implicit)
		c.Defaults[w.Target] = implicit.ID
	}
	if w.ID == "" {
		w.ID = newSessionID()
	}

	index := -1
	for i, old := range c.Items {
		if old.ID == w.ID {
			if old.Target != w.Target {
				return w, errors.New("workspace machine cannot be changed")
			}
			index = i
		} else if old.Target == w.Target && old.Path == w.Path {
			return w, errors.New("this folder is already a saved workspace")
		}
	}
	if c.Defaults[w.Target] == "" || w.Default {
		c.Defaults[w.Target] = w.ID
	}
	w.Default = c.Defaults[w.Target] == w.ID
	if index < 0 {
		if len(c.Items) >= 100 {
			return w, errors.New("maximum 100 saved workspaces")
		}
		c.Items = append(c.Items, w)
	} else {
		c.Items[index] = w
	}
	return w, putStoreJSON(bkState, "saved_workspaces", c)
}

func handleWorkspaces(w http.ResponseWriter, r *http.Request) {
	if !usageVaultAccess(w) {
		return
	}
	target := r.URL.Query().Get("target")
	if target == "" {
		target = "local"
	}
	if target != "local" {
		if _, err := workspaceMachine(target); err != nil {
			sendJSON(w, 404, map[string]any{"ok": false, "error": err.Error()})
			return
		}
	}
	if r.Method == http.MethodGet {
		sendJSON(w, 200, map[string]any{"ok": true, "workspaces": workspaceList(target)})
		return
	}
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		Action    string              `json:"action"`
		Workspace resources.Workspace `json:"workspace"`
		ID        string              `json:"id"`
		Create    bool                `json:"create"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	var saved *resources.Workspace
	if req.Action == "save" || req.Action == "" {
		if req.Workspace.Target != target && req.Workspace.Target != "" {
			sendJSON(w, 400, map[string]any{"ok": false, "error": "workspace target mismatch"})
			return
		}
		req.Workspace.Target = target
		folder, err := saveWorkspace(r.Context(), req.Workspace, req.Create)
		if err != nil {
			sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		saved = &folder
	} else if req.Action == "default" || req.Action == "remove" {
		workspaceFoldersMu.Lock()
		c := savedWorkspaces()
		found := false
		items := []resources.Workspace{}
		for _, item := range c.Items {
			if item.ID == req.ID && item.Target == target {
				found = true
				if req.Action == "remove" {
					continue
				}
				c.Defaults[target] = item.ID
			}
			items = append(items, item)
		}
		if !found {
			workspaceFoldersMu.Unlock()
			sendJSON(w, 404, map[string]any{"ok": false, "error": "workspace not found"})
			return
		}
		c.Items = items
		if req.Action == "remove" && c.Defaults[target] == req.ID {
			delete(c.Defaults, target)
			for _, item := range items {
				if item.Target == target {
					c.Defaults[target] = item.ID
					break
				}
			}
		}
		err := putStoreJSON(bkState, "saved_workspaces", c)
		workspaceFoldersMu.Unlock()
		if err != nil {
			sendJSON(w, 500, map[string]any{"ok": false, "error": "workspace could not be saved"})
			return
		}
	} else {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "invalid workspace action"})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "workspaces": workspaceList(target), "workspace": saved})
}
