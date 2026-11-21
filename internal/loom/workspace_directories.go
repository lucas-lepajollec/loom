package loom

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

func usesNodeTransport(m RemoteMachine) bool { return m.NodeID != "" && m.User == "" }
func localWorkdir(path string) (string, error) {
	if path == "" || !filepath.IsAbs(path) {
		return "", errors.New("choose an existing absolute working directory")
	}
	path, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", errors.New("directory inaccessible")
	}
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		return "", errors.New("the path must be an existing directory")
	}
	return filepath.Clean(path), nil
}
func remoteWorkdir(path string) (string, error) {
	driveAbsolute := len(path) >= 3 && (path[0] >= 'A' && path[0] <= 'Z' || path[0] >= 'a' && path[0] <= 'z') && path[1] == ':' && (path[2] == '/' || path[2] == '\\')
	if path == "" || len(path) > 4096 || strings.ContainsAny(path, "\x00\r\n") || !(strings.HasPrefix(path, "/") || driveAbsolute) {
		return "", errors.New("absolute directory required")
	}
	return path, nil
}
func nodeDirectory(path string) (string, error) {
	if path == "" {
		path, _ = os.UserHomeDir()
	}
	return localWorkdir(path)
}
func handleNodeInventory(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodGet) {
		return
	}
	host, _ := os.Hostname()
	home, _ := os.UserHomeDir()
	sendJSON(w, 200, map[string]any{"ok": true, "hostname": host, "home": home, "os": runtime.GOOS, "tools": []any{}})
}
func handleNodeFolders(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodGet) {
		return
	}
	dir, err := nodeDirectory(r.URL.Query().Get("path"))
	if err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	f, err := os.Open(dir)
	if err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "node directory cannot be read"})
		return
	}
	defer f.Close()
	folders := []string{}
	for len(folders) < filesMaxEntries {
		entries, err := f.ReadDir(128)
		if err == io.EOF {
			break
		}
		if err != nil {
			sendJSON(w, 400, map[string]any{"ok": false, "error": "node directory cannot be read"})
			return
		}
		for _, e := range entries {
			if e.IsDir() {
				folders = append(folders, filepath.Join(dir, e.Name()))
				if len(folders) == filesMaxEntries {
					break
				}
			}
		}
	}
	sort.Strings(folders)
	sendJSON(w, 200, map[string]any{"ok": true, "path": dir, "parent": filepath.Dir(dir), "folders": folders})
}
func handleNodeWorkspace(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		Path string `json:"path"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	if !filepath.IsAbs(req.Path) || strings.ContainsAny(req.Path, "\x00\r\n") {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "absolute directory required"})
		return
	}
	if err := os.MkdirAll(req.Path, 0700); err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "node workspace cannot be created"})
		return
	}
	dir, err := nodeDirectory(req.Path)
	if err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "path": dir})
}
